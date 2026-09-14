package gormstore

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	appgovernance "github.com/pomkita/pomkita-be/internal/service/governance"
)

func TestAlertRepository_RecordOccurrence_IsIdempotentAndClears(t *testing.T) {
	ctx := context.Background()
	fixture := newGovernanceFixture(t, ctx)
	defer fixture.cleanup()
	now := fixture.now
	ruleID := uuid.New()
	if err := fixture.store.db.Create(&AlertRuleModel{RuleID: ruleID, OrgID: fixture.orgID, StationID: fixture.stationID, RuleType: "starvation", AlertKey: "starvation", Threshold: Decimal("24"), Enabled: true, Channel: "in_app", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create alert rule: %v", err)
	}
	period := now.Truncate(time.Hour)
	request := appgovernance.AlertOccurrenceRequest{OrgID: fixture.orgID, StationID: fixture.stationID, RuleID: ruleID, SubjectKind: "shift", SubjectID: fixture.shiftID, EventType: "fired", PeriodStart: period, SourceKind: "scheduler", SourceID: fixture.shiftID, SourceAt: now}
	first, err := NewGovernanceRepository(fixture.store).RecordAlertOccurrence(ctx, request, now)
	if err != nil {
		t.Fatalf("fire alert: %v", err)
	}
	if !first.Inserted || first.EventID == uuid.Nil {
		t.Fatalf("first alert: %+v", first)
	}
	second, err := NewGovernanceRepository(fixture.store).RecordAlertOccurrence(ctx, request, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("replay alert: %v", err)
	}
	if second.Inserted || second.EventID != first.EventID {
		t.Fatalf("replay alert: %+v, first=%+v", second, first)
	}
	related := first.EventID
	cleared, err := NewGovernanceRepository(fixture.store).RecordAlertOccurrence(ctx, appgovernance.AlertOccurrenceRequest{OrgID: fixture.orgID, StationID: fixture.stationID, RuleID: ruleID, SubjectKind: "shift", SubjectID: fixture.shiftID, EventType: "cleared", PeriodStart: period, RelatedFiredEventID: &related, SourceKind: "shift_transition", SourceID: fixture.shiftID, SourceAt: now}, now)
	if err != nil {
		t.Fatalf("clear alert: %v", err)
	}
	if !cleared.Inserted || cleared.EventID == uuid.Nil || cleared.RelatedFiredID == nil || *cleared.RelatedFiredID != first.EventID {
		t.Fatalf("cleared alert: %+v", cleared)
	}
}
