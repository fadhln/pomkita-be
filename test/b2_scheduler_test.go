package test

import (
	"context"
	"testing"
)

func TestB2SchedulerMigration_ProvidesAbandonmentAndGovernanceAnomalyRead(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), "000001_b0_foundation.up.sql")); err != nil {
		t.Fatalf("apply foundation: %v", err)
	}
	for _, name := range []string{"000005_b1_catalog.up.sql", "000006_b1_shifts.up.sql", "000007_b1_drafts.up.sql", "000008_b1_submit.up.sql", "000009_b1_recovery.up.sql", "000010_b2_ack.up.sql", "000011_b2_amendment.up.sql", "000012_b2_audit.up.sql", "000013_b2_relay.up.sql", "000014_b2_scheduler.up.sql"} {
		if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), name)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	if _, err := conn.Exec(ctx, `select fn_abandon_failed_shifts()`); err != nil {
		t.Fatalf("call abandonment scheduler: %v", err)
	}
	var anomalies int
	if err := conn.QueryRow(ctx, `select count(*) from pg_proc where proname='read_governance_anomalies'`).Scan(&anomalies); err != nil {
		t.Fatalf("query anomaly read procedure: %v", err)
	}
	if anomalies != 1 {
		t.Fatal("governance anomaly read procedure is missing")
	}
}
