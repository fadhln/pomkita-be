package test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

func resetB0Foundation(t *testing.T, conn *pgx.Conn, withContextFunction bool) {
	t.Helper()
	ctx := context.Background()
	root := repositoryRoot(t)
	if _, err := conn.Exec(ctx, readMigration(t, root, "000004_b1_catalog.down.sql")); err != nil {
		t.Fatalf("reset B1 catalog: %v", err)
	}
	if _, err := conn.Exec(ctx, `drop table if exists schema_migrations`); err != nil {
		t.Fatalf("reset migration metadata: %v", err)
	}
	if withContextFunction {
		if _, err := conn.Exec(ctx, readMigration(t, root, "000002_b0_request_context.down.sql")); err != nil {
			t.Fatalf("reset request context: %v", err)
		}
	}
	if _, err := conn.Exec(ctx, readMigration(t, root, "000001_b0_foundation.down.sql")); err != nil {
		t.Fatalf("reset foundation: %v", err)
	}
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
