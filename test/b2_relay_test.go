package test

import (
	"context"
	"testing"
)

func TestB2RelayMigration_ProvidesLeaseStateAndClaimProcedures(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	applyB2Migrations(t, conn)
	defer resetB0Foundation(t, conn, true)
	if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), "000012_b2_audit.up.sql")); err != nil {
		t.Fatalf("apply audit migration: %v", err)
	}
	if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), "000013_b2_relay.up.sql")); err != nil {
		t.Fatalf("apply relay migration: %v", err)
	}
	var exists bool
	if err := conn.QueryRow(ctx, `select to_regclass('public.outbox_relay_state') is not null`).Scan(&exists); err != nil {
		t.Fatalf("query relay state: %v", err)
	}
	if !exists {
		t.Fatal("relay state table is missing")
	}
	if _, err := conn.Exec(ctx, `select * from fn_relay_claim_event(null,null)`); err == nil {
		t.Fatal("relay claim accepted an invalid request")
	}
}
