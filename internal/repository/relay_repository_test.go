package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	appaudit "github.com/pomkita/pomkita-be/internal/service/audit"
)

func TestRelayRepository_ClaimAndFinishSupportsLeaseRetry(t *testing.T) {
	ctx := context.Background()
	fixture := newGovernanceFixture(t, ctx)
	defer fixture.cleanup()
	now := fixture.now
	eventID := uuid.New()
	if _, err := NewAuditRepository(fixture.store).Append(ctx, appaudit.AppendRequest{OrgID: fixture.orgID, EventID: eventID, EventType: "test", Payload: []byte(`{"ok":true}`), Outcome: "success"}, now); err != nil {
		t.Fatalf("append event: %v", err)
	}
	repository := NewRelayRepository(fixture.store)
	event, lease, claimed, err := repository.ClaimAt(ctx, fixture.orgID, eventID, now)
	if err != nil || !claimed || lease == uuid.Nil || event.EventID != eventID {
		t.Fatalf("claim: event=%+v lease=%v claimed=%v err=%v", event, lease, claimed, err)
	}
	if err := repository.FinishAt(ctx, fixture.orgID, eventID, lease, false, "sink unavailable", now); err != nil {
		t.Fatalf("finish failed delivery: %v", err)
	}
	if _, _, claimed, err := repository.ClaimAt(ctx, fixture.orgID, eventID, now); err != nil || claimed {
		t.Fatalf("claimed before retry window: claimed=%v err=%v", claimed, err)
	}
	if _, _, claimed, err := repository.ClaimAt(ctx, fixture.orgID, eventID, now.Add(time.Hour)); err != nil || !claimed {
		t.Fatalf("claimed after retry window: claimed=%v err=%v", claimed, err)
	}
}
