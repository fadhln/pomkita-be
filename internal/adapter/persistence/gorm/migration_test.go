package gormstore

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	cleanmigrations "github.com/pomkita/pomkita-be/internal/platform/migrations"
)

func TestCleanMigrationSet_HasAReversibleProcedureFreeInitialSchema(t *testing.T) {
	root := repositoryRoot(t)
	upPath := filepath.Join(root, "migrations", "clean", "000001_initial_schema.up.sql")
	downPath := filepath.Join(root, "migrations", "clean", "000001_initial_schema.down.sql")

	up, err := os.ReadFile(upPath)
	if err != nil {
		t.Fatalf("read clean up migration: %v", err)
	}
	down, err := os.ReadFile(downPath)
	if err != nil {
		t.Fatalf("read clean down migration: %v", err)
	}

	upText := strings.ToLower(string(up))
	for _, forbidden := range []string{"create function", "create procedure", "security definer", "create trigger"} {
		if strings.Contains(upText, forbidden) {
			t.Fatalf("clean up migration contains forbidden database behavior %q", forbidden)
		}
	}
	for _, required := range []string{"create table organizations", "create table stations", "create table shifts", "create table shift_drafts", "create table shift_reports"} {
		if !strings.Contains(upText, required) {
			t.Fatalf("clean up migration does not contain %q", required)
		}
	}
	if !strings.Contains(strings.ToLower(string(down)), "drop table if exists shift_reports") {
		t.Fatal("clean down migration does not reverse shift_reports")
	}
}

func TestCleanMigrationSet_DefinesCatalogTablesAndTemporalConstraints(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(root, "migrations", "clean", "000002_catalog_tables.up.sql")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read catalog migration: %v", err)
	}

	migration := strings.ToLower(string(contents))
	for _, required := range []string{
		"create table dispensers",
		"create table tanks",
		"create table nozzles",
		"create table nozzle_tank_map",
		"create table dispenser_nozzle_map",
		"create table dispenser_prices",
		"exclude using gist",
	} {
		if !strings.Contains(migration, required) {
			t.Fatalf("catalog migration does not contain %q", required)
		}
	}
}

func TestCleanMigrationSet_DefinesDraftChildrenAndSubmitIdempotency(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(root, "migrations", "clean", "000003_draft_tables.up.sql")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read draft migration: %v", err)
	}

	migration := strings.ToLower(string(contents))
	for _, required := range []string{
		"create table draft_readings",
		"create table draft_sales",
		"create table draft_losses",
		"create table draft_evidence_staging",
		"create table submit_idempotency",
		"numeric(8,2)",
		"numeric(14,0)",
	} {
		if !strings.Contains(migration, required) {
			t.Fatalf("draft migration does not contain %q", required)
		}
	}
}

func TestCleanMigrationSet_DefinesReportInputsAndMeterState(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(root, "migrations", "clean", "000004_report_inputs.up.sql")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read report input migration: %v", err)
	}

	migration := strings.ToLower(string(contents))
	for _, required := range []string{
		"create table dispenser_readings",
		"create table sales_declared",
		"create table loss_identity",
		"create table loss_entries",
		"create table deliveries",
		"create table dip_readings",
		"create table shift_transitions",
		"create table meter_reset_events",
		"numeric(8,2)",
		"numeric(14,0)",
	} {
		if !strings.Contains(migration, required) {
			t.Fatalf("report input migration does not contain %q", required)
		}
	}
}

func TestCleanMigrationSet_DefinesPoliciesSnapshotsEvidenceAndBaselines(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(root, "migrations", "clean", "000005_policy_snapshot_tables.up.sql")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read policy migration: %v", err)
	}

	migration := strings.ToLower(string(contents))
	for _, required := range []string{
		"create table threshold_policy_revisions",
		"create table evidence_policy_revisions",
		"create table evidence_policy_types",
		"create table policy_snapshot_items",
		"create table delivery_snapshots",
		"create table dip_snapshots",
		"create table loss_exception",
		"create table evidence_event",
		"create table nozzle_baseline_revisions",
		"create table nozzle_baseline_current",
		"shift_price_map_snapshot",
		"backfilled",
	} {
		if !strings.Contains(migration, required) {
			t.Fatalf("policy migration does not contain %q", required)
		}
	}
}

