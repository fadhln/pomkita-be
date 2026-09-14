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
	for _, relative := range []string{
		"internal/adapter/persistence/gorm/auth_repository.go",
		"internal/adapter/persistence/gorm/models.go",
	} {
		contents, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			t.Fatalf("read source %s: %v", relative, err)
		}
		for _, marker := range []string{"NewLegacyAuthRepository", "legacySessionModel", "legacy  bool"} {
			if string(contents) != "" && containsText(string(contents), marker) {
				t.Fatalf("legacy compatibility marker %q remains in %s", marker, relative)
			}
		}
	}
}

func containsText(contents, marker string) bool {
	for index := 0; index+len(marker) <= len(contents); index++ {
		if contents[index:index+len(marker)] == marker {
			return true
		}
	}
	return false
}
