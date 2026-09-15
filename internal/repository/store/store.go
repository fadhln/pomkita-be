package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Store owns the GORM connection used by persistence adapters.
type Store struct {
	DB *gorm.DB
}

// Open opens a PostgreSQL-backed GORM store and verifies the connection.
func Open(ctx context.Context, databaseURL string) (*Store, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, errors.New("database URL is required")
	}
	database, err := gorm.Open(postgres.Open(databaseURL), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		return nil, fmt.Errorf("open GORM database: %w", err)
	}
	sqlDatabase, err := database.DB()
	if err != nil {
		return nil, fmt.Errorf("get GORM SQL database: %w", err)
	}
	if err := sqlDatabase.PingContext(ctx); err != nil {
		_ = sqlDatabase.Close()
		return nil, fmt.Errorf("ping GORM database: %w", err)
	}
	return &Store{DB: database}, nil
}

// Close closes the underlying SQL connection pool.
func (s *Store) Close() error {
	if s == nil || s.DB == nil {
		return nil
	}
	sqlDatabase, err := s.DB.DB()
	if err != nil {
		return fmt.Errorf("get GORM SQL database: %w", err)
	}
	return sqlDatabase.Close()
}

// Ping checks database reachability for the readiness endpoint.
func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.DB == nil {
		return errors.New("GORM store is not configured")
	}
	database, err := s.DB.DB()
	if err != nil {
		return fmt.Errorf("get GORM SQL database: %w", err)
	}
	if err := database.PingContext(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	return nil
}

// MigrationsCurrent reports whether the migration state is current.
func (s *Store) MigrationsCurrent(ctx context.Context, expected int) (bool, error) {
	if s == nil || s.DB == nil {
		return false, errors.New("GORM store is not configured")
	}
	var state struct {
		Version int  `gorm:"column:version"`
		Dirty   bool `gorm:"column:dirty"`
	}
	result := s.DB.WithContext(ctx).Table("schema_migrations").Select("version, dirty").Order("version desc").Limit(1).Scan(&state)
	if result.Error != nil {
		return false, fmt.Errorf("read migration state: %w", result.Error)
	}
	return state.Version == expected && !state.Dirty, nil
}

// Transaction runs fn in one database transaction and rolls it back on error.
func (s *Store) Transaction(ctx context.Context, fn func(context.Context, *gorm.DB) error) error {
	if s == nil || s.DB == nil {
		return errors.New("GORM store is not configured")
	}
	if fn == nil {
		return errors.New("transaction callback is required")
	}
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(ctx, tx)
	})
}