func TestCleanMigrationSet_AppliesAndReversesInAnIsolatedSchema(t *testing.T) {
	ctx := context.Background()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://pomkita:pomkita_dev@127.0.0.1:5432/pomkita?sslmode=disable"
	}
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to database: %v", err)
	}
	defer admin.Close(ctx)

	schema := "phase1_clean_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(ctx, `create schema `+schema); err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	defer admin.Exec(ctx, `drop schema if exists `+schema+` cascade`)

	scopedURL, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse database URL: %v", err)
	}
	query := scopedURL.Query()
	query.Set("options", "-c search_path="+schema)
	scopedURL.RawQuery = query.Encode()

	migrationsPath, err := filepath.Abs(filepath.Join(repositoryRoot(t), "migrations", "clean"))
	if err != nil {
		t.Fatalf("resolve clean migrations: %v", err)
	}
	runner, err := cleanmigrations.New(scopedURL.String(), migrationsPath)
	if err != nil {
		t.Fatalf("create clean migration runner: %v", err)
	}
	if err := runner.Up(ctx); err != nil {
		t.Fatalf("apply clean migrations: %v", err)
	}

	connection, err := pgx.Connect(ctx, scopedURL.String())
	if err != nil {
		t.Fatalf("connect to isolated schema: %v", err)
	}
	defer connection.Close(ctx)
	var tableCount int
	if err := connection.QueryRow(ctx, `
		select count(*) from pg_class c
		join pg_namespace n on n.oid = c.relnamespace
		where n.nspname = current_schema()
		  and c.relkind = 'r'
		  and c.relname = any($1::text[])
	`, []string{"organizations", "stations", "users", "user_station_roles", "policy_snapshot_sets", "shifts", "shift_drafts", "shift_reports", "dispensers", "tanks", "nozzles", "nozzle_tank_map", "dispenser_nozzle_map", "dispenser_prices", "draft_readings", "draft_sales", "draft_losses", "draft_evidence_staging", "submit_idempotency", "dispenser_readings", "sales_declared", "loss_identity", "loss_entries", "deliveries", "dip_readings", "shift_transitions", "meter_reset_events", "threshold_policy_revisions", "evidence_policy_revisions", "evidence_policy_types", "policy_snapshot_items", "delivery_snapshots", "dip_snapshots", "loss_exception", "evidence_event", "nozzle_baseline_revisions", "nozzle_baseline_current"}).Scan(&tableCount); err != nil {
		t.Fatalf("count clean tables: %v", err)
	}
	if tableCount != 37 {
		t.Fatalf("clean table count: got %d, want 37", tableCount)
	}

	if err := runner.Down(ctx); err != nil {
		t.Fatalf("reverse clean migration: %v", err)
	}
	var remaining int
	if err := connection.QueryRow(ctx, `select count(*) from pg_class c join pg_namespace n on n.oid = c.relnamespace where n.nspname = current_schema() and c.relkind = 'r' and c.relname <> 'schema_migrations'`).Scan(&remaining); err != nil {
		t.Fatalf("count clean tables after down: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("remaining clean tables after down: got %d, want 0", remaining)
	}

	if err := runner.Up(ctx); err != nil {
		t.Fatalf("reapply clean migrations: %v", err)
	}
	if err := connection.QueryRow(ctx, `select count(*) from pg_class c join pg_namespace n on n.oid = c.relnamespace where n.nspname = current_schema() and c.relkind = 'r' and c.relname <> 'schema_migrations'`).Scan(&remaining); err != nil {
		t.Fatalf("count clean tables after reapply: %v", err)
	}
	if remaining != 37 {
		t.Fatalf("clean table count after reapply: got %d, want 37", remaining)
	}
	if err := runner.Down(ctx); err != nil {
		t.Fatalf("reverse reapplied migrations: %v", err)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	root, err := filepath.Abs(filepath.Join(workingDirectory, "..", "..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	return root
}
