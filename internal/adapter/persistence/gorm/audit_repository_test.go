package gormstore

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	appaudit "github.com/pomkita/pomkita-be/internal/service/audit"
)

func TestAuditRepository_AppendBuildsAndVerifiesChainWithOutbox(t *testing.T) {
	ctx := context.Background()
	fixture := newGovernanceFixture(t, ctx)
	defer fixture.cleanup()
	now := fixture.now
	repository := NewAuditRepository(fixture.store)
	first, err := repository.Append(ctx, appaudit.AppendRequest{OrgID: fixture.orgID, EventID: uuid.New(), EventType: "test", Payload: []byte(`{"ok":true}`), Outcome: "success"}, now)
	if err != nil {
		t.Fatalf("append first audit event: %v", err)
	}
	second, err := repository.Append(ctx, appaudit.AppendRequest{OrgID: fixture.orgID, EventID: uuid.New(), EventType: "test", Payload: []byte(`{"ok":false}`), Outcome: "success"}, now)
	if err != nil {
		t.Fatalf("append second audit event: %v", err)
	}
	if first.OrgSequence != 1 || second.OrgSequence != 2 {
		t.Fatalf("sequences: first=%d second=%d", first.OrgSequence, second.OrgSequence)
	}
	var outboxCount int64
	if err := fixture.store.db.Model(&AuditOutboxModel{}).Where("org_id = ?", fixture.orgID).Count(&outboxCount).Error; err != nil {
		t.Fatalf("count audit outbox: %v", err)
	}
	if outboxCount != 2 {
		t.Fatalf("outbox count: got %d, want 2", outboxCount)
	}
	if err := repository.Verify(ctx, fixture.orgID); err != nil {
		t.Fatalf("verify audit chain: %v", err)
	}
	if err := fixture.store.db.Model(&AuditLogModel{}).Where("event_id = ?", first.EventID).Update("row_hash", make([]byte, 32)).Error; err != nil {
		t.Fatalf("tamper audit row: %v", err)
	}
	if err := repository.Verify(ctx, fixture.orgID); err != appaudit.ErrChainTampered {
		t.Fatalf("tampered chain error: got %v, want %v", err, appaudit.ErrChainTampered)
	}
}

func TestAuditRepository_ConcurrentFirstAppendsAllocateContiguousSequence(t *testing.T) {
	ctx := context.Background()
	fixture := newGovernanceFixture(t, ctx)
	defer fixture.cleanup()
	repository := NewAuditRepository(fixture.store)
	const count = 20
	results := make(chan appaudit.Event, count)
	errors := make(chan error, count)
	var group sync.WaitGroup
	for range count {
		group.Add(1)
		go func() {
			defer group.Done()
			event, err := repository.Append(ctx, appaudit.AppendRequest{OrgID: fixture.orgID, EventID: uuid.New(), EventType: "concurrent_test", Payload: []byte(`{"ok":true}`), Outcome: "success"}, fixture.now)
			if err != nil {
				errors <- err
				return
			}
			results <- event
		}()
	}
	group.Wait()
	close(results)
	close(errors)
	for err := range errors {
		t.Fatalf("concurrent append: %v", err)
	}
	sequences := make(map[int64]bool, count)
	for event := range results {
		sequences[event.OrgSequence] = true
	}
	if len(sequences) != count {
		t.Fatalf("sequence count: got %d, want %d", len(sequences), count)
	}
	for sequence := int64(1); sequence <= count; sequence++ {
		if !sequences[sequence] {
			t.Fatalf("missing sequence %d", sequence)
		}
	}
}
