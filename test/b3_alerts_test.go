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
