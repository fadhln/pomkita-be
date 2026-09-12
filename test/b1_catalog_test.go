package test

import (
	"context"
	"strings"
	"testing"
)

func TestB1Catalog_ExclusionCompositeForeignKeyAndClassification(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	defer func() {
		if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), "000005_b1_catalog.down.sql")); err != nil {
			t.Errorf("clean up catalog migration: %v", err)
		}
	}()
	if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), "000005_b1_catalog.down.sql")); err != nil {
		t.Fatalf("reset catalog: %v", err)
	}
	resetB0Foundation(t, conn, true)
	if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), "000005_b1_catalog.up.sql")); err != nil {
		t.Fatalf("apply catalog migration: %v", err)
	}

	var tableCount int
	if err := conn.QueryRow(ctx, `
		select count(*)
		from pg_class c
		join pg_namespace n on n.oid = c.relnamespace
		where n.nspname = 'public'
		  and c.relname = any($1::text[])
	`, []string{"nozzles", "dispensers", "tanks", "nozzle_tank_map", "dispenser_nozzle_map", "dispenser_prices", "meter_reset_events", "nozzle_baseline_revisions", "nozzle_baseline_current"}).Scan(&tableCount); err != nil {
		t.Fatalf("catalog tables are missing: %v", err)
	}
	if tableCount != 9 {
		t.Fatalf("catalog table count: got %d, want 9", tableCount)
	}

	orgID := "11111111-1111-4111-8111-111111111111"
	stationID := "22222222-2222-4222-8222-222222222222"
	dispenserID := "33333333-3333-4333-8333-333333333333"
	nozzleID := "44444444-4444-4444-8444-444444444444"
	userID := "55555555-5555-4555-8555-555555555555"
	otherOrgID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`insert into organizations (org_id, name) values ($1, 'Catalog Org')`, []any{orgID}},
		{`insert into organizations (org_id, name) values ($1, 'Other Org')`, []any{otherOrgID}},
		{`insert into stations (org_id, station_id, timezone) values ($1, $2, 'Asia/Jakarta')`, []any{orgID, stationID}},
		{`insert into users (user_id, org_id, email, display_name, password_hash) values ($1, $2, 'catalog@example.com', 'Catalog User', app.crypt('pw', app.gen_salt('bf')))`, []any{userID, orgID}},
		{`insert into dispensers (org_id, station_id, dispenser_id) values ($1, $2, $3)`, []any{orgID, stationID, dispenserID}},
		{`insert into nozzles (org_id, station_id, nozzle_id, dispenser_id, meter_max) values ($1, $2, $3, $4, 99999.9)`, []any{orgID, stationID, nozzleID, dispenserID}},
	} {
		if _, err := conn.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed catalog rows: %v", err)
		}
	}
	if _, err := conn.Exec(ctx, `
		insert into dispenser_prices (org_id, station_id, nozzle_id, price, valid_period, created_by)
		values ($1, $2, $3, 10000, tstzrange('2026-01-01', '2026-02-01', '[)'), $4),
		       ($1, $2, $3, 11000, tstzrange('2026-01-15', '2026-03-01', '[)'), $4)
	`, orgID, stationID, nozzleID, userID); err == nil || !strings.Contains(err.Error(), "exclusion") {
		t.Fatalf("overlapping price period: got %v, want exclusion violation", err)
	}
	if _, err := conn.Exec(ctx, `
		insert into dispensers (org_id, station_id, dispenser_id)
		values ($1, $2, $3)
	`, otherOrgID, stationID, dispenserID); err == nil {
		t.Fatal("cross-tenant dispenser row was accepted")
	}

	var classified bool
	if err := conn.QueryRow(ctx, `
		select not exists (
			select 1
			from pg_class c
			join pg_namespace n on n.oid = c.relnamespace
			where n.nspname not in ('pg_catalog', 'information_schema')
			  and n.nspname not like 'pg_toast%'
			  and c.relkind in ('r', 'v', 'm', 'S')
			  and c.relname <> 'schema_migrations'
			  and c.relname not in ('audit_chain_locks', 'jwt_keys', 'organizations', 'procedure_registry', 'sessions', 'stations', 'user_station_roles', 'users', 'nozzles', 'dispensers', 'tanks', 'nozzle_tank_map', 'dispenser_nozzle_map', 'dispenser_prices', 'meter_reset_events', 'nozzle_baseline_revisions', 'nozzle_baseline_current')
		)
	`).Scan(&classified); err != nil {
		t.Fatalf("catalog classification query: %v", err)
	}
	if !classified {
		t.Fatal("catalog contains an unclassified relation")
	}
}
