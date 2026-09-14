package gormstore

import (
	"context"
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
