package migrations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrationSet_DefinesStationDetailAndReversesOnlyItsChanges(t *testing.T) {
	root := repositoryRoot(t)
	up, err := os.ReadFile(filepath.Join(root, "migrations", "000015_station_detail.up.sql"))
	if err != nil {
		t.Fatalf("read station up migration: %v", err)
	}
	down, err := os.ReadFile(filepath.Join(root, "migrations", "000015_station_detail.down.sql"))
	if err != nil {
		t.Fatalf("read station down migration: %v", err)
	}
	upText, downText := strings.ToLower(string(up)), strings.ToLower(string(down))
	for _, required := range []string{
		"add column code text",
		"add column address text",
		"add column enabled boolean not null default true",
		"add column updated_at timestamptz(6)",
		"unique index stations_org_code_ci",
		"lower(code)",
		"alter column name drop default",
	} {
		if !strings.Contains(upText, required) {
			t.Fatalf("station migration does not contain %q", required)
		}
	}
	for _, required := range []string{
		"drop index if exists stations_org_code_ci",
		"drop column if exists code",
		"drop column if exists address",
		"drop column if exists enabled",
		"drop column if exists updated_at",
		"alter column name set default 'station'",
	} {
		if !strings.Contains(downText, required) {
			t.Fatalf("station down migration does not contain %q", required)
		}
	}
	for _, forbidden := range []string{"create function", "create procedure", "create trigger", "create role", "grant "} {
		if strings.Contains(upText, forbidden) || strings.Contains(downText, forbidden) {
			t.Fatalf("station migration contains forbidden database behavior %q", forbidden)
		}
	}
}
