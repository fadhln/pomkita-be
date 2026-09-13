package test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestB3EvidenceWajib_RejectsMissingEvidenceAndException(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	resetMigrations(t, conn)
	applyB3Migrations(t, conn)
	setEvidenceContext(t, conn)
	seedEvidenceReport(t, conn, "wajib")

	if _, err := conn.Exec(ctx, `select fn_validate_evidence($1)`, evidenceReport); err == nil {
		t.Fatal("wajib validation accepted missing evidence")
	}
	if _, err := conn.Exec(ctx, `insert into loss_exception(exception_id,org_id,station_id,shift_id,report_id,loss_row_id,reason,actor_user_id) values($1,$2,$3,$4,$5,$6,'exception',$7)`, evidenceException, evidenceOrg, evidenceStation, evidenceShift, evidenceReport, evidenceLoss, evidenceUser); err == nil {
		t.Fatal("wajib accepted a loss exception")
	}
}

func TestB3EvidenceOpsional_RequiresExceptionAndRejectsExceptionWithEvidence(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	resetMigrations(t, conn)
	applyB3Migrations(t, conn)
	setEvidenceContext(t, conn)
	seedEvidenceReport(t, conn, "opsional")

	if _, err := conn.Exec(ctx, `select fn_validate_evidence($1)`, evidenceReport); err == nil {
		t.Fatal("opsional validation accepted missing exception")
	}
	if _, err := conn.Exec(ctx, `insert into loss_exception(exception_id,org_id,station_id,shift_id,report_id,loss_row_id,reason,actor_user_id) values($1,$2,$3,$4,$5,$6,'exception',$7)`, evidenceException, evidenceOrg, evidenceStation, evidenceShift, evidenceReport, evidenceLoss, evidenceUser); err != nil {
		t.Fatalf("insert optional exception: %v", err)
	}
	if _, err := conn.Exec(ctx, `select fn_validate_evidence($1)`, evidenceReport); err != nil {
		t.Fatalf("opsional validation with exception: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into loss_entries(row_id,org_id,station_id,shift_id,report_id,version_no,loss_id,direction,reason_code,liters,created_by) values($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,1,$1::uuid,'loss','test',1,$6::uuid)`, evidenceLossWithEvidence, evidenceOrg, evidenceStation, evidenceShift, evidenceReport, evidenceUser); err != nil {
		t.Fatalf("insert evidence loss: %v", err)
	}

	if _, err := conn.Exec(ctx, `insert into evidence_event(evidence_event_id,evidence_id,org_id,station_id,shift_id,report_id,loss_row_id,event_seq,event_type,evidence_type,object_key,content_hash,size_bytes,mime,actor_user_id) values($1,$2,$3,$4,$5,$6,$7,1,'finalized','photo','object',decode(repeat('00',32),'hex'),10,'image/jpeg',$8)`, evidenceEvent, evidenceID, evidenceOrg, evidenceStation, evidenceShift, evidenceReport, evidenceLossWithEvidence, evidenceUser); err != nil {
		t.Fatalf("insert finalized evidence: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into loss_exception(exception_id,org_id,station_id,shift_id,report_id,loss_row_id,reason,actor_user_id) values($1,$2,$3,$4,$5,$6,'exception',$7)`, evidenceExceptionWithEvidence, evidenceOrg, evidenceStation, evidenceShift, evidenceReport, evidenceLossWithEvidence, evidenceUser); err == nil {
		t.Fatal("opsional accepted exception with finalized evidence")
	}
}

func TestB3Submit_UsesEvidenceValidator(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	resetMigrations(t, conn)
	applyB3Migrations(t, conn)
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`insert into organizations(org_id,name) values($1,'Submit Evidence')`, []any{evidenceOrg}},
		{`insert into stations(org_id,station_id,timezone) values($1,$2,'UTC')`, []any{evidenceOrg, evidenceStation}},
		{`insert into users(user_id,org_id,email,display_name,password_hash) values($1,$2,'submit-evidence@example.com','Supervisor','hash')`, []any{evidenceUser, evidenceOrg}},
		{`insert into user_station_roles(org_id,station_id,user_id,role) values($1,$2,$3,'Supervisor')`, []any{evidenceOrg, evidenceStation, evidenceUser}},
		{`insert into dispensers(org_id,station_id,dispenser_id) values($1,$2,$3)`, []any{evidenceOrg, evidenceStation, submitDispenser}},
		{`insert into nozzles(org_id,station_id,nozzle_id,dispenser_id,meter_max) values($1,$2,$3,$4,99999.9)`, []any{evidenceOrg, evidenceStation, submitNozzle, submitDispenser}},
		{`insert into dispenser_nozzle_map(org_id,station_id,dispenser_id,nozzle_id,valid_period) values($1,$2,$3,$4,tstzrange('2020-01-01','2030-01-01','[)'))`, []any{evidenceOrg, evidenceStation, submitDispenser, submitNozzle}},
		{`insert into dispenser_prices(org_id,station_id,nozzle_id,price,valid_period,created_by) values($1,$2,$3,10000,tstzrange('2020-01-01','2030-01-01','[)'),$4)`, []any{evidenceOrg, evidenceStation, submitNozzle, evidenceUser}},
		{`insert into threshold_policy_revisions(rev_id,policy_id,org_id,valid_from,created_by) values($1,$2,$3,'2020-01-01',$4)`, []any{submitThreshold, submitPolicy, evidenceOrg, evidenceUser}},
		{`insert into evidence_policy_revisions(rev_id,policy_id,org_id,valid_from,mode,created_by) values($1,$2,$3,'2020-01-01','wajib',$4)`, []any{submitEvidencePolicy, submitPolicy, evidenceOrg, evidenceUser}},
		{`insert into evidence_policy_types(org_id,rev_id,evidence_type,minimum_count_per_loss,accepted_mime_types) values($1,$2,'photo',1,array['image/jpeg'])`, []any{evidenceOrg, submitEvidencePolicy}},
	} {
		if _, err := conn.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed submit evidence: %v", err)
		}
	}
	setEvidenceContext(t, conn)
	var shift, draft, claim string
	var revision int
	if err := conn.QueryRow(ctx, `select shift_id::text from fn_open_shift($1,$2,'2026-01-01 12:00+00',false,null,null,null,null)`, evidenceStation, evidenceUser).Scan(&shift); err != nil {
		t.Fatalf("open evidence shift: %v", err)
	}
	if err := conn.QueryRow(ctx, `select draft_id::text,claim_token::text,revision from fn_claim_draft($1)`, shift).Scan(&draft, &claim, &revision); err != nil {
		t.Fatalf("claim evidence shift: %v", err)
	}
	request := `{"hash_version":1,"readings":[{"nozzle_id":"` + submitNozzle + `","meter_start":"0.0","meter_end":"1.0"}],"losses":[{"loss_id":"` + evidenceLoss + `","direction":"loss","reason_code":"test","liters":"1.00"}]}`
	if _, err := conn.Exec(ctx, `select * from fn_submit_shift($1,$2,$3,$4,'submit-evidence',$5::jsonb)`, shift, draft, claim, revision, request); err == nil {
		t.Fatal("submit accepted missing wajib evidence")
	}
}

