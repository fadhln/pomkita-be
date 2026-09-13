package test

import (
	"context"
	"testing"
)

func TestB3Backfill_SubmitIsIdempotentAndCarriesForwardMeter(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	resetMigrations(t, conn)
	applyB3Migrations(t, conn)
	const (
		org             = "11111111-1111-4111-8111-111111111111"
		station         = "22222222-2222-4222-8222-222222222222"
		supervisor      = "33333333-3333-4333-8333-333333333333"
		owner           = "44444444-4444-4444-8444-444444444444"
		dispenser       = "55555555-5555-4555-8555-555555555555"
		nozzle          = "66666666-6666-4666-8666-666666666666"
		previous        = "77777777-7777-4777-8777-777777777777"
		previousReport  = "88888888-8888-4888-8888-888888888888"
		previousSet     = "99999999-9999-4999-8999-999999999999"
		previousReading = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		policy          = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
		threshold       = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
		evidence        = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	)
	if _, err := conn.Exec(ctx, `select set_config('app.context_valid','true',false),set_config('app.org_id',$1,false),set_config('app.station_id',$2,false),set_config('app.user_id',$3,false),set_config('app.role','Supervisor',false),set_config('app.transition','submit_shift',false)`, org, station, supervisor); err != nil {
		t.Fatalf("set backfill seed context: %v", err)
	}
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`insert into organizations(org_id,name) values($1,'Backfill')`, []any{org}},
		{`insert into stations(org_id,station_id,timezone) values($1,$2,'UTC')`, []any{org, station}},
		{`insert into users(user_id,org_id,email,display_name,password_hash) values($1,$2,'supervisor@example.com','Supervisor','hash'),($3,$2,'owner@example.com','Owner','hash')`, []any{supervisor, org, owner}},
		{`insert into user_station_roles(org_id,station_id,user_id,role) values($1,$2,$3,'Supervisor'),($1,$2,$4,'Owner')`, []any{org, station, supervisor, owner}},
		{`insert into dispensers(org_id,station_id,dispenser_id) values($1,$2,$3)`, []any{org, station, dispenser}},
		{`insert into nozzles(org_id,station_id,nozzle_id,dispenser_id,meter_max) values($1,$2,$3,$4,99999.9)`, []any{org, station, nozzle, dispenser}},
		{`insert into dispenser_nozzle_map(org_id,station_id,dispenser_id,nozzle_id,valid_period) values($1,$2,$3,$4,tstzrange('2020-01-01','2030-01-01','[)'))`, []any{org, station, dispenser, nozzle}},
		{`insert into dispenser_prices(org_id,station_id,nozzle_id,price,valid_period,created_by) values($1,$2,$3,10000,tstzrange('2020-01-01','2030-01-01','[)'),$4)`, []any{org, station, nozzle, supervisor}},
		{`insert into threshold_policy_revisions(rev_id,policy_id,org_id,valid_from,created_by) values($1,$2,$3,'2020-01-01',$4)`, []any{threshold, policy, org, supervisor}},
		{`insert into evidence_policy_revisions(rev_id,policy_id,org_id,valid_from,mode,created_by) values($1,$2,$3,'2020-01-01','opsional',$4)`, []any{evidence, policy, org, supervisor}},
		{`insert into shifts(shift_id,org_id,station_id,station_seq,supervisor_id,opened_at,timezone_snapshot,business_date,shift_price_map_snapshot,shift_price_map_hash,status) values($1,$2,$3,1,$4,'2025-12-31','UTC','2025-12-31',jsonb_build_object('hash_version',1,'items',jsonb_build_array(jsonb_build_object('nozzle_id',$5::text,'dispenser_id',$6::text,'meter_max','99999.9','modulus','100000.0','price','10000'))),app.digest('{}','sha256'),'locked')`, []any{previous, org, station, supervisor, nozzle, dispenser}},
		{`insert into policy_snapshot_sets(set_id,org_id,station_id,shift_id) values($1,$2,$3,$4)`, []any{previousSet, org, station, previous}},
		{`insert into policy_snapshot_items(item_id,org_id,station_id,shift_id,set_id,policy_kind,policy_id,rev_id,scope,payload,payload_hash) values($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,'evidence',$6::uuid,$6::uuid,'organization','{"hash_version":1,"mode":"opsional","types":[]}',app.digest($6::text,'sha256'))`, []any{previousReading, org, station, previous, previousSet, evidence}},
	} {
		if _, err := conn.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed backfill: %v", err)
		}
	}
	if _, err := conn.Exec(ctx, `select set_config('app.transition','',false)`); err != nil {
		t.Fatalf("clear seed transition: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into shift_reports(report_id,org_id,station_id,shift_id,version_no,status,submitted_by,policy_snapshot_set_id) values($1,$2,$3,$4,1,'locked',$5,$6)`, previousReport, org, station, previous, supervisor, previousSet); err != nil {
		t.Fatalf("insert previous report: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into dispenser_readings(reading_id,org_id,station_id,shift_id,report_id,nozzle_id,meter_start,meter_end,price_used,expected_sale_rupiah) values($1,$2,$3,$4,$5,$6,10,20,10000,100000)`, previousReading, org, station, previous, previousReport, nozzle); err != nil {
		t.Fatalf("insert previous reading: %v", err)
	}
	if _, err := conn.Exec(ctx, `select set_config('app.transition','submit_shift',false)`); err != nil {
		t.Fatalf("set pointer transition: %v", err)
	}
	if _, err := conn.Exec(ctx, `update shifts set current_report_id=$1 where org_id=$2 and station_id=$3 and shift_id=$4`, previousReport, org, station, previous); err != nil {
		t.Fatalf("set previous report pointer: %v", err)
	}
	var shift, draft, claim string
	var revision int
	if err := conn.QueryRow(ctx, `select shift_id::text from fn_open_shift($1,$2,'2026-01-01 12:00+00',true,'2025-12-30',1,$3,'late report')`, station, supervisor, owner).Scan(&shift); err != nil {
		t.Fatalf("open backfill: %v", err)
	}
	if err := conn.QueryRow(ctx, `select draft_id::text,claim_token::text,revision from fn_claim_draft($1)`, shift).Scan(&draft, &claim, &revision); err != nil {
		t.Fatalf("claim backfill: %v", err)
	}
	request := `{"hash_version":1,"readings":[],"sales":[],"losses":[]}`
	var report string
	if err := conn.QueryRow(ctx, `select report_id::text from fn_submit_shift($1,$2,$3,$4,'backfill-1',$5::jsonb)`, shift, draft, claim, revision, request).Scan(&report); err != nil {
		t.Fatalf("submit backfill: %v", err)
	}
	var observed, carried bool
	if err := conn.QueryRow(ctx, `select observed,is_carried_forward from dispenser_readings where report_id=$1`, report).Scan(&observed, &carried); err != nil {
		t.Fatalf("read carried reading: %v", err)
	}
	if observed || !carried {
		t.Fatalf("carried reading flags: observed=%t carried=%t", observed, carried)
	}
	var replayReport string
	var replay bool
	if err := conn.QueryRow(ctx, `select report_id::text,replay from fn_submit_shift($1,$2,$3,$4,'backfill-1',$5::jsonb)`, shift, draft, claim, revision, request).Scan(&replayReport, &replay); err != nil {
		t.Fatalf("retry backfill: %v", err)
	}
	if !replay || replayReport != report {
		t.Fatalf("backfill retry: report=%s replay=%t", replayReport, replay)
	}
	var count int
	if err := conn.QueryRow(ctx, `select count(*) from shift_reports where shift_id=$1`, shift).Scan(&count); err != nil {
		t.Fatalf("count backfill reports: %v", err)
	}
	if count != 1 {
		t.Fatalf("backfill reports: got %d, want 1", count)
	}
}
