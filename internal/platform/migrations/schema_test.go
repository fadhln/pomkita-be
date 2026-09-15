package migrations

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestMigrationSet_HasAReversibleProcedureFreeInitialSchema(t *testing.T) {
	root := repositoryRoot(t)
	upPath := filepath.Join(root, "migrations", "000001_initial_schema.up.sql")
	downPath := filepath.Join(root, "migrations", "000001_initial_schema.down.sql")

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

func TestMigrationSet_SerializesSharedExtensionSchemaCreation(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(root, "migrations", "000001_initial_schema.up.sql")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read initial migration: %v", err)
	}
	migration := strings.ToLower(string(contents))
	lock := "pg_advisory_xact_lock(hashtext('pomkita:app-schema'))"
	if !strings.Contains(migration, lock) {
		t.Fatalf("initial migration does not serialize app schema creation with %q", lock)
	}
}

func TestMigrationSet_DefinesCatalogTablesAndTemporalConstraints(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(root, "migrations", "000002_catalog_tables.up.sql")
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

func TestMigrationSet_DefinesDraftChildrenAndSubmitIdempotency(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(root, "migrations", "000003_draft_tables.up.sql")
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

func TestMigrationSet_DefinesReportInputsAndMeterState(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(root, "migrations", "000004_report_inputs.up.sql")
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

func TestMigrationSet_DefinesPoliciesSnapshotsEvidenceAndBaselines(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(root, "migrations", "000005_policy_snapshot_tables.up.sql")
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

func TestMigrationSet_DefinesAuthenticationTablesWithoutProcedures(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(root, "migrations", "000006_auth_tables.up.sql")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read authentication migration: %v", err)
	}

	migration := strings.ToLower(string(contents))
	for _, required := range []string{
		"alter table users",
		"password_hash",
		"create table jwt_keys",
		"create table sessions",
		"unique index users_email_ci",
		"status in ('active', 'previous', 'retired')",
	} {
		if !strings.Contains(migration, required) {
			t.Fatalf("authentication migration does not contain %q", required)
		}
	}
	for _, forbidden := range []string{"create function", "create procedure", "create role", "grant insert", "grant update", "grant delete"} {
		if strings.Contains(migration, forbidden) {
			t.Fatalf("authentication migration contains forbidden database behavior %q", forbidden)
		}
	}
}

func TestMigrationSet_DefinesIdentityAdministrationTables(t *testing.T) {
	root := repositoryRoot(t)
	up, err := os.ReadFile(filepath.Join(root, "migrations", "000012_identity_administration.up.sql"))
	if err != nil {
		t.Fatalf("read identity migration: %v", err)
	}
	down, err := os.ReadFile(filepath.Join(root, "migrations", "000012_identity_administration.down.sql"))
	if err != nil {
		t.Fatalf("read identity down migration: %v", err)
	}
	upText := strings.ToLower(string(up))
	for _, required := range []string{
		"add column username text",
		"users_username_ci",
		"password_hash",
		"invited_at timestamptz(6)",
		"activated_at timestamptz(6)",
		"updated_at timestamptz(6)",
		"users_enabled_identity_check",
		"create table account_tokens",
		"purpose in ('invitation', 'password_reset')",
		"octet_length(token_hash) = 32",
		"foreign key (org_id, user_id)",
		"foreign key (created_by)",
		"account_tokens_token_hash",
	} {
		if !strings.Contains(upText, required) {
			t.Fatalf("identity migration does not contain %q", required)
		}
	}
	if strings.Contains(upText, "create function") || strings.Contains(upText, "grant ") {
		t.Fatal("identity migration contains unsupported database behavior")
	}
	downText := strings.ToLower(string(down))
	for _, required := range []string{"drop table if exists account_tokens", "drop column if exists username", "set not null"} {
		if !strings.Contains(downText, required) {
			t.Fatalf("identity down migration does not contain %q", required)
		}
	}
}

func TestMigrationSet_DefinesAccountStationName(t *testing.T) {
	root := repositoryRoot(t)
	up, err := os.ReadFile(filepath.Join(root, "migrations", "000013_account_station_name.up.sql"))
	if err != nil {
		t.Fatalf("read account station up migration: %v", err)
	}
	down, err := os.ReadFile(filepath.Join(root, "migrations", "000013_account_station_name.down.sql"))
	if err != nil {
		t.Fatalf("read account station down migration: %v", err)
	}
	if !strings.Contains(strings.ToLower(string(up)), "add column name text not null") {
		t.Fatal("account station up migration does not add station name")
	}
	if !strings.Contains(strings.ToLower(string(down)), "drop column if exists name") {
		t.Fatal("account station down migration does not remove station name")
	}
}

func TestMigrationSet_DefinesOrganizationDetail(t *testing.T) {
	root := repositoryRoot(t)
	up, err := os.ReadFile(filepath.Join(root, "migrations", "000014_organization_detail.up.sql"))
	if err != nil {
		t.Fatalf("read organization up migration: %v", err)
	}
	down, err := os.ReadFile(filepath.Join(root, "migrations", "000014_organization_detail.down.sql"))
	if err != nil {
		t.Fatalf("read organization down migration: %v", err)
	}
	upText, downText := strings.ToLower(string(up)), strings.ToLower(string(down))
	for _, required := range []string{"add column legal_name text", "add column address text", "add column contact_email text", "add column timezone text", "add column enabled boolean not null default true", "add column updated_at timestamptz(6)"} {
		if !strings.Contains(upText, required) {
			t.Fatalf("organization migration does not contain %q", required)
		}
	}
	for _, required := range []string{"drop column if exists legal_name", "drop column if exists address", "drop column if exists contact_email", "drop column if exists timezone", "drop column if exists enabled", "drop column if exists updated_at"} {
		if !strings.Contains(downText, required) {
			t.Fatalf("organization down migration does not contain %q", required)
		}
	}
	if strings.Contains(upText, "create function") || strings.Contains(upText, "create trigger") || strings.Contains(upText, "grant ") {
		t.Fatal("organization migration contains unsupported database behavior")
	}
}

func TestMigrationSet_DefinesAcknowledgementTables(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(root, "migrations", "000007_governance_ack.up.sql")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read governance migration: %v", err)
	}

	migration := strings.ToLower(string(contents))
	for _, required := range []string{
		"create type ack_decision",
		"create table ack_decisions",
		"create table ack_head",
		"create table ack_supersessions",
		"deferrable initially deferred",
	} {
		if !strings.Contains(migration, required) {
			t.Fatalf("governance migration does not contain %q", required)
		}
	}
}

func TestMigrationSet_DefinesAmendmentTables(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(root, "migrations", "000008_governance_amendments.up.sql")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read amendment migration: %v", err)
	}

	migration := strings.ToLower(string(contents))
	for _, required := range []string{
		"create table amendments",
		"create table amendment_items",
		"amendments_one_pending_base",
		"stale_check_hash",
		"target_kind",
	} {
		if !strings.Contains(migration, required) {
			t.Fatalf("amendment migration does not contain %q", required)
		}
	}
}

func TestMigrationSet_DefinesAlertTables(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(root, "migrations", "000009_alerts.up.sql")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read alert migration: %v", err)
	}

	migration := strings.ToLower(string(contents))
	for _, required := range []string{"create type alert_rule_type", "create table alert_rules", "create table alert_events", "period_bucket", "related_fired_event_id"} {
		if !strings.Contains(migration, required) {
			t.Fatalf("alert migration does not contain %q", required)
		}
	}
}

