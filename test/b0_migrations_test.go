package test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestB0FoundationMigrationUpAndDown(t *testing.T) {
	ctx := context.Background()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://pomkita:pomkita_dev@127.0.0.1:5432/pomkita?sslmode=disable"
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to database: %v", err)
	}
	defer conn.Close(ctx)

	root := repositoryRoot(t)
	up := readMigration(t, root, "000001_b0_foundation.up.sql")
	down := readMigration(t, root, "000001_b0_foundation.down.sql")
	if _, err := conn.Exec(ctx, `drop table if exists schema_migrations`); err != nil {
		t.Fatalf("reset migration metadata: %v", err)
	}

	if _, err := conn.Exec(ctx, readMigration(t, root, "000002_b0_request_context.down.sql")); err != nil {
		t.Fatalf("reset request context: %v", err)
	}
	if _, err := conn.Exec(ctx, down); err != nil {
		t.Fatalf("reset foundation with down migration: %v", err)
	}
	if _, err := conn.Exec(ctx, up); err != nil {
		t.Fatalf("apply foundation migration: %v", err)
	}

	var tableCount int
	if err := conn.QueryRow(ctx, `
		select count(*)
		from pg_class c
		join pg_namespace n on n.oid = c.relnamespace
		where n.nspname = 'public' and c.relkind = 'r'
		  and c.relname = any($1::text[])
	`, []string{"organizations", "stations", "users", "user_station_roles", "sessions", "jwt_keys", "procedure_registry", "audit_chain_locks"}).Scan(&tableCount); err != nil {
		t.Fatalf("count foundation tables: %v", err)
	}
	if tableCount != 8 {
		t.Fatalf("expected 8 foundation tables, got %d", tableCount)
	}

	var roleCount int
	if err := conn.QueryRow(ctx, `
		select count(*) from pg_roles
		where rolname = any($1::text[])
	`, []string{"pomkita_app", "report_writer", "audit_owner", "relay", "org_owner", "station_owner", "user_owner", "auth_owner", "registry_owner", "audit_lock_owner"}).Scan(&roleCount); err != nil {
		t.Fatalf("count foundation roles: %v", err)
	}
	if roleCount != 10 {
		t.Fatalf("expected 10 foundation roles, got %d", roleCount)
	}

	var appTablePrivileges int
	if err := conn.QueryRow(ctx, `
		select count(*)
		from information_schema.role_table_grants
		where grantee = 'pomkita_app'
		  and table_schema = 'public'
		  and table_name = any($1::text[])
		  and privilege_type in ('SELECT', 'INSERT', 'UPDATE', 'DELETE', 'TRUNCATE')
	`, []string{"organizations", "stations", "users", "user_station_roles", "sessions", "jwt_keys", "procedure_registry", "audit_chain_locks"}).Scan(&appTablePrivileges); err != nil {
		t.Fatalf("inspect application grants: %v", err)
	}
	if appTablePrivileges != 0 {
		t.Fatalf("application role has %d table privileges", appTablePrivileges)
	}

	if _, err := conn.Exec(ctx, down); err != nil {
		t.Fatalf("reverse foundation migration: %v", err)
	}

	var remaining int
	if err := conn.QueryRow(ctx, `
		select count(*) from pg_class c
		join pg_namespace n on n.oid = c.relnamespace
		where n.nspname = 'public' and c.relname = any($1::text[])
	`, []string{"organizations", "stations", "users", "user_station_roles", "sessions", "jwt_keys", "procedure_registry", "audit_chain_locks"}).Scan(&remaining); err != nil {
		t.Fatalf("check foundation removal: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("expected no foundation objects after down, got %d", remaining)
	}
}

