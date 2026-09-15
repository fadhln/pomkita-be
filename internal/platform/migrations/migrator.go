// Package migrations applies the SQL migration sequence.
package migrations

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

// Migrator applies and reverses one ordered SQL migration source.
type Migrator struct {
	databaseURL string
	sourceURL   string
}

// New creates a migrator for an existing migration directory.
func New(databaseURL, migrationsDirectory string) (*Migrator, error) {
	if databaseURL == "" {
		return nil, fmt.Errorf("database URL is required")
	}
	absolutePath, err := filepath.Abs(migrationsDirectory)
	if err != nil {
		return nil, fmt.Errorf("resolve migration directory: %w", err)
	}
	info, err := os.Stat(absolutePath)
	if err != nil {
		return nil, fmt.Errorf("stat migration directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("migration path is not a directory: %s", absolutePath)
	}
	return &Migrator{
		databaseURL: databaseURL,
		sourceURL:   (&url.URL{Scheme: "file", Path: absolutePath}).String(),
	}, nil
}

// Up applies all pending migrations.
func (m *Migrator) Up(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	runner, err := migrate.New(m.sourceURL, m.databaseURL)
	if err != nil {
		return fmt.Errorf("open migration runner: %w", err)
	}
	defer closeRunner(runner)
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
	defer closeRunner(runner)
	if err := runner.Down(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("reverse migrations: %w", err)
	}
	return nil
}

func closeRunner(runner *migrate.Migrate) {
	_, _ = runner.Close()
}
