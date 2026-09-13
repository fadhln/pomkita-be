package test

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestB3StarvationAlert_IsIdempotentAndClearsOnLock(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	resetMigrations(t, conn)
	applyB3Migrations(t, conn)

	const (
		org     = "11111111-1111-4111-8111-111111111111"
		station = "22222222-2222-4222-8222-222222222222"
		user    = "33333333-3333-4333-8333-333333333333"
		rule    = "44444444-4444-4444-8444-444444444444"
		shift   = "55555555-5555-4555-8555-555555555555"
	)
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := old.Add(25 * time.Hour)
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`insert into organizations(org_id,name) values($1,'B3')`, []any{org}},
		{`insert into stations(org_id,station_id,timezone) values($1,$2,'UTC')`, []any{org, station}},
		{`insert into users(user_id,org_id,email,display_name,password_hash) values($1,$2,'b3@example.com','Owner','hash')`, []any{user, org}},
		{`insert into user_station_roles(org_id,station_id,user_id,role) values($1,$2,$3,'Owner')`, []any{org, station, user}},
		{`insert into alert_rules(rule_id,org_id,station_id,rule_type,alert_key,threshold,enabled,channel,created_by) values($1,$2,$3,'starvation','starvation',24,true,'in_app',$4)`, []any{rule, org, station, user}},
		{`insert into shifts(shift_id,org_id,station_id,station_seq,supervisor_id,opened_at,timezone_snapshot,business_date,shift_price_map_snapshot,shift_price_map_hash,status) values($1,$2,$3,1,$4,$5,'UTC',$6,'{}',app.digest('{}','sha256'),'awaiting_confirmation')`, []any{shift, org, station, user, old, old.Format("2006-01-02")}},
	} {
		if _, err := conn.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed %s: %v", statement.query, err)
		}
	}

	var first int
	if err := conn.QueryRow(ctx, `select fn_run_starvation_alerts($1)`, now).Scan(&first); err != nil {
		t.Fatalf("first starvation run: %v", err)
	}
	if first != 1 {
		t.Fatalf("first starvation run: got %d, want 1", first)
	}
	var second int
	if err := conn.QueryRow(ctx, `select fn_run_starvation_alerts($1)`, now).Scan(&second); err != nil {
		t.Fatalf("retry starvation run: %v", err)
	}
	if second != 0 {
		t.Fatalf("retry starvation run: got %d, want 0 new events", second)
	}
	var fired int
	if err := conn.QueryRow(ctx, `select count(*) from alert_events where rule_id=$1 and event_type='fired'`, rule).Scan(&fired); err != nil {
		t.Fatalf("count fired events: %v", err)
	}
	if fired != 1 {
		t.Fatalf("fired events: got %d, want 1", fired)
	}

	if _, err := conn.Exec(ctx, `select set_config('app.context_valid','true',false), set_config('app.org_id',$1,false), set_config('app.station_id',$2,false), set_config('app.user_id',$3,false), set_config('app.role','Owner',false)`, org, station, user); err != nil {
		t.Fatalf("set transition context: %v", err)
	}
	if _, err := conn.Exec(ctx, `select fn_transition_shift($1,'locked','test')`, shift); err != nil {
		t.Fatalf("lock shift: %v", err)
	}
	var cleared int
	if err := conn.QueryRow(ctx, `select count(*) from alert_events where rule_id=$1 and event_type='cleared'`, rule).Scan(&cleared); err != nil {
		t.Fatalf("count cleared events: %v", err)
	}
	if cleared != 1 {
		t.Fatalf("cleared events: got %d, want 1", cleared)
	}
}

