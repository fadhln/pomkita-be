package test

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestB4BackupRunbook_DescribesDumpWALRestoreAndChecksum(t *testing.T) {
	content, err := os.ReadFile(filepath.Join(repositoryRoot(t), "docs", "backup-restore.md"))
	if err != nil {
		t.Fatalf("read backup runbook: %v", err)
	}
	for _, required := range []string{"pg_dump", "archive_mode", "restore", "checksum", "RPO"} {
		if !strings.Contains(string(content), required) {
			t.Fatalf("backup runbook does not contain %q", required)
		}
	}
}

func TestB4BackupRestore_RehearsalPreservesChecksum(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	resetMigrations(t, conn)
	applyB3Migrations(t, conn)
	defer resetB0Foundation(t, conn, true)
	seed, err := os.ReadFile(filepath.Join(repositoryRoot(t), "seed", "demo.sql"))
	if err != nil {
		t.Fatalf("read demo seed: %v", err)
	}
	if _, err := conn.Exec(ctx, string(seed)); err != nil {
		t.Fatalf("apply demo seed: %v", err)
	}

	sourceURL := testDatabaseURL()
	source, err := url.Parse(sourceURL)
	if err != nil {
		t.Fatalf("parse database URL: %v", err)
	}
	targetName := fmt.Sprintf("pomkita_b4_restore_%d", time.Now().UnixNano())
	if _, err := conn.Exec(ctx, `create database `+targetName); err != nil {
		t.Fatalf("create restore database: %v", err)
	}
	defer func() {
		if _, err := conn.Exec(ctx, `drop database if exists `+targetName); err != nil {
			t.Errorf("drop restore database: %v", err)
		}
	}()

	dump := runDatabaseTool(t, "pg_dump", "--format=custom", "--no-owner", "--no-privileges", "--dbname="+sourceURL)
	restore := runDatabaseToolWithInput(t, dump, "pg_restore", "--exit-on-error", "--no-owner", "--no-privileges", "--dbname="+databaseURLForName(source, targetName))
	if len(restore) != 0 {
		t.Fatalf("restore wrote unexpected output")
	}

	sourceChecksum := databaseChecksum(t, sourceURL)
	targetChecksum := databaseChecksum(t, databaseURLForName(source, targetName))
	if sourceChecksum != targetChecksum {
		t.Fatalf("restore checksum mismatch: source=%s target=%s", sourceChecksum, targetChecksum)
	}
}

func runDatabaseTool(t *testing.T, name string, args ...string) []byte {
	t.Helper()
	if _, err := exec.LookPath(name); err == nil {
		output, err := exec.Command(name, args...).Output()
		if err != nil {
			t.Fatalf("run %s: %v", name, err)
		}
		return output
	}
	containerArgs := append([]string{"exec", "pomkita-pg", name, "-U", "pomkita"}, args...)
	output, err := exec.Command("docker", containerArgs...).Output()
	if err != nil {
		t.Fatalf("run container %s: %v", name, err)
	}
	return output
}

func runDatabaseToolWithInput(t *testing.T, input []byte, name string, args ...string) []byte {
	t.Helper()
	if _, err := exec.LookPath(name); err == nil {
		command := exec.Command(name, args...)
		command.Stdin = bytes.NewReader(input)
		output, err := command.Output()
		if err != nil {
			t.Fatalf("run %s: %v", name, err)
		}
		return output
	}
	containerArgs := append([]string{"exec", "-i", "pomkita-pg", name, "-U", "pomkita"}, args...)
	command := exec.Command("docker", containerArgs...)
	command.Stdin = bytes.NewReader(input)
	output, err := command.Output()
	if err != nil {
		t.Fatalf("run container %s: %v", name, err)
	}
	return output
}

func databaseURLForName(source *url.URL, name string) string {
	copyOfURL := *source
	copyOfURL.Path = "/" + name
	return copyOfURL.String()
}

func databaseChecksum(t *testing.T, databaseURL string) string {
	t.Helper()
	conn, err := pgx.Connect(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("connect checksum database: %v", err)
	}
	defer conn.Close(context.Background())
	var checksum string
	err = conn.QueryRow(context.Background(), `
		select encode(app.digest(convert_to(coalesce(string_agg(
		  table_name || ':' || row_count::text, E'\n' order by table_name), ''), 'UTF8'), 'sha256'), 'hex')
		from (
		  select table_name, count(*) as row_count
		  from information_schema.tables
		  where table_schema = 'public'
		  group by table_name
		) counts`).Scan(&checksum)
	if err != nil {
		t.Fatalf("read database checksum: %v", err)
	}
	return checksum
}
