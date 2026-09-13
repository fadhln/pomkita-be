package test

import (
	"context"
	"testing"
)

func TestB2AckMigration_ProvidesDecisionHeadAndProcedure(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	resetB0Foundation(t, conn, true)
	for _, name := range []string{
		"000005_b1_catalog.up.sql",
		"000006_b1_shifts.up.sql",
		"000007_b1_drafts.up.sql",
		"000008_b1_submit.up.sql",
		"000009_b1_recovery.up.sql",
	} {
		if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), name)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), "000010_b2_ack.up.sql")); err != nil {
		t.Fatalf("apply ack migration: %v", err)
	}

	var tables int
	if err := conn.QueryRow(ctx, `
		select count(*)
		from pg_class c
		join pg_namespace n on n.oid = c.relnamespace
		where n.nspname = 'public'
		  and c.relname = any($1::text[])
	`, []string{"ack_decisions", "ack_head", "ack_supersessions"}).Scan(&tables); err != nil {
		t.Fatalf("query ack tables: %v", err)
	}
	if tables != 3 {
		t.Fatalf("ack table count: got %d, want 3", tables)
	}
	if _, err := conn.Exec(ctx, `select fn_ack_shift(null, null, null, null, null, null, null)`); err == nil {
		t.Fatal("ack procedure accepted an invalid call")
	}
}