func TestB3AlertEventConstraints_EnforceDedupeAndSourceVersionRules(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	resetMigrations(t, conn)
	applyB3Migrations(t, conn)
	const (
		org     = "11111111-1111-4111-8111-111111111111"
		station = "22222222-2222-4222-8222-222222222222"
		user    = "33333333-3333-4333-8333-333333333333"
		rule    = "44444444-4444-4444-8444-444444444444"
		shift   = "55555555-5555-4555-8555-555555555555"
		event   = "66666666-6666-4666-8666-666666666666"
	)
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`insert into organizations(org_id,name) values($1,'Alert constraints')`, []any{org}},
		{`insert into stations(org_id,station_id,timezone) values($1,$2,'UTC')`, []any{org, station}},
		{`insert into users(user_id,org_id,email,display_name,password_hash) values($1,$2,'alert@example.com','Owner','hash')`, []any{user, org}},
		{`insert into alert_rules(rule_id,org_id,station_id,rule_type,alert_key,threshold,enabled,channel,created_by) values($1,$2,$3,'starvation','starvation',24,true,'in_app',$4)`, []any{rule, org, station, user}},
		{`insert into shifts(shift_id,org_id,station_id,station_seq,supervisor_id,opened_at,timezone_snapshot,business_date,shift_price_map_snapshot,shift_price_map_hash) values($1,$2,$3,1,$4,'2026-01-01','UTC','2026-01-01','{}',app.digest('{}','sha256'))`, []any{shift, org, station, user}},
	} {
		if _, err := conn.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed alert constraints: %v", err)
		}
	}
	valid := `insert into alert_events(event_id,org_id,station_id,rule_id,subject_kind,subject_id,event_type,period_start,related_fired_event_id,source_kind,source_id,source_at) values($1,$2,$3,$4,'shift',$5,'fired','2026-01-01',$1,'scheduler',$5,'2026-01-01')`
	if _, err := conn.Exec(ctx, valid, event, org, station, rule, shift); err != nil {
		t.Fatalf("insert valid alert: %v", err)
	}
	if _, err := conn.Exec(ctx, valid, event, org, station, rule, shift); err == nil {
		t.Fatal("duplicate fired alert was accepted")
	}
	if _, err := conn.Exec(ctx, `insert into alert_events(event_id,org_id,station_id,rule_id,subject_kind,subject_id,event_type,period_start,related_fired_event_id,source_kind,source_id,source_version_no,source_at) values(gen_random_uuid(),$1,$2,$3,'shift',$4,'fired','2026-01-01',gen_random_uuid(),'scheduler',$4,1,'2026-01-01')`, org, station, rule, shift); err == nil {
		t.Fatal("scheduler alert accepted a source version")
	}
	if _, err := conn.Exec(ctx, `insert into alert_events(event_id,org_id,station_id,rule_id,subject_kind,subject_id,event_type,period_start,related_fired_event_id,source_kind,source_id,source_at) values(gen_random_uuid(),$1,$2,$3,'shift',$4,'fired','2026-01-01',gen_random_uuid(),'report',$4,'2026-01-01')`, org, station, rule, shift); err == nil {
		t.Fatal("report alert accepted a null source version")
	}
}

