package migrations

import (
	"context"
	"testing"
)

func TestNew_RejectsAMissingMigrationDirectory(t *testing.T) {
	_, err := New("postgres://example", "/path/that/does/not/exist")
	if err == nil {
		t.Fatal("New accepted a missing migration directory")
	}
}

func TestMigrator_StopsBeforeOpeningWhenContextIsCanceled(t *testing.T) {
	directory := t.TempDir()
	migrator, err := New("postgres://example", directory)
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := migrator.Up(ctx); err == nil {
		t.Fatal("Up accepted a canceled context")
	}
	if err := migrator.Down(ctx); err == nil {
		t.Fatal("Down accepted a canceled context")
	}
}
