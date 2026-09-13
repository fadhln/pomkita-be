package test

import (
	"context"
	"testing"
)

func TestB4ReportingProcedures_AreAvailableAfterMigration(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	resetMigrations(t, conn)
	applyB3Migrations(t, conn)
	if _, err := conn.Exec(ctx, `select set_config('app.context_valid', 'true', false), set_config('app.org_id', '11111111-1111-4111-8111-111111111111', false), set_config('app.role', 'Owner', false)`); err != nil {
		t.Fatalf("set reporting context: %v", err)
	}

	for _, name := range []string{
		`public.read_report_printout(uuid)`,
		`public.read_anomaly_export()`,
		`public.read_audit_export()`,
		`public.read_policy_history()`,
	} {
		var procedure string
		if err := conn.QueryRow(ctx, `select to_regprocedure($1)`, name).Scan(&procedure); err != nil {
			t.Fatalf("find B4 procedure %q: %v", name, err)
		}
		if procedure == "" {
			t.Fatalf("B4 procedure %q is missing", name)
		}
	}
}

func TestB4ReportingProcedures_AreRegisteredAndExecutableByApplicationRole(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	resetMigrations(t, conn)
	applyB3Migrations(t, conn)

	rows, err := conn.Query(ctx, `
		select r.name, pg_get_userbyid(p.proowner), has_function_privilege('pomkita_app', p.oid, 'EXECUTE')
		from public.procedure_registry r
		join pg_proc p on p.proname = r.name
		join pg_namespace n on n.oid = p.pronamespace and n.nspname = 'public'
		where r.name = any($1::text[])
		order by r.name`, []string{"read_report_printout", "read_anomaly_export", "read_audit_export", "read_policy_history", "read_audit_chain", "fn_verify_audit_chain"})
	if err != nil {
		t.Fatalf("read reporting grants: %v", err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var name, owner string
		var executable bool
		if err := rows.Scan(&name, &owner, &executable); err != nil {
			t.Fatalf("scan reporting grants: %v", err)
		}
		seen++
		if !executable {
			t.Errorf("%s is not executable by pomkita_app", name)
		}
		if name == "read_audit_export" || name == "read_audit_chain" || name == "fn_verify_audit_chain" {
			if owner != "audit_owner" {
				t.Errorf("%s owner: got %q, want audit_owner", name, owner)
			}
		} else if owner != "report_writer" {
			t.Errorf("%s owner: got %q, want report_writer", name, owner)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read reporting grant rows: %v", err)
	}
	if seen != 6 {
		t.Fatalf("reporting procedure count: got %d, want 6", seen)
	}
}