func TestMigrationSet_DefinesAuditAndRelayTables(t *testing.T) {
	root := repositoryRoot(t)
	for _, name := range []string{"000010_audit.up.sql", "000011_relay.up.sql"} {
		contents, err := os.ReadFile(filepath.Join(root, "migrations", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		migration := strings.ToLower(string(contents))
		for _, required := range []string{"create table"} {
			if !strings.Contains(migration, required) {
				t.Fatalf("%s does not contain %q", name, required)
			}
		}
	}
	audit, _ := os.ReadFile(filepath.Join(root, "migrations", "000010_audit.up.sql"))
	for _, required := range []string{"create table audit_chain_locks", "create table audit_log", "create table audit_outbox", "create table audit_denied", "prev_hash", "row_hash"} {
		if !strings.Contains(strings.ToLower(string(audit)), required) {
			t.Fatalf("audit migration does not contain %q", required)
		}
	}
	relay, _ := os.ReadFile(filepath.Join(root, "migrations", "000011_relay.up.sql"))
	if !strings.Contains(strings.ToLower(string(relay)), "create table outbox_relay_state") {
		t.Fatal("relay migration does not contain outbox_relay_state")
	}
}

func TestMigrationSet_AppliesAndReversesInAnIsolatedSchema(t *testing.T) {
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

	migrationsPath, err := filepath.Abs(filepath.Join(repositoryRoot(t), "migrations"))
	if err != nil {
		t.Fatalf("resolve migrations: %v", err)
	}
	runner, err := New(scopedURL.String(), migrationsPath)
	if err != nil {
		t.Fatalf("create migration runner: %v", err)
	}
	if err := runner.Up(ctx); err != nil {
		t.Fatalf("apply migrations: %v", err)
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
	`, []string{"organizations", "stations", "users", "user_station_roles", "policy_snapshot_sets", "shifts", "shift_drafts", "shift_reports", "dispensers", "tanks", "nozzles", "nozzle_tank_map", "dispenser_nozzle_map", "dispenser_prices", "draft_readings", "draft_sales", "draft_losses", "draft_evidence_staging", "submit_idempotency", "dispenser_readings", "sales_declared", "loss_identity", "loss_entries", "deliveries", "dip_readings", "shift_transitions", "meter_reset_events", "threshold_policy_revisions", "evidence_policy_revisions", "evidence_policy_types", "policy_snapshot_items", "delivery_snapshots", "dip_snapshots", "loss_exception", "evidence_event", "nozzle_baseline_revisions", "nozzle_baseline_current", "jwt_keys", "sessions", "account_tokens", "ack_decisions", "ack_head", "ack_supersessions", "amendments", "amendment_items", "alert_rules", "alert_events", "audit_chain_locks", "audit_log", "audit_outbox", "audit_denied", "outbox_relay_state"}).Scan(&tableCount); err != nil {
		t.Fatalf("count clean tables: %v", err)
	}
	if tableCount != 52 {
		t.Fatalf("clean table count: got %d, want 52", tableCount)
	}

	if err := runner.Steps(ctx, -1); err != nil {
		t.Fatalf("reverse paired role history index migration: %v", err)
	}
	var roleHistoryIndexCount int
	if err := connection.QueryRow(ctx, `select count(*) from pg_indexes where schemaname = current_schema() and indexname = 'audit_log_role_history_target'`).Scan(&roleHistoryIndexCount); err != nil {
		t.Fatalf("count role history index after paired down: %v", err)
	}
	if roleHistoryIndexCount != 0 {
		t.Fatalf("role history index remains after paired down: got %d", roleHistoryIndexCount)
	}
	if err := runner.Steps(ctx, 1); err != nil {
		t.Fatalf("reapply paired role history index migration: %v", err)
	}
	if err := connection.QueryRow(ctx, `select count(*) from pg_indexes where schemaname = current_schema() and indexname = 'audit_log_role_history_target'`).Scan(&roleHistoryIndexCount); err != nil {
		t.Fatalf("count role history index after paired up: %v", err)
	}
	if roleHistoryIndexCount != 1 {
		t.Fatalf("role history index missing after paired up: got %d", roleHistoryIndexCount)
	}
	if err := runner.Steps(ctx, -1); err != nil {
		t.Fatalf("reverse paired station detail migration: %v", err)
	}
	if err := runner.Steps(ctx, -1); err != nil {
		t.Fatalf("reverse paired station detail migration after role index round trip: %v", err)
	}
	var stationDetailCount int
	if err := connection.QueryRow(ctx, `select count(*) from information_schema.columns where table_schema = current_schema() and table_name = 'stations' and column_name = 'code'`).Scan(&stationDetailCount); err != nil {
		t.Fatalf("count station detail after paired down: %v", err)
	}
	if stationDetailCount != 0 {
		t.Fatalf("station detail remains after paired down: got %d", stationDetailCount)
	}
	if err := runner.Steps(ctx, 1); err != nil {
		t.Fatalf("reapply paired station detail migration: %v", err)
	}
	if err := connection.QueryRow(ctx, `select count(*) from information_schema.columns where table_schema = current_schema() and table_name = 'stations' and column_name = 'code'`).Scan(&stationDetailCount); err != nil {
		t.Fatalf("count station detail after paired up: %v", err)
	}
	if stationDetailCount != 1 {
		t.Fatalf("station detail changed after paired up: got %d", stationDetailCount)
	}
	var stationNameCount int
	if err := connection.QueryRow(ctx, `select count(*) from information_schema.columns where table_schema = current_schema() and table_name = 'stations' and column_name = 'name'`).Scan(&stationNameCount); err != nil {
		t.Fatalf("count station name after paired up: %v", err)
	}
	if stationNameCount != 1 {
		t.Fatalf("station name changed after organization round trip: got %d", stationNameCount)
	}

	if err := runner.Steps(ctx, -1); err != nil {
		t.Fatalf("reverse paired station detail migration: %v", err)
	}
	if err := runner.Steps(ctx, -1); err != nil {
		t.Fatalf("reverse paired organization migration: %v", err)
	}
	if err := connection.QueryRow(ctx, `select count(*) from information_schema.columns where table_schema = current_schema() and table_name = 'stations' and column_name = 'name'`).Scan(&stationNameCount); err != nil {
		t.Fatalf("count station name after paired down: %v", err)
	}
	if stationNameCount != 1 {
		t.Fatalf("station name changed after organization down: got %d", stationNameCount)
	}
	if err := runner.Steps(ctx, -1); err != nil {
		t.Fatalf("reverse paired station name migration: %v", err)
	}
	if err := connection.QueryRow(ctx, `select count(*) from information_schema.columns where table_schema = current_schema() and table_name = 'stations' and column_name = 'name'`).Scan(&stationNameCount); err != nil {
		t.Fatalf("count station name after paired down: %v", err)
	}
	if stationNameCount != 0 {
		t.Fatalf("station name remains after paired down: got %d", stationNameCount)
	}
	if err := runner.Steps(ctx, -1); err != nil {
		t.Fatalf("reverse paired identity migration: %v", err)
	}
	var identityTableCount int
	if err := connection.QueryRow(ctx, `select count(*) from pg_class c join pg_namespace n on n.oid = c.relnamespace where n.nspname = current_schema() and c.relkind = 'r' and c.relname = 'account_tokens'`).Scan(&identityTableCount); err != nil {
		t.Fatalf("count identity table after paired down: %v", err)
	}
	if identityTableCount != 0 {
		t.Fatalf("account_tokens remains after paired down: got %d", identityTableCount)
	}
	if err := runner.Steps(ctx, 1); err != nil {
		t.Fatalf("reapply paired identity migration: %v", err)
	}
	if err := runner.Steps(ctx, 1); err != nil {
		t.Fatalf("reapply paired station migration: %v", err)
	}
	if err := runner.Steps(ctx, 1); err != nil {
		t.Fatalf("reapply paired organization migration: %v", err)
	}
	if err := runner.Steps(ctx, 1); err != nil {
		t.Fatalf("reapply paired station detail migration: %v", err)
	}

	if err := runner.Down(ctx); err != nil {
		t.Fatalf("reverse migration: %v", err)
	}
	var remaining int
	if err := connection.QueryRow(ctx, `select count(*) from pg_class c join pg_namespace n on n.oid = c.relnamespace where n.nspname = current_schema() and c.relkind = 'r' and c.relname <> 'schema_migrations'`).Scan(&remaining); err != nil {
		t.Fatalf("count clean tables after down: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("remaining clean tables after down: got %d, want 0", remaining)
	}

	if err := runner.Up(ctx); err != nil {
		t.Fatalf("reapply migrations: %v", err)
	}
	if err := connection.QueryRow(ctx, `select count(*) from pg_class c join pg_namespace n on n.oid = c.relnamespace where n.nspname = current_schema() and c.relkind = 'r' and c.relname <> 'schema_migrations'`).Scan(&remaining); err != nil {
		t.Fatalf("count clean tables after reapply: %v", err)
	}
	if remaining != 52 {
		t.Fatalf("clean table count after reapply: got %d, want 52", remaining)
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
	root, err := filepath.Abs(filepath.Join(workingDirectory, "..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	return root
}
