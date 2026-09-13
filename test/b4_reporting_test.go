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