func setEvidenceContext(t *testing.T, conn *pgx.Conn) {
	t.Helper()
	if _, err := conn.Exec(context.Background(), `select set_config('app.context_valid','true',false), set_config('app.org_id',$1,false), set_config('app.station_id',$2,false), set_config('app.user_id',$3,false), set_config('app.role','Supervisor',false)`, evidenceOrg, evidenceStation, evidenceUser); err != nil {
		t.Fatalf("set evidence context: %v", err)
	}
}

const (
	evidenceOrg                   = "11111111-1111-4111-8111-111111111111"
	evidenceStation               = "22222222-2222-4222-8222-222222222222"
	evidenceUser                  = "33333333-3333-4333-8333-333333333333"
	evidenceShift                 = "44444444-4444-4444-8444-444444444444"
	evidenceReport                = "55555555-5555-4555-8555-555555555555"
	evidencePolicySet             = "66666666-6666-4666-8666-666666666666"
	evidencePolicyItem            = "77777777-7777-4777-8777-777777777777"
	evidenceLoss                  = "88888888-8888-4888-8888-888888888888"
	evidenceLossWithEvidence      = "99999999-9999-4999-8999-999999999999"
	evidenceException             = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	evidenceEvent                 = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	evidenceID                    = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	evidenceExceptionWithEvidence = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	submitDispenser               = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	submitNozzle                  = "ffffffff-ffff-4fff-8fff-ffffffffffff"
	submitThreshold               = "12121212-1212-4121-8121-121212121212"
	submitEvidencePolicy          = "13131313-1313-4131-8131-131313131313"
	submitPolicy                  = "14141414-1414-4141-8141-141414141414"
)

