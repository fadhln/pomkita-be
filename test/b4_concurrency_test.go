package test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestB4Concurrency_ThreeStationsCompleteTwelveShiftsWithoutDeadlock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	setup := openB0Connection(t)
	defer setup.Close(context.Background())
	resetMigrations(t, setup)
	applyB3Migrations(t, setup)
	defer resetB0Foundation(t, setup, true)

	const org = "11111111-1111-4111-8111-111111111111"
	const owner = "99999999-9999-4999-8999-999999999999"
	if _, err := setup.Exec(ctx, `insert into organizations(org_id,name) values($1,'Soak')`, org); err != nil {
		t.Fatalf("seed soak organization: %v", err)
	}
	if _, err := setup.Exec(ctx, `insert into audit_chain_locks(org_id) values($1)`, org); err != nil {
		t.Fatalf("seed audit lock: %v", err)
	}
	if _, err := setup.Exec(ctx, `insert into users(user_id,org_id,email,display_name,password_hash) values($1,$2,'owner@soak.test','Owner','hash')`, owner, org); err != nil {
		t.Fatalf("seed soak owner: %v", err)
	}
	if _, err := setup.Exec(ctx, `insert into threshold_policy_revisions(rev_id,policy_id,org_id,valid_from,created_by) values($1,$2,$3,'2020-01-01',$4)`, uuid.New(), uuid.New(), org, owner); err != nil {
		t.Fatalf("seed soak threshold policy: %v", err)
	}
	if _, err := setup.Exec(ctx, `insert into evidence_policy_revisions(rev_id,policy_id,org_id,valid_from,mode,created_by) values($1,$2,$3,'2020-01-01','opsional',$4)`, uuid.New(), uuid.New(), org, owner); err != nil {
		t.Fatalf("seed soak evidence policy: %v", err)
	}

	type stationFixture struct{ station, supervisor, admin, dispenser, nozzle, tank string }
	fixtures := make([]stationFixture, 3)
	for index := range fixtures {
		fixture := stationFixture{
			station:    uuid.NewString(),
			supervisor: uuid.NewString(),
			admin:      uuid.NewString(),
			dispenser:  uuid.NewString(),
			nozzle:     uuid.NewString(),
			tank:       uuid.NewString(),
		}
		fixtures[index] = fixture
		if _, err := setup.Exec(ctx, `insert into stations(org_id,station_id,timezone) values($1,$2,'UTC')`, org, fixture.station); err != nil {
			t.Fatalf("seed station %d: %v", index, err)
		}
		for _, user := range []struct{ id, email, name, role string }{
			{fixture.supervisor, fmt.Sprintf("supervisor-%d@soak.test", index), "Supervisor", "Supervisor"},
			{fixture.admin, fmt.Sprintf("admin-%d@soak.test", index), "Station Admin", "Station Admin"},
		} {
			if _, err := setup.Exec(ctx, `insert into users(user_id,org_id,email,display_name,password_hash) values($1,$2,$3,$4,'hash')`, user.id, org, user.email, user.name); err != nil {
				t.Fatalf("seed user %d: %v", index, err)
			}
			if _, err := setup.Exec(ctx, `insert into user_station_roles(org_id,station_id,user_id,role) values($1,$2,$3,$4)`, org, fixture.station, user.id, user.role); err != nil {
				t.Fatalf("seed role %d: %v", index, err)
			}
		}
		for _, statement := range []struct {
			query string
			args  []any
		}{
			{`insert into dispensers(org_id,station_id,dispenser_id) values($1,$2,$3)`, []any{org, fixture.station, fixture.dispenser}},
			{`insert into tanks(org_id,station_id,tank_id) values($1,$2,$3)`, []any{org, fixture.station, fixture.tank}},
			{`insert into nozzles(org_id,station_id,nozzle_id,dispenser_id,meter_max) values($1,$2,$3,$4,99999.9)`, []any{org, fixture.station, fixture.nozzle, fixture.dispenser}},
			{`insert into dispenser_nozzle_map(org_id,station_id,dispenser_id,nozzle_id,valid_period) values($1,$2,$3,$4,tstzrange('2020-01-01','2035-01-01','[)'))`, []any{org, fixture.station, fixture.dispenser, fixture.nozzle}},
			{`insert into dispenser_prices(org_id,station_id,nozzle_id,price,valid_period,created_by) values($1,$2,$3,1000,tstzrange('2020-01-01','2035-01-01','[)'),$4)`, []any{org, fixture.station, fixture.nozzle, fixture.supervisor}},
		} {
			if _, err := setup.Exec(ctx, statement.query, statement.args...); err != nil {
				t.Fatalf("seed station catalog %d: %v", index, err)
			}
		}
	}

	errs := make(chan error, len(fixtures))
	var workers sync.WaitGroup
	for _, fixture := range fixtures {
		fixture := fixture
		workers.Add(1)
		go func() {
			defer workers.Done()
			if err := runSoakWorker(ctx, fixture, org); err != nil {
				errs <- err
			}
		}()
	}
	workers.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("soak worker: %v", err)
	}

	var locked int
	if err := setup.QueryRow(ctx, `select count(*) from shifts where org_id=$1 and status='locked'`, org).Scan(&locked); err != nil {
		t.Fatalf("count locked shifts: %v", err)
	}
	if locked != 36 {
		t.Fatalf("locked shifts: got %d, want 36", locked)
	}
	var auditCount, firstSequence, lastSequence int64
	if err := setup.QueryRow(ctx, `select count(*),coalesce(min(org_sequence),0),coalesce(max(org_sequence),0) from audit_log where org_id=$1`, org).Scan(&auditCount, &firstSequence, &lastSequence); err != nil {
		t.Fatalf("read audit sequence: %v", err)
	}
	if auditCount != 72 || firstSequence != 1 || lastSequence != 72 {
		t.Fatalf("audit sequence: count=%d first=%d last=%d", auditCount, firstSequence, lastSequence)
	}
	setSoakContext(t, setup, org, "", owner, "Owner")
	var verified bool
	if err := setup.QueryRow(ctx, `select public.fn_verify_audit_chain()`).Scan(&verified); err != nil || !verified {
		t.Fatalf("verify soak audit chain: verified=%t error=%v", verified, err)
	}
}

