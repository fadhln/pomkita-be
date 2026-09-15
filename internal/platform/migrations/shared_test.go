package migrations

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// freshDatabase creates a disposable database and returns its connection URL.
// The test drops the database when the test ends.
func freshDatabase(t *testing.T, ctx context.Context) string {
	t.Helper()
	baseURL := testDatabaseURL(t)
	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("parse database URL: %v", err)
	}

	// The maintenance connection uses the maintenance database.
	maintenance := *parsed
	maintenance.Path = "/postgres"
	maintenanceURL := maintenance.String()
	admin, err := pgx.Connect(ctx, maintenanceURL)
	if err != nil {
		t.Fatalf("connect to maintenance database: %v", err)
	}

	databaseName := "migration_race_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(ctx, `create database `+databaseName); err != nil {
		_ = admin.Close(ctx)
		t.Skipf("cannot create a disposable database: %v", err)
	}
	target := *parsed
	target.Path = "/" + databaseName
	t.Cleanup(func() {
		cleanupContext := context.WithoutCancel(ctx)
		_, _ = admin.Exec(cleanupContext, `drop database if exists `+databaseName)
		_ = admin.Close(cleanupContext)
	})
	return target.String()
}

func testDatabaseURL(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://pomkita:pomkita_dev@127.0.0.1:5432/pomkita?sslmode=disable"
	}
	return dsn
}

// TestEnsureSharedObjects_CreatesSharedObjectsUnderConcurrency proves that two
// processes can create the shared application schema at the same time.
func TestEnsureSharedObjects_CreatesSharedObjectsUnderConcurrency(t *testing.T) {
	ctx := context.Background()
	databaseURL := freshDatabase(t, ctx)

	const workers = 8
	waitGroup := sync.WaitGroup{}
	errs := make([]error, workers)
	for worker := 0; worker < workers; worker++ {
		waitGroup.Add(1)
		go func(worker int) {
			defer waitGroup.Done()
			errs[worker] = EnsureSharedObjects(ctx, databaseURL)
		}(worker)
	}
	waitGroup.Wait()

	for worker, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: ensure shared objects: %v", worker, err)
		}
	}

	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to disposable database: %v", err)
	}
	defer connection.Close(ctx)

	var schemaCount int
	if err := connection.QueryRow(ctx, `select count(*) from pg_namespace where nspname = 'app'`).Scan(&schemaCount); err != nil {
		t.Fatalf("count application schema: %v", err)
	}
	if schemaCount != 1 {
		t.Fatalf("application schema count: got %d, want 1", schemaCount)
	}

	var extensionCount int
	if err := connection.QueryRow(ctx, `select count(*) from pg_extension where extname = 'pgcrypto'`).Scan(&extensionCount); err != nil {
		t.Fatalf("count pgcrypto extension: %v", err)
	}
	if extensionCount != 1 {
		t.Fatalf("pgcrypto extension count: got %d, want 1", extensionCount)
	}
}

// TestMigrator_AppliesIsolatedSchemasOnAFreshDatabaseConcurrently reproduces the
// parallel repository test pattern: several processes migrate one new database at
// the same time, each in an isolated schema.
func TestMigrator_AppliesIsolatedSchemasOnAFreshDatabaseConcurrently(t *testing.T) {
	ctx := context.Background()
	databaseURL := freshDatabase(t, ctx)
	migrationsPath, err := filepath.Abs(filepath.Join(repositoryRoot(t), "migrations"))
	if err != nil {
		t.Fatalf("resolve migrations: %v", err)
	}

	// Three processes are sufficient to race on the first schema creation, and
	// the test stays cheap because each worker applies the complete migration set.
	const workers = 3
	waitGroup := sync.WaitGroup{}
	errs := make([]error, workers)
	for worker := 0; worker < workers; worker++ {
		waitGroup.Add(1)
		go func(worker int) {
			defer waitGroup.Done()
			schema := "race_" + strings.ReplaceAll(uuid.NewString(), "-", "")

			admin, err := pgx.Connect(ctx, databaseURL)
			if err != nil {
				errs[worker] = err
				return
			}
			defer admin.Close(ctx)
			if _, err := admin.Exec(ctx, `create schema `+schema); err != nil {
				errs[worker] = err
				return
			}

			scoped, err := url.Parse(databaseURL)
			if err != nil {
				errs[worker] = err
				return
			}
			query := scoped.Query()
			query.Set("options", "-c search_path="+schema)
			scoped.RawQuery = query.Encode()

			runner, err := New(scoped.String(), migrationsPath)
			if err != nil {
				errs[worker] = err
				return
			}
			errs[worker] = runner.Up(ctx)
		}(worker)
	}
	waitGroup.Wait()

	for worker, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: apply migrations: %v", worker, err)
		}
	}
}
