package test

import (
	"context"
	"strings"
	"testing"
)

func TestB1Drafts_ExpiredClaimCannotWriteAndTakeoverFencesRevision(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	defer func() {
		if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), "000006_b1_drafts.down.sql")); err != nil {
			t.Errorf("clean up drafts: %v", err)
		}
	}()
	if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), "000005_b1_shifts.down.sql")); err != nil {
		t.Fatalf("reset shifts: %v", err)
	}
	if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), "000004_b1_catalog.down.sql")); err != nil {
		t.Fatalf("reset catalog: %v", err)
	}
	resetB0Foundation(t, conn, true)
	for _, name := range []string{"000004_b1_catalog.up.sql", "000005_b1_shifts.up.sql", "000006_b1_drafts.up.sql"} {
		if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), name)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}

	const (
		orgID       = "11111111-1111-4111-8111-111111111111"
		stationID   = "22222222-2222-4222-8222-222222222222"
		supervisor  = "33333333-3333-4333-8333-333333333333"
		dispenserID = "44444444-4444-4444-8444-444444444444"
		nozzleID    = "55555555-5555-4555-8555-555555555555"
	)
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`insert into organizations (org_id, name) values ($1, 'Draft Org')`, []any{orgID}},
		{`insert into stations (org_id, station_id, timezone) values ($1, $2, 'Asia/Jakarta')`, []any{orgID, stationID}},
		{`insert into users (user_id, org_id, display_name) values ($1, $2, 'Supervisor')`, []any{supervisor, orgID}},
		{`insert into user_station_roles (org_id, station_id, user_id, role) values ($1, $2, $3, 'Supervisor')`, []any{orgID, stationID, supervisor}},
		{`insert into dispensers (org_id, station_id, dispenser_id) values ($1, $2, $3)`, []any{orgID, stationID, dispenserID}},
		{`insert into nozzles (org_id, station_id, nozzle_id, dispenser_id, meter_max) values ($1, $2, $3, $4, 99999.9)`, []any{orgID, stationID, nozzleID, dispenserID}},
		{`insert into dispenser_nozzle_map (org_id, station_id, dispenser_id, nozzle_id, valid_period) values ($1, $2, $3, $4, tstzrange('2020-01-01', '2030-01-01', '[)'))`, []any{orgID, stationID, dispenserID, nozzleID}},
		{`insert into dispenser_prices (org_id, station_id, nozzle_id, price, valid_period, created_by) values ($1, $2, $3, 10000, tstzrange('2020-01-01', '2030-01-01', '[)'), $4)`, []any{orgID, stationID, nozzleID, supervisor}},
	} {
		if _, err := conn.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed draft data: %v", err)
		}
	}
	for key, value := range map[string]string{
		"app.context_valid": "true", "app.org_id": orgID, "app.station_id": stationID, "app.user_id": supervisor, "app.role": "Supervisor",
	} {
		if _, err := conn.Exec(ctx, `select set_config($1, $2, false)`, key, value); err != nil {
			t.Fatalf("set request context: %v", err)
		}
	}
	var shiftID, draftID, token1 string
	if err := conn.QueryRow(ctx, `select shift_id::text from fn_open_shift($1, $2, '2026-01-01 12:00:00+00', false, null, null, null, null)`, stationID, supervisor).Scan(&shiftID); err != nil {
		t.Fatalf("open shift: %v", err)
	}
	if err := conn.QueryRow(ctx, `select draft_id::text, claim_token::text from fn_claim_draft($1)`, shiftID).Scan(&draftID, &token1); err != nil {
		t.Fatalf("claim draft: %v", err)
	}
	if _, err := conn.Exec(ctx, `update shift_drafts set claim_expires_at = clock_timestamp() - interval '1 second' where draft_id = $1`, draftID); err != nil {
		t.Fatalf("expire claim: %v", err)
	}
	var token2 string
	if err := conn.QueryRow(ctx, `select claim_token::text from fn_claim_draft($1)`, shiftID).Scan(&token2); err != nil {
		t.Fatalf("take over draft: %v", err)
	}
	if token1 == token2 {
		t.Fatal("takeover did not create a new claim token")
	}
	if _, err := conn.Exec(ctx, `select fn_write_draft_reading($1, $2, 2, $3, '1.0', '2.0')`, draftID, token1, nozzleID); err == nil || !strings.Contains(err.Error(), "draft_fence") {
		t.Fatalf("expired claim write: got %v, want draft_fence conflict", err)
	}
	var revision int
	if err := conn.QueryRow(ctx, `select fn_write_draft_reading($1, $2, 3, $3, '1.0', '2.0')`, draftID, token2, nozzleID).Scan(&revision); err != nil {
		t.Fatalf("current claim write: %v", err)
	}
	if revision != 4 {
		t.Fatalf("revision after claim and child write: got %d, want 4", revision)
	}
}