func seedEvidenceReport(t *testing.T, conn *pgx.Conn, mode string) {
	t.Helper()
	ctx := context.Background()
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`insert into organizations(org_id,name) values($1,'Evidence')`, []any{evidenceOrg}},
		{`insert into stations(org_id,station_id,timezone) values($1,$2,'UTC')`, []any{evidenceOrg, evidenceStation}},
		{`insert into users(user_id,org_id,email,display_name,password_hash) values($1,$2,'evidence@example.com','Supervisor','hash')`, []any{evidenceUser, evidenceOrg}},
		{`insert into shifts(shift_id,org_id,station_id,station_seq,supervisor_id,opened_at,timezone_snapshot,business_date,shift_price_map_snapshot,shift_price_map_hash) values($1,$2,$3,1,$4,'2026-01-01','UTC','2026-01-01','{}',app.digest('{}','sha256'))`, []any{evidenceShift, evidenceOrg, evidenceStation, evidenceUser}},
		{`insert into policy_snapshot_sets(set_id,org_id,station_id,shift_id) values($1,$2,$3,$4)`, []any{evidencePolicySet, evidenceOrg, evidenceStation, evidenceShift}},
		{`insert into policy_snapshot_items(item_id,org_id,station_id,shift_id,set_id,policy_kind,policy_id,rev_id,scope,payload,payload_hash) values($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,'evidence',$6::uuid,$6::uuid,'organization',jsonb_build_object('hash_version',1,'mode',$7::text,'types',jsonb_build_array(jsonb_build_object('type','photo','minimum_count_per_loss',1,'accepted_mime_types',jsonb_build_array('image/jpeg')))),app.digest($6::text,'sha256'))`, []any{evidencePolicyItem, evidenceOrg, evidenceStation, evidenceShift, evidencePolicySet, evidencePolicyItem, mode}},
		{`insert into shift_reports(report_id,org_id,station_id,shift_id,version_no,status,submitted_by,policy_snapshot_set_id) values($1,$2,$3,$4,1,'submitted',$5,$6)`, []any{evidenceReport, evidenceOrg, evidenceStation, evidenceShift, evidenceUser, evidencePolicySet}},
		{`insert into loss_identity(loss_id,org_id,station_id,created_by) values($1,$2,$3,$4),($5,$2,$3,$4)`, []any{evidenceLoss, evidenceOrg, evidenceStation, evidenceUser, evidenceLossWithEvidence}},
		{`insert into loss_entries(row_id,org_id,station_id,shift_id,report_id,version_no,loss_id,direction,reason_code,liters,created_by) values($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,1,$1::uuid,'loss','test',1,$6::uuid)`, []any{evidenceLoss, evidenceOrg, evidenceStation, evidenceShift, evidenceReport, evidenceUser}},
	} {
		if _, err := conn.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed evidence query %s: %v", statement.query, err)
		}
	}
}
