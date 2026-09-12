package test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5"
	appdb "github.com/pomkita/pomkita-be/internal/db"
)

func TestB0MigratorAppliesAndReversesAllMigrations(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://pomkita:pomkita_dev@127.0.0.1:5432/pomkita?sslmode=disable"
	}
	root := repositoryRoot(t)
	migrator, err := appdb.NewMigrator(dsn, filepath.Join(root, "migrations"))
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	admin, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect for migration reset: %v", err)
	}
	resetMigrations(t, admin)
	admin.Close(context.Background())
	if err := migrator.Down(context.Background()); err != nil {
		t.Fatalf("reset migrations: %v", err)
	}
	if err := migrator.Up(context.Background()); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	conn, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect after migration: %v", err)
	}
	defer conn.Close(context.Background())
	var version int
	if err := conn.QueryRow(context.Background(), `select version from schema_migrations`).Scan(&version); err != nil {
		t.Fatalf("read migration version: %v", err)
	}
	if version != migrationCount(t) {
		t.Fatalf("got migration version %d, want %d", version, migrationCount(t))
	}

	if err := migrator.Down(context.Background()); err != nil {
		t.Fatalf("reverse migrations: %v", err)
	}
	var tableCount int
	if err := conn.QueryRow(context.Background(), `
		select count(*) from pg_class c
		join pg_namespace n on n.oid = c.relnamespace
		where n.nspname = 'public'
		  and c.relname = any($1::text[])
	`, []string{"organizations", "stations", "users", "user_station_roles", "sessions", "jwt_keys", "procedure_registry", "audit_chain_locks"}).Scan(&tableCount); err != nil {
		t.Fatalf("check reversed tables: %v", err)
	}
	if tableCount != 0 {
		t.Fatalf("got %d foundation tables after down", tableCount)
	}
}
