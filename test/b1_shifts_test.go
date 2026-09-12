package test

import (
	"context"
	"strings"
	"testing"
)

func TestB1Shifts_OpenAllocatesSequenceSnapshotAndTransitions(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	defer func() {
		if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), "000006_b1_shifts.down.sql")); err != nil {
			t.Errorf("clean up shifts: %v", err)
		}
	}()
	if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), "000006_b1_shifts.down.sql")); err != nil {
		t.Fatalf("reset shifts: %v", err)
	}
	if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), "000005_b1_catalog.down.sql")); err != nil {
		t.Fatalf("reset catalog: %v", err)
	}
	resetB0Foundation(t, conn, true)
	if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), "000005_b1_catalog.up.sql")); err != nil {
		t.Fatalf("apply catalog: %v", err)
	}
	if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), "000006_b1_shifts.up.sql")); err != nil {
		t.Fatalf("apply shifts: %v", err)
	}

	const (
		orgID       = "11111111-1111-4111-8111-111111111111"
		stationID   = "22222222-2222-4222-8222-222222222222"
		supervisor  = "33333333-3333-4333-8333-333333333333"
		dispenserID = "44444444-4444-4444-8444-444444444444"
		nozzleID    = "55555555-5555-4555-8555-555555555555"
		tankID      = "66666666-6666-4666-8666-666666666666"
	)
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`insert into organizations (org_id, name) values ($1, 'Shift Org')`, []any{orgID}},
		{`insert into stations (org_id, station_id, timezone) values ($1, $2, 'Asia/Jakarta')`, []any{orgID, stationID}},
		{`insert into users (user_id, org_id, email, display_name, password_hash) values ($1, $2, 'supervisor@example.com', 'Supervisor', app.crypt('pw', app.gen_salt('bf')))`, []any{supervisor, orgID}},
		{`insert into user_station_roles (org_id, station_id, user_id, role) values ($1, $2, $3, 'Supervisor')`, []any{orgID, stationID, supervisor}},
		{`insert into dispensers (org_id, station_id, dispenser_id) values ($1, $2, $3)`, []any{orgID, stationID, dispenserID}},
		{`insert into tanks (org_id, station_id, tank_id) values ($1, $2, $3)`, []any{orgID, stationID, tankID}},
		{`insert into nozzles (org_id, station_id, nozzle_id, dispenser_id, meter_max) values ($1, $2, $3, $4, 99999.9)`, []any{orgID, stationID, nozzleID, dispenserID}},
		{`insert into nozzle_tank_map (org_id, station_id, nozzle_id, tank_id, valid_period) values ($1, $2, $3, $4, tstzrange('2020-01-01', '2030-01-01', '[)'))`, []any{orgID, stationID, nozzleID, tankID}},
		{`insert into dispenser_nozzle_map (org_id, station_id, dispenser_id, nozzle_id, valid_period) values ($1, $2, $3, $4, tstzrange('2020-01-01', '2030-01-01', '[)'))`, []any{orgID, stationID, dispenserID, nozzleID}},
		{`insert into dispenser_prices (org_id, station_id, nozzle_id, price, valid_period, created_by) values ($1, $2, $3, 10000, tstzrange('2020-01-01', '2030-01-01', '[)'), $4)`, []any{orgID, stationID, nozzleID, supervisor}},
	} {
		if _, err := conn.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed shift data: %v", err)
		}
	}
	for key, value := range map[string]string{
		"app.context_valid": "true", "app.org_id": orgID, "app.station_id": stationID, "app.user_id": supervisor, "app.role": "Supervisor",
	} {
		if _, err := conn.Exec(ctx, `select set_config($1, $2, false)`, key, value); err != nil {
			t.Fatalf("set request context: %v", err)
		}
	}

	var shiftID, hash string
	var sequence int64
	var businessDate string
	if err := conn.QueryRow(ctx, `
		select shift_id::text, station_seq, business_date::text, encode(shift_price_map_hash, 'hex')
		from fn_open_shift($1, $2, '2026-01-01 23:30:00+00', false, null, null, null, null)
	`, stationID, supervisor).Scan(&shiftID, &sequence, &businessDate, &hash); err != nil {
		t.Fatalf("open shift: %v", err)
	}
	if sequence != 1 || businessDate != "2026-01-02" || hash == "" {
		t.Fatalf("open result: sequence=%d business_date=%q hash=%q", sequence, businessDate, hash)
	}

	if _, err := conn.Exec(ctx, `select fn_transition_shift($1, 'submitting', null)`, shiftID); err != nil {
		t.Fatalf("valid transition: %v", err)
	}
	var status string
	if err := conn.QueryRow(ctx, `select status::text from shifts where shift_id = $1`, shiftID).Scan(&status); err != nil {
		t.Fatalf("read transition: %v", err)
	}
	if status != "submitting" {
		t.Fatalf("status after transition: got %q, want submitting", status)
	}
	if _, err := conn.Exec(ctx, `select fn_transition_shift($1, 'locked', null)`, shiftID); err == nil || !strings.Contains(err.Error(), "transition") {
		t.Fatalf("invalid transition: got %v, want transition error", err)
	}
}