func TestB0CatalogContainsOnlyClassifiedObjects(t *testing.T) {
	ctx := context.Background()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://pomkita:pomkita_dev@127.0.0.1:5432/pomkita?sslmode=disable"
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to database: %v", err)
	}
	defer conn.Close(ctx)

	root := repositoryRoot(t)
	up := readMigration(t, root, "000001_b0_foundation.up.sql")
	down := readMigration(t, root, "000001_b0_foundation.down.sql")
	if _, err := conn.Exec(ctx, `drop table if exists schema_migrations`); err != nil {
		t.Fatalf("reset migration metadata: %v", err)
	}
	if _, err := conn.Exec(ctx, readMigration(t, root, "000002_b0_request_context.down.sql")); err != nil {
		t.Fatalf("reset request context: %v", err)
	}
	if _, err := conn.Exec(ctx, down); err != nil {
		t.Fatalf("reset foundation with down migration: %v", err)
	}
	if _, err := conn.Exec(ctx, up); err != nil {
		t.Fatalf("apply foundation migration: %v", err)
	}
	if _, err := conn.Exec(ctx, readMigration(t, root, "000002_b0_request_context.up.sql")); err != nil {
		t.Fatalf("apply request context migration: %v", err)
	}

	rows, err := conn.Query(ctx, `
		select c.relname
		from pg_class c
		join pg_namespace n on n.oid = c.relnamespace
		where n.nspname not in ('pg_catalog', 'information_schema')
		  and n.nspname not like 'pg_toast%'
		  and c.relkind in ('r', 'v', 'm', 'S')
		  and c.relname <> 'schema_migrations'
		order by n.nspname, c.relname
	`)
	if err != nil {
		t.Fatalf("query catalog relations: %v", err)
	}
	var relations []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan catalog relation: %v", err)
		}
		relations = append(relations, name)
	}
	rows.Close()
	expectedRelations := []string{"audit_chain_locks", "jwt_keys", "organizations", "procedure_registry", "sessions", "stations", "user_station_roles", "users"}
	if fmt.Sprint(relations) != fmt.Sprint(expectedRelations) {
		t.Fatalf("unclassified or missing relations: got %v, want %v", relations, expectedRelations)
	}

	rows, err = conn.Query(ctx, `
		select p.proname || '(' || pg_get_function_identity_arguments(p.oid) || ')'
		from pg_proc p
		join pg_namespace n on n.oid = p.pronamespace
		where n.nspname not in ('pg_catalog', 'information_schema')
		  and n.nspname not like 'pg_toast%'
		  and not exists (
			select 1
			from pg_depend d
			join pg_extension e on e.oid = d.refobjid
			where d.classid = 'pg_proc'::regclass
			  and d.objid = p.oid
			  and d.deptype = 'e'
		  )
		order by 1
	`)
	if err != nil {
		t.Fatalf("query catalog functions: %v", err)
	}
	var functions []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan catalog function: %v", err)
		}
		functions = append(functions, name)
	}
	rows.Close()
	expectedFunctions := []string{"fn_set_request_context(p_raw_token text)"}
	if fmt.Sprint(functions) != fmt.Sprint(expectedFunctions) {
		t.Fatalf("unclassified or missing public functions: got %v, want %v", functions, expectedFunctions)
	}

	rows, err = conn.Query(ctx, `
		select rolname from pg_roles
		where rolname not like 'pg_%' and rolname not in ('pomkita', 'postgres')
		order by rolname
	`)
	if err != nil {
		t.Fatalf("query catalog roles: %v", err)
	}
	var roles []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan catalog role: %v", err)
		}
		roles = append(roles, name)
	}
	rows.Close()
	expectedRoles := []string{"audit_lock_owner", "audit_owner", "auth_owner", "org_owner", "pomkita_app", "registry_owner", "relay", "report_writer", "station_owner", "user_owner"}
	if fmt.Sprint(roles) != fmt.Sprint(expectedRoles) {
		t.Fatalf("unclassified or missing roles: got %v, want %v", roles, expectedRoles)
	}

	if _, err := conn.Exec(ctx, readMigration(t, root, "000002_b0_request_context.down.sql")); err != nil {
		t.Fatalf("reverse request context migration: %v", err)
	}
	if _, err := conn.Exec(ctx, down); err != nil {
		t.Fatalf("reverse foundation migration: %v", err)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("find test file")
	}
	return filepath.Dir(filepath.Dir(file))
}

func readMigration(t *testing.T, root, name string) string {
	t.Helper()
	path := filepath.Join(root, "migrations", name)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration %s: %v", name, err)
	}
	return string(contents)
}