func TestB3VarianceAlert_IsOptInAndKeepsNegativeVariance(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	resetMigrations(t, conn)
	applyB3Migrations(t, conn)
	const (
		org       = "11111111-1111-4111-8111-111111111111"
		station   = "22222222-2222-4222-8222-222222222222"
		user      = "33333333-3333-4333-8333-333333333333"
		dispenser = "44444444-4444-4444-8444-444444444444"
		nozzle    = "55555555-5555-4555-8555-555555555555"
		shift     = "66666666-6666-4666-8666-666666666666"
		report    = "77777777-7777-4777-8777-777777777777"
		setID     = "88888888-8888-4888-8888-888888888888"
		threshold = "99999999-9999-4999-8999-999999999999"
		evidence  = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		rule      = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	)
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`insert into organizations(org_id,name) values($1,'Variance')`, []any{org}},
		{`insert into stations(org_id,station_id,timezone) values($1,$2,'UTC')`, []any{org, station}},
		{`insert into users(user_id,org_id,email,display_name,password_hash) values($1,$2,'variance@example.com','Owner','hash')`, []any{user, org}},
		{`insert into dispensers(org_id,station_id,dispenser_id) values($1,$2,$3)`, []any{org, station, dispenser}},
		{`insert into nozzles(org_id,station_id,nozzle_id,dispenser_id,meter_max) values($1,$2,$3,$4,99999.9)`, []any{org, station, nozzle, dispenser}},
		{`insert into shifts(shift_id,org_id,station_id,station_seq,supervisor_id,opened_at,timezone_snapshot,business_date,shift_price_map_snapshot,shift_price_map_hash) values($1,$2,$3,1,$4,'2026-01-01','UTC','2026-01-01','{}',app.digest('{}','sha256'))`, []any{shift, org, station, user}},
		{`insert into policy_snapshot_sets(set_id,org_id,station_id,shift_id) values($1,$2,$3,$4)`, []any{setID, org, station, shift}},
		{`insert into policy_snapshot_items(item_id,org_id,station_id,shift_id,set_id,policy_kind,policy_id,rev_id,scope,payload,payload_hash) values(gen_random_uuid(),$1::uuid,$2::uuid,$3::uuid,$4::uuid,'threshold',$5::uuid,$5::uuid,'organization','{"hash_version":1,"variance_rupiah_threshold":"10"}',app.digest($5::text,'sha256')), (gen_random_uuid(),$1::uuid,$2::uuid,$3::uuid,$4::uuid,'evidence',$6::uuid,$6::uuid,'organization','{"hash_version":1,"mode":"opsional","types":[]}',app.digest($6::text,'sha256'))`, []any{org, station, shift, setID, threshold, evidence}},
		{`insert into alert_rules(rule_id,org_id,station_id,rule_type,alert_key,threshold,enabled,channel,created_by) values($1,$2,$3,'variance','variance',10,true,'in_app',$4)`, []any{rule, org, station, user}},
	} {
		if _, err := conn.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed variance: %v", err)
		}
	}
	if _, err := conn.Exec(ctx, `select set_config('app.context_valid','true',false),set_config('app.org_id',$1,false),set_config('app.station_id',$2,false),set_config('app.user_id',$3,false),set_config('app.role','Owner',false)`, org, station, user); err != nil {
		t.Fatalf("set variance context: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into shift_reports(report_id,org_id,station_id,shift_id,version_no,status,submitted_by,policy_snapshot_set_id) values($1,$2,$3,$4,1,'submitted',$5,$6)`, report, org, station, shift, user, setID); err != nil {
		t.Fatalf("insert variance report: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into dispenser_readings(org_id,station_id,shift_id,report_id,nozzle_id,meter_start,meter_end,price_used,expected_sale_rupiah) values($1,$2,$3,$4,$5,0,1,1000,1000)`, org, station, shift, report, nozzle); err != nil {
		t.Fatalf("insert variance reading: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into sales_declared(org_id,station_id,shift_id,report_id,dispenser_id,cash_amount,cashless_amount,created_by) values($1,$2,$3,$4,$5,1200,0,$6)`, org, station, shift, report, dispenser, user); err != nil {
		t.Fatalf("insert variance sale: %v", err)
	}
	var eventID string
	if err := conn.QueryRow(ctx, `select fn_evaluate_variance_alert($1)::text`, report).Scan(&eventID); err != nil {
		t.Fatalf("evaluate variance: %v", err)
	}
	if eventID == "" {
		t.Fatal("negative variance did not fire")
	}
	var count int
	if err := conn.QueryRow(ctx, `select count(*) from alert_events where rule_id=$1 and event_type='fired'`, rule).Scan(&count); err != nil {
		t.Fatalf("count variance alerts: %v", err)
	}
	if count != 1 {
		t.Fatalf("variance alerts: got %d, want 1", count)
	}
}

func applyB3Migrations(t *testing.T, conn *pgx.Conn) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(repositoryRoot(t), "migrations"))
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	var names []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".up.sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if _, err := conn.Exec(context.Background(), readMigration(t, repositoryRoot(t), name)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
}
