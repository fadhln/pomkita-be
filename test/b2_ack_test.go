package test

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestB2AckMigration_ProvidesDecisionHeadAndProcedure(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	resetB0Foundation(t, conn, true)
	for _, name := range []string{
		"000005_b1_catalog.up.sql",
		"000006_b1_shifts.up.sql",
		"000007_b1_drafts.up.sql",
		"000008_b1_submit.up.sql",
		"000009_b1_recovery.up.sql",
	} {
		if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), name)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), "000010_b2_ack.up.sql")); err != nil {
		t.Fatalf("apply ack migration: %v", err)
	}

	var tables int
	if err := conn.QueryRow(ctx, `
		select count(*)
		from pg_class c
		join pg_namespace n on n.oid = c.relnamespace
		where n.nspname = 'public'
		  and c.relname = any($1::text[])
	`, []string{"ack_decisions", "ack_head", "ack_supersessions"}).Scan(&tables); err != nil {
		t.Fatalf("query ack tables: %v", err)
	}
	if tables != 3 {
		t.Fatalf("ack table count: got %d, want 3", tables)
	}
	if _, err := conn.Exec(ctx, `select fn_ack_shift(null, null, null, null, null, null, null)`); err == nil {
		t.Fatal("ack procedure accepted an invalid call")
	}
}

func TestB2AmendmentMigration_ProvidesAllowlistedApprovalTables(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	resetB0Foundation(t, conn, true)
	for _, name := range []string{
		"000005_b1_catalog.up.sql",
		"000006_b1_shifts.up.sql",
		"000007_b1_drafts.up.sql",
		"000008_b1_submit.up.sql",
		"000009_b1_recovery.up.sql",
		"000010_b2_ack.up.sql",
		"000011_b2_amendment.up.sql",
	} {
		if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), name)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	var partialIndex bool
	if err := conn.QueryRow(ctx, `
		select exists (
			select 1
			from pg_index i
			join pg_class c on c.oid = i.indexrelid
			join pg_class t on t.oid = i.indrelid
			join pg_namespace n on n.oid = t.relnamespace
			where n.nspname = 'public'
			  and t.relname = 'amendments'
			  and c.relname = 'amendments_one_pending_base'
			  and i.indpred is not null
		)
	`).Scan(&partialIndex); err != nil {
		t.Fatalf("query amendment index: %v", err)
	}
	if !partialIndex {
		t.Fatal("pending amendment index is missing")
	}
}

