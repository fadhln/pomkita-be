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
	if _, err := conn.Exec(ctx, readMigration(t, root, "000001_b0_foundation.down.sql")); err != nil {
		t.Fatalf("reset foundation: %v", err)
	}
	if _, err := conn.Exec(ctx, `drop table if exists schema_migrations`); err != nil {
		t.Fatalf("reset migration metadata: %v", err)
	}
}

func openB0Connection(t *testing.T) *pgx.Conn {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://pomkita:pomkita_dev@127.0.0.1:5432/pomkita?sslmode=disable"
	}
	conn, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect to database: %v", err)
	}
	return conn
}
