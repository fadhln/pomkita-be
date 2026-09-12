package test

import (
	"context"
	"testing"
)

func TestB1Recovery_ProcedureAndReadSurfaceExist(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	defer func() {
		if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), "000008_b1_recovery.down.sql")); err != nil {
			t.Errorf("clean up recovery: %v", err)
		}
		for _, name := range []string{"000007_b1_submit.down.sql", "000006_b1_drafts.down.sql", "000005_b1_shifts.down.sql", "000004_b1_catalog.down.sql"} {
			if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), name)); err != nil {
				t.Errorf("clean up %s: %v", name, err)
			}
		}
	}()
	if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), "000008_b1_recovery.down.sql")); err != nil {
		t.Fatalf("reset recovery: %v", err)
	}
	resetB0Foundation(t, conn, true)
	for _, name := range []string{"000004_b1_catalog.up.sql", "000005_b1_shifts.up.sql", "000006_b1_drafts.up.sql", "000007_b1_submit.up.sql"} {
		if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), name)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), "000008_b1_recovery.up.sql")); err != nil {
		t.Fatalf("apply recovery: %v", err)
	}
	var recoveryProcedure, shiftList, shiftDetail, reportView string
	if err := conn.QueryRow(ctx, `select to_regprocedure('public.fn_recover_submitting_shifts()'), to_regprocedure('public.read_shift_list(uuid)'), to_regprocedure('public.read_shift_detail(uuid)'), to_regprocedure('public.read_report(uuid)')`).Scan(&recoveryProcedure, &shiftList, &shiftDetail, &reportView); err != nil {
		t.Fatalf("read recovery procedures: %v", err)
	}
	if recoveryProcedure == "" || shiftList == "" || shiftDetail == "" || reportView == "" {
		t.Fatalf("missing recovery procedure: recovery=%q list=%q detail=%q report=%q", recoveryProcedure, shiftList, shiftDetail, reportView)
	}
	var recovered int
	if err := conn.QueryRow(ctx, `select fn_recover_submitting_shifts()`).Scan(&recovered); err != nil {
		t.Fatalf("empty recovery run: %v", err)
	}
	if recovered != 0 {
		t.Fatalf("empty recovery run: got %d recovered rows, want 0", recovered)
	}
}
