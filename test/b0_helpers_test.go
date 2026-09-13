package test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestResetB0FoundationIsRepeatableAfterB1Migrations(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)

	resetB0Foundation(t, conn, true)
	root := repositoryRoot(t)
	for _, name := range []string{
		"000005_b1_catalog.up.sql",
		"000006_b1_shifts.up.sql",
		"000007_b1_drafts.up.sql",
		"000008_b1_submit.up.sql",
		"000009_b1_recovery.up.sql",
	} {
		if _, err := conn.Exec(ctx, readMigration(t, root, name)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}

	resetB0Foundation(t, conn, true)
	resetB0Foundation(t, conn, true)
}

func resetB0Foundation(t *testing.T, conn *pgx.Conn, withContextFunction bool) {
	t.Helper()
	ctx := context.Background()
	root := repositoryRoot(t)
	resetMigrations(t, conn)
	if _, err := conn.Exec(ctx, readMigration(t, root, "000001_b0_foundation.up.sql")); err != nil {
		t.Fatalf("apply foundation: %v", err)
	}
	if withContextFunction {
		if _, err := conn.Exec(ctx, readMigration(t, root, "000002_b0_request_context.up.sql")); err != nil {
			t.Fatalf("apply request context: %v", err)
		}
		if _, err := conn.Exec(ctx, readMigration(t, root, "000003_b0_session_activity.up.sql")); err != nil {
			t.Fatalf("apply session activity: %v", err)
		}
		if _, err := conn.Exec(ctx, readMigration(t, root, "000004_b01_session_contract.up.sql")); err != nil {
			t.Fatalf("apply session contract: %v", err)
		}
	}
}

func resetMigrations(t *testing.T, conn *pgx.Conn) {
	t.Helper()
	ctx := context.Background()
	root := repositoryRoot(t)
	for _, migration := range []struct {
		name  string
		check string
	}{
		{"000016_b3_evidence.down.sql", `select to_regclass('public.evidence_event') is not null`},
		{"000015_b3_alerts.down.sql", `select to_regclass('public.alert_rules') is not null`},
		{"000014_b2_scheduler.down.sql", `select to_regprocedure('public.fn_abandon_failed_shifts()') is not null`},
		{"000013_b2_relay.down.sql", `select to_regclass('public.outbox_relay_state') is not null`},
		{"000012_b2_audit.down.sql", `select to_regclass('public.audit_log') is not null`},
		{"000011_b2_amendment.down.sql", `select to_regclass('public.amendments') is not null`},
		{"000010_b2_ack.down.sql", `select to_regclass('public.ack_decisions') is not null`},
		{"000009_b1_recovery.down.sql", `select to_regprocedure('public.fn_recover_submitting_shifts()') is not null`},
		{"000008_b1_submit.down.sql", `select to_regprocedure('public.fn_submit_shift(uuid,uuid,uuid,integer,text,jsonb)') is not null`},
		{"000007_b1_drafts.down.sql", `select to_regprocedure('public.fn_claim_draft(uuid)') is not null`},
		{"000006_b1_shifts.down.sql", `select to_regclass('public.shifts') is not null`},
		{"000005_b1_catalog.down.sql", `select to_regclass('public.dispensers') is not null`},
		{"000004_b01_session_contract.down.sql", `select to_regprocedure('public.fn_login_user(text,text)') is not null`},
		{"000003_b0_session_activity.down.sql", `select exists (select 1 from pg_attribute where attrelid = to_regclass('public.sessions') and attname = 'last_active_at' and not attisdropped)`},
		{"000002_b0_request_context.down.sql", `select to_regprocedure('public.fn_set_request_context(text)') is not null`},
	} {
		var exists bool
		if err := conn.QueryRow(ctx, migration.check).Scan(&exists); err != nil {
			t.Fatalf("check %s: %v", migration.name, err)
		}
		if !exists {
			continue
		}
		if _, err := conn.Exec(ctx, readMigration(t, root, migration.name)); err != nil {
			t.Fatalf("reset with %s: %v", migration.name, err)
		}
	}
	if _, err := conn.Exec(ctx, `
		do $$ declare r text; begin
			foreach r in array array['pomkita_app','report_writer','audit_owner','relay','org_owner','station_owner','user_owner','auth_owner','registry_owner','audit_lock_owner'] loop
				if exists (select 1 from pg_roles where rolname = r) then
					execute format('revoke all on schema public from %I', r);
					if to_regnamespace('app') is not null then execute format('revoke all on schema app from %I', r); end if;
				end if;
			end loop;
		end $$;
	`); err != nil {
		t.Fatalf("revoke foundation schema grants: %v", err)
	}
	if _, err := conn.Exec(ctx, readMigration(t, root, "000001_b0_foundation.down.sql")); err != nil {
		t.Fatalf("reset foundation: %v", err)
	}
	if _, err := conn.Exec(ctx, `drop table if exists schema_migrations`); err != nil {
		t.Fatalf("reset migration metadata: %v", err)
	}
}

func openB0Connection(t *testing.T) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(context.Background(), testDatabaseURL())
	if err != nil {
		t.Fatalf("connect to database: %v", err)
	}
	return conn
}

func testDatabaseURL() string {
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		return dsn
	}
	return "postgres://pomkita:pomkita_dev@127.0.0.1:5432/pomkita?sslmode=disable"
}
