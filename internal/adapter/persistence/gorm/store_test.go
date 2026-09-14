package gormstore

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestStoreTransaction_RollsBackAllWritesWhenCallbackFails(t *testing.T) {
	ctx := context.Background()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://pomkita:pomkita_dev@127.0.0.1:5432/pomkita?sslmode=disable"
	}
	store, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open GORM store: %v", err)
	}
	defer store.Close()

	table := "phase1_transaction_probe_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := store.db.Exec(`create table ` + table + ` (id uuid primary key, value text not null)`).Error; err != nil {
		t.Fatalf("create probe table: %v", err)
	}
	defer store.db.Exec(`drop table if exists ` + table)

	wantErr := errors.New("force rollback")
	err = store.Transaction(ctx, func(_ context.Context, tx *gorm.DB) error {
		if err := tx.Table(table).Create(map[string]any{"id": uuid.New(), "value": "not committed"}).Error; err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("transaction error: got %v, want %v", err, wantErr)
	}

	var count int64
	if err := store.db.Table(table).Count(&count).Error; err != nil {
		t.Fatalf("count probe rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("probe rows after rollback: got %d, want 0", count)
	}
}
