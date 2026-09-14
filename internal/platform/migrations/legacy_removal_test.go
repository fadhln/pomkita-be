package migrations

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyProcedureSourcesAreRemoved(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	for _, relative := range []string{
		"internal/db",
		"internal/httpapi/shift_handlers.go",
		"internal/httpapi/governance_handlers.go",
		"internal/httpapi/reporting_handlers.go",
		"migrations/000001_b0_foundation.up.sql",
		"test",
	} {
		if _, err := os.Stat(filepath.Join(root, relative)); !os.IsNotExist(err) {
			t.Fatalf("legacy source remains at %s", relative)
		}
	}
}
