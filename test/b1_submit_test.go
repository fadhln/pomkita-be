package test

import (
	"context"
	"strings"
	"testing"
)

func TestB1Submit_ReportAndPolicyObjectsExist(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), "000007_b1_submit.down.sql")); err != nil {
		t.Fatalf("reset submit: %v", err)
	}
	defer func() {
		if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), "000007_b1_submit.down.sql")); err != nil {
			t.Errorf("clean up submit: %v", err)
		}
	}()
	resetB0Foundation(t, conn, true)
	for _, name := range []string{"000004_b1_catalog.up.sql", "000005_b1_shifts.up.sql", "000006_b1_drafts.up.sql", "000007_b1_submit.up.sql"} {
		if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), name)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	var count int
	if err := conn.QueryRow(ctx, `select count(*) from pg_class c join pg_namespace n on n.oid=c.relnamespace where n.nspname='public' and c.relname=any($1::text[])`, []string{"threshold_policy_revisions", "evidence_policy_revisions", "evidence_policy_types", "policy_snapshot_sets", "policy_snapshot_items", "shift_reports", "dispenser_readings", "sales_declared", "loss_identity", "loss_entries", "deliveries", "dip_readings"}).Scan(&count); err != nil {
		t.Fatalf("query submit tables: %v", err)
	}
	if count != 12 {
		t.Fatalf("submit table count: got %d, want 12", count)
	}
}

func TestB1Submit_RolloverAndReplay(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	for _, name := range []string{"000007_b1_submit.down.sql", "000006_b1_drafts.down.sql", "000005_b1_shifts.down.sql", "000004_b1_catalog.down.sql"} {
		if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), name)); err != nil {
			t.Fatalf("reset %s: %v", name, err)
		}
	}
	resetB0Foundation(t, conn, true)
	for _, name := range []string{"000004_b1_catalog.up.sql", "000005_b1_shifts.up.sql", "000006_b1_drafts.up.sql", "000007_b1_submit.up.sql"} {
		if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), name)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	defer func() {
		if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), "000007_b1_submit.down.sql")); err != nil {
			t.Errorf("clean up submit: %v", err)
		}
	}()
	const org = "11111111-1111-4111-8111-111111111111"
	const station = "22222222-2222-4222-8222-222222222222"
	const user = "33333333-3333-4333-8333-333333333333"
	const dispenser = "44444444-4444-4444-8444-444444444444"
	const nozzle = "55555555-5555-4555-8555-555555555555"
	const policy = "66666666-6666-4666-8666-666666666666"
	const threshold = "77777777-7777-4777-8777-777777777777"
	const evidence = "88888888-8888-4888-8888-888888888888"
	for _, s := range []struct {
		q string
		a []any
	}{
		{`insert into organizations(org_id,name) values($1,'Submit')`, []any{org}},
		{`insert into stations(org_id,station_id,timezone) values($1,$2,'Asia/Jakarta')`, []any{org, station}},
		{`insert into users(user_id,org_id,display_name) values($1,$2,'Supervisor')`, []any{user, org}},
		{`insert into user_station_roles(org_id,station_id,user_id,role) values($1,$2,$3,'Supervisor')`, []any{org, station, user}},
		{`insert into dispensers(org_id,station_id,dispenser_id) values($1,$2,$3)`, []any{org, station, dispenser}},
		{`insert into nozzles(org_id,station_id,nozzle_id,dispenser_id,meter_max) values($1,$2,$3,$4,99999.9)`, []any{org, station, nozzle, dispenser}},
		{`insert into dispenser_nozzle_map(org_id,station_id,dispenser_id,nozzle_id,valid_period) values($1,$2,$3,$4,tstzrange('2020-01-01','2030-01-01','[)'))`, []any{org, station, dispenser, nozzle}},
		{`insert into dispenser_prices(org_id,station_id,nozzle_id,price,valid_period,created_by) values($1,$2,$3,10000,tstzrange('2020-01-01','2030-01-01','[)'),$4)`, []any{org, station, nozzle, user}},
		{`insert into threshold_policy_revisions(rev_id,policy_id,org_id,valid_from,created_by) values($1,$2,$3,'2020-01-01',$4)`, []any{threshold, policy, org, user}},
		{`insert into evidence_policy_revisions(rev_id,policy_id,org_id,valid_from,mode,created_by) values($1,$2,$3,'2020-01-01','opsional',$4)`, []any{evidence, policy, org, user}},
	} {
		if _, err := conn.Exec(ctx, s.q, s.a...); err != nil {
			t.Fatalf("seed submit: %v", err)
		}
	}
	for k, v := range map[string]string{"app.context_valid": "true", "app.org_id": org, "app.station_id": station, "app.user_id": user, "app.role": "Supervisor"} {
		if _, err := conn.Exec(ctx, `select set_config($1,$2,false)`, k, v); err != nil {
			t.Fatalf("context: %v", err)
		}
	}
	var shift, draft, token string
	var revision int
	if err := conn.QueryRow(ctx, `select shift_id::text from fn_open_shift($1,$2,'2026-01-01 12:00+00',false,null,null,null,null)`, station, user).Scan(&shift); err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := conn.QueryRow(ctx, `select draft_id::text,claim_token::text,revision from fn_claim_draft($1)`, shift).Scan(&draft, &token, &revision); err != nil {
		t.Fatalf("claim: %v", err)
	}
	req := `{"hash_version":1,"readings":[{"nozzle_id":"55555555-5555-4555-8555-555555555555","meter_start":"99999.9","meter_end":"0.0"}],"sales":[{"dispenser_id":"44444444-4444-4444-8444-444444444444","cash_amount":"1000"}]}`
	var report string
	if err := conn.QueryRow(ctx, `select report_id::text from fn_submit_shift($1,$2,$3,$4,'idem-1',$5::jsonb)`, shift, draft, token, revision, req).Scan(&report); err != nil {
		t.Fatalf("submit: %v", err)
	}
	var sale string
	if err := conn.QueryRow(ctx, `select expected_sale_rupiah::text from dispenser_readings where report_id=$1`, report).Scan(&sale); err != nil {
		t.Fatalf("reading: %v", err)
	}
	if sale != "1000" {
		t.Fatalf("sale: got %s, want 1000", sale)
	}
	var replay bool
	var replayReport string
	if err := conn.QueryRow(ctx, `select report_id::text,replay from fn_submit_shift($1,$2,$3,$4,'idem-1',$5::jsonb)`, shift, draft, token, revision, req).Scan(&replayReport, &replay); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !replay || replayReport != report {
		t.Fatalf("replay: report=%s replay=%t", replayReport, replay)
	}
	if _, err := conn.Exec(ctx, `select * from fn_submit_shift($1,$2,$3,$4,'idem-1',$5::jsonb)`, shift, draft, token, revision, strings.Replace(req, `"1000"`, `"1001"`, 1)); err == nil {
		t.Fatal("hash mismatch was accepted")
	}
}
