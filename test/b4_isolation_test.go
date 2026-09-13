package test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	b4IsolationOrg     = "11111111-1111-4111-8111-111111111111"
	b4IsolationStation = "22222222-2222-4222-8222-222222222222"
	b4OtherOrg         = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	b4OtherStation     = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	b4ReportID         = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
)

func TestB4Reporting_IsolationMatrixCoversEveryRegisteredProcedure(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	resetMigrations(t, conn)
	applyB3Migrations(t, conn)
	defer resetB0Foundation(t, conn, true)

	expected := []string{
		"read_report_printout", "read_anomaly_export", "read_audit_export",
		"read_policy_history", "read_audit_chain", "fn_verify_audit_chain",
	}
	rows, err := conn.Query(ctx, `select name from procedure_registry where name = any($1::text[]) order by name`, expected)
	if err != nil {
		t.Fatalf("read procedure registry: %v", err)
	}
	registered := make(map[string]bool, len(expected))
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan procedure registry: %v", err)
		}
		registered[name] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("read procedure registry rows: %v", err)
	}
	for _, name := range expected {
		if !registered[name] {
			t.Fatalf("registered procedure is missing: %s", name)
		}
	}

	for _, scenario := range []struct {
		name    string
		org     string
		station string
	}{
		{name: "cross-org", org: b4OtherOrg, station: b4OtherStation},
		{name: "cross-station", org: b4IsolationOrg, station: b4OtherStation},
	} {
		setIsolationContext(t, conn, scenario.org, scenario.station, "Owner", true)
		for _, procedure := range expected {
			t.Run(scenario.name+"/"+procedure, func(t *testing.T) {
				if err := callB4ReportingProcedure(ctx, conn, procedure, b4ReportID); err != nil {
					var pgError *pgconn.PgError
					if errors.As(err, &pgError) && pgError.Code == "42501" {
						return
					}
					t.Fatalf("procedure error: %v", err)
				}
			})
		}
	}

	setIsolationContext(t, conn, b4IsolationOrg, b4IsolationStation, "Owner", false)
	for _, procedure := range expected {
		t.Run("no-context/"+procedure, func(t *testing.T) {
			if err := callB4ReportingProcedure(ctx, conn, procedure, b4ReportID); err == nil {
				t.Fatal("procedure accepted a missing request context")
			}
		})
	}
}

func setIsolationContext(t *testing.T, conn *pgx.Conn, org, station, role string, valid bool) {
	t.Helper()
	if _, err := conn.Exec(context.Background(), `
		select set_config('app.context_valid',$1,false),
		       set_config('app.org_id',$2,false), set_config('app.station_id',$3,false),
		       set_config('app.role',$4,false)`, boolText(valid), org, station, role); err != nil {
		t.Fatalf("set isolation context: %v", err)
	}
}

func boolText(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func callB4ReportingProcedure(ctx context.Context, conn *pgx.Conn, name, reportID string) error {
	switch name {
	case "read_report_printout":
		var payload []byte
		return conn.QueryRow(ctx, `select public.read_report_printout($1::uuid)`, reportID).Scan(&payload)
	case "read_anomaly_export":
		rows, err := conn.Query(ctx, `select * from public.read_anomaly_export()`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
		}
		return rows.Err()
	case "read_audit_export":
		rows, err := conn.Query(ctx, `select * from public.read_audit_export()`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
		}
		return rows.Err()
	case "read_policy_history":
		rows, err := conn.Query(ctx, `select * from public.read_policy_history()`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
		}
		return rows.Err()
	case "read_audit_chain":
		rows, err := conn.Query(ctx, `select * from public.read_audit_chain()`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
		}
		return rows.Err()
	case "fn_verify_audit_chain":
		var verified bool
		return conn.QueryRow(ctx, `select public.fn_verify_audit_chain()`).Scan(&verified)
	default:
		return errors.New("unknown reporting procedure")
	}
}