func runSoakWorker(ctx context.Context, fixture struct{ station, supervisor, admin, dispenser, nozzle, tank string }, org string) error {
	conn, err := pgx.Connect(ctx, testDatabaseURL())
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())
	setSoakContextValue(ctx, conn, org, fixture.station, fixture.supervisor, "Supervisor")
	for index := 0; index < 12; index++ {
		var shift uuid.UUID
		var stationSequence string
		if err := conn.QueryRow(ctx, `select shift_id,station_seq::text from fn_open_shift($1,$2,$3,false,null,null,null,null)`, fixture.station, fixture.supervisor, time.Date(2026, 1, 1+index, 8, 0, 0, 0, time.UTC)).Scan(&shift, &stationSequence); err != nil {
			return fmt.Errorf("open shift %d: %w", index, err)
		}
		if stationSequence != fmt.Sprintf("%d", index+1) {
			return fmt.Errorf("station sequence %s at shift %d", stationSequence, index)
		}
		var draft, claim uuid.UUID
		var revision int
		if err := conn.QueryRow(ctx, `select draft_id,claim_token,revision from fn_claim_draft($1)`, shift).Scan(&draft, &claim, &revision); err != nil {
			return fmt.Errorf("claim shift %d: %w", index, err)
		}
		request := `{"hash_version":1,"readings":[],"sales":[],"losses":[]}`
		var report uuid.UUID
		if err := conn.QueryRow(ctx, `select report_id from fn_submit_shift($1,$2,$3,$4,$5,$6::jsonb)`, shift, draft, claim, revision, fmt.Sprintf("soak-%d", index), request).Scan(&report); err != nil {
			return fmt.Errorf("submit shift %d: %w", index, err)
		}
		setSoakContextValue(ctx, conn, org, fixture.station, fixture.admin, "Station Admin")
		if err := conn.QueryRow(ctx, `select report_id from fn_ack_shift($1,$2,1,'acked',null,false,null)`, shift, report).Scan(&report); err != nil {
			return fmt.Errorf("ack shift %d: %w", index, err)
		}
		setSoakContextValue(ctx, conn, org, fixture.station, fixture.supervisor, "Supervisor")
	}
	return nil
}

func setSoakContext(t *testing.T, conn *pgx.Conn, org, station, user, role string) {
	t.Helper()
	if err := setSoakContextValue(context.Background(), conn, org, station, user, role); err != nil {
		t.Fatalf("set soak context: %v", err)
	}
}

func setSoakContextValue(ctx context.Context, conn *pgx.Conn, org, station, user, role string) error {
	_, err := conn.Exec(ctx, `select set_config('app.context_valid','true',false),set_config('app.org_id',$1,false),set_config('app.station_id',$2,false),set_config('app.user_id',$3,false),set_config('app.role',$4,false)`, org, station, user, role)
	return err
}
