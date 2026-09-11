package db

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// Migrator applies and reverses the versioned SQL migrations.
type Migrator struct {
	databaseURL string
	sourceURL   string
}

// NewMigrator creates a migration runner for a local migrations directory.
func NewMigrator(databaseURL, migrationsDir string) (*Migrator, error) {
	absolutePath, err := filepath.Abs(migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("resolve migrations directory: %w", err)
	}
	info, err := os.Stat(absolutePath)
	if err != nil {
		return nil, fmt.Errorf("stat migrations directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("migrations path is not a directory: %s", absolutePath)
	}
	return &Migrator{databaseURL: databaseURL, sourceURL: (&url.URL{Scheme: "file", Path: absolutePath}).String()}, nil
}

// Up applies all pending migrations. No change is a successful result.
func (m *Migrator) Up(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	runner, err := migrate.New(m.sourceURL, m.databaseURL)
	if err != nil {
		return fmt.Errorf("open migration runner: %w", err)
	}
	defer closeMigrator(runner)
	if err := runner.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// Down reverses all applied migrations.
func (m *Migrator) Down(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	runner, err := migrate.New(m.sourceURL, m.databaseURL)
	if err != nil {
		return fmt.Errorf("open migration runner: %w", err)
	}
	defer closeMigrator(runner)
	if err := runner.Down(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("reverse migrations: %w", err)
	}
	return nil
}

func closeMigrator(runner *migrate.Migrate) {
	_, _ = runner.Close()
}