func TestB2Governance_AckAndAmendmentSupersedeTheCurrentHead(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	applyB2Migrations(t, conn)
	defer resetB0Foundation(t, conn, true)

	const (
		org        = "11111111-1111-4111-8111-111111111111"
		station    = "22222222-2222-4222-8222-222222222222"
		supervisor = "33333333-3333-4333-8333-333333333333"
		admin      = "44444444-4444-4444-8444-444444444444"
		requester  = "55555555-5555-4555-8555-555555555555"
		dispenser  = "66666666-6666-4666-8666-666666666666"
		nozzle     = "77777777-7777-4777-8777-777777777777"
		tank       = "88888888-8888-4888-8888-888888888888"
		lossID     = "99999999-9999-4999-8999-999999999999"
		policy     = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		threshold  = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
		evidence   = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	)
	seedGovernanceData(t, conn, org, station, supervisor, admin, requester, dispenser, nozzle, tank, policy, threshold, evidence)
	setTestContext(t, conn, org, station, supervisor, "Supervisor")
	shift, report := submitGovernanceReport(t, conn, station, supervisor, lossID)
	setTestContext(t, conn, org, station, admin, "Station Admin")
	if _, err := conn.Exec(ctx, `select * from fn_ack_shift($1,$2,1,'acked',null,false,null)`, shift, report); err != nil {
		t.Fatalf("ack report: %v", err)
	}
	if _, err := conn.Exec(ctx, `select * from fn_ack_shift($1,$2,1,'rejected','second',false,null)`, shift, report); err == nil {
		t.Fatal("second acknowledgement was accepted")
	}

	var ackID string
	if err := conn.QueryRow(ctx, `select active_ack_id::text from ack_head where report_id=$1`, report).Scan(&ackID); err != nil {
		t.Fatalf("read old head: %v", err)
	}
	var staleHash []byte
	if err := conn.QueryRow(ctx, `select app.digest(convert_to(jsonb_build_array(fn_amendment_base_payload($1,$2,$3,$4),1)::text,'UTF8'),'sha256')`, org, station, shift, report).Scan(&staleHash); err != nil {
		t.Fatalf("base hash: %v", err)
	}
	const amendment = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	var salesID string
	if err := conn.QueryRow(ctx, `select sales_id::text from sales_declared where report_id=$1`, report).Scan(&salesID); err != nil {
		t.Fatalf("read sale ID: %v", err)
	}
	_, err := conn.Exec(ctx, `
		insert into amendments(amendment_id,org_id,station_id,shift_id,base_report_id,reason,requester_user_id,stale_check_hash)
		values($1,$2,$3,$4,$5,'correct declared cash',$6,$7)
	`, amendment, org, station, shift, report, requester, staleHash)
	if err != nil {
		t.Fatalf("insert amendment: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		insert into amendment_items(amendment_id,org_id,station_id,shift_id,target_kind,target_logical_id,field,old_value,new_value)
		select $1,$2,$3,$4,'sales_declared',sales_id,'cash_amount',to_jsonb(cash_amount::text),to_jsonb('2500'::text)
		from sales_declared where report_id=$5
	`, amendment, org, station, shift, report); err != nil {
		t.Fatalf("insert amendment item: %v", err)
	}
	var applied string
	var version int
	if err := conn.QueryRow(ctx, `select applied_report_id::text,version_no from fn_approve_amendment($1,$2)`, amendment, staleHash).Scan(&applied, &version); err != nil {
		t.Fatalf("approve amendment: %v", err)
	}
	if version != 2 || applied == report {
		t.Fatalf("amendment result: applied=%s version=%d", applied, version)
	}
	var oldHead, newHead *string
	if err := conn.QueryRow(ctx, `select active_ack_id::text from ack_head where report_id=$1`, report).Scan(&oldHead); err != nil {
		t.Fatalf("read superseded head: %v", err)
	}
	if err := conn.QueryRow(ctx, `select active_ack_id::text from ack_head where report_id=$1`, applied).Scan(&newHead); err != nil {
		t.Fatalf("read replacement head: %v", err)
	}
	if oldHead != nil || newHead != nil {
		t.Fatalf("head pointers: old=%v new=%v, want both NULL", oldHead, newHead)
	}
	var replacementVersion int
	var superseded string
	if err := conn.QueryRow(ctx, `select replacement_version_no,superseded_ack_id::text from ack_supersessions where old_report_id=$1`, report).Scan(&replacementVersion, &superseded); err != nil {
		t.Fatalf("read supersession: %v", err)
	}
	if replacementVersion != 2 || superseded != ackID {
		t.Fatalf("supersession: version=%d ack=%s", replacementVersion, superseded)
	}
}

func applyB2Migrations(t *testing.T, conn *pgx.Conn) {
	t.Helper()
	resetB0Foundation(t, conn, true)
	for _, name := range []string{
		"000005_b1_catalog.up.sql", "000006_b1_shifts.up.sql", "000007_b1_drafts.up.sql",
		"000008_b1_submit.up.sql", "000009_b1_recovery.up.sql", "000010_b2_ack.up.sql",
		"000011_b2_amendment.up.sql",
	} {
		if _, err := conn.Exec(context.Background(), readMigration(t, repositoryRoot(t), name)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
}

func seedGovernanceData(t *testing.T, conn *pgx.Conn, org, station, supervisor, admin, requester, dispenser, nozzle, tank, policy, threshold, evidence string) {
	t.Helper()
	ctx := context.Background()
	statements := []struct {
		query string
		args  []any
	}{
		{`insert into organizations(org_id,name) values($1,'Governance')`, []any{org}},
		{`insert into stations(org_id,station_id,timezone) values($1,$2,'Asia/Jakarta')`, []any{org, station}},
		{`insert into users(user_id,org_id,email,display_name,password_hash) values($1,$2,$3,$4,app.crypt('pw',app.gen_salt('bf')))`, []any{supervisor, org, "supervisor@example.com", "Supervisor"}},
		{`insert into users(user_id,org_id,email,display_name,password_hash) values($1,$2,$3,$4,app.crypt('pw',app.gen_salt('bf')))`, []any{admin, org, "admin@example.com", "Admin"}},
		{`insert into users(user_id,org_id,email,display_name,password_hash) values($1,$2,$3,$4,app.crypt('pw',app.gen_salt('bf')))`, []any{requester, org, "requester@example.com", "Requester"}},
		{`insert into user_station_roles(org_id,station_id,user_id,role) values($1,$2,$3,'Supervisor')`, []any{org, station, supervisor}},
		{`insert into user_station_roles(org_id,station_id,user_id,role) values($1,$2,$3,'Station Admin')`, []any{org, station, admin}},
		{`insert into user_station_roles(org_id,station_id,user_id,role) values($1,$2,$3,'Owner')`, []any{org, station, requester}},
		{`insert into dispensers(org_id,station_id,dispenser_id) values($1,$2,$3)`, []any{org, station, dispenser}},
		{`insert into tanks(org_id,station_id,tank_id) values($1,$2,$3)`, []any{org, station, tank}},
		{`insert into nozzles(org_id,station_id,nozzle_id,dispenser_id,meter_max) values($1,$2,$3,$4,99999.9)`, []any{org, station, nozzle, dispenser}},
		{`insert into dispenser_nozzle_map(org_id,station_id,dispenser_id,nozzle_id,valid_period) values($1,$2,$3,$4,tstzrange('2020-01-01','2030-01-01','[)'))`, []any{org, station, dispenser, nozzle}},
		{`insert into dispenser_prices(org_id,station_id,nozzle_id,price,valid_period,created_by) values($1,$2,$3,10000,tstzrange('2020-01-01','2030-01-01','[)'),$4)`, []any{org, station, nozzle, supervisor}},
		{`insert into threshold_policy_revisions(rev_id,policy_id,org_id,valid_from,created_by) values($1,$2,$3,'2020-01-01',$4)`, []any{threshold, policy, org, supervisor}},
		{`insert into evidence_policy_revisions(rev_id,policy_id,org_id,valid_from,mode,created_by) values($1,$2,$3,'2020-01-01','opsional',$4)`, []any{evidence, policy, org, supervisor}},
	}
	for _, statement := range statements {
		if _, err := conn.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed governance data: %v", err)
		}
	}
}

func setTestContext(t *testing.T, conn *pgx.Conn, org, station, user, role string) {
	t.Helper()
	for key, value := range map[string]string{
		"app.context_valid": "true", "app.org_id": org, "app.station_id": station,
		"app.user_id": user, "app.role": role,
	} {
		if _, err := conn.Exec(context.Background(), `select set_config($1,$2,false)`, key, value); err != nil {
			t.Fatalf("set test context: %v", err)
		}
	}
}

func submitGovernanceReport(t *testing.T, conn *pgx.Conn, station, supervisor, lossID string) (string, string) {
	t.Helper()
	ctx := context.Background()
	var shift string
	if err := conn.QueryRow(ctx, `select shift_id::text from fn_open_shift($1,$2,'2026-01-01 12:00+00',false,null,null,null,null)`, station, supervisor).Scan(&shift); err != nil {
		t.Fatalf("open governance shift: %v", err)
	}
	var draft, claim string
	var revision int
	if err := conn.QueryRow(ctx, `select draft_id::text,claim_token::text,revision from fn_claim_draft($1)`, shift).Scan(&draft, &claim, &revision); err != nil {
		t.Fatalf("claim governance draft: %v", err)
	}
	request := fmt.Sprintf(`{"hash_version":1,"readings":[{"nozzle_id":"77777777-7777-4777-8777-777777777777","meter_start":"10.0","meter_end":"20.0"}],"sales":[{"dispenser_id":"66666666-6666-4666-8666-666666666666","cash_amount":"2000"}],"losses":[{"loss_id":"%s","direction":"loss","reason_code":"test","liters":"1.00","cash_amount":"100","note":"old"}]}`, lossID)
	var report string
	if err := conn.QueryRow(ctx, `select report_id::text from fn_submit_shift($1,$2,$3,$4,'governance-submit',$5::jsonb)`, shift, draft, claim, revision, request).Scan(&report); err != nil {
		t.Fatalf("submit governance report: %v", err)
	}
	return shift, report
}
