package repository

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
	if err := fixture.store.DB.Create(&AlertRuleModel{RuleID: ruleID, OrgID: fixture.orgID, StationID: fixture.stationID, RuleType: "starvation", AlertKey: "starvation", Threshold: Decimal("24"), Enabled: true, Channel: "in_app", CreatedAt: now}).Error; err != nil {
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

func TestAlertScheduler_EvaluatesStarvationIdempotentlyAndClearsOnLock(t *testing.T) {
	ctx := context.Background()
	fixture := newGovernanceFixture(t, ctx)
	defer fixture.cleanup()
	now := fixture.now
	ruleID := uuid.New()
	if err := fixture.store.DB.Create(&AlertRuleModel{RuleID: ruleID, OrgID: fixture.orgID, StationID: fixture.stationID, RuleType: "starvation", AlertKey: "starvation", Threshold: Decimal("24"), Enabled: true, Channel: "in_app", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create alert rule: %v", err)
	}
	if err := fixture.store.DB.Model(&ShiftModel{}).Where("shift_id = ?", fixture.shiftID).Update("opened_at", now.Add(-25*time.Hour)).Error; err != nil {
		t.Fatalf("age shift: %v", err)
	}
	scheduler := appgovernance.NewAlertScheduler(NewGovernanceRepository(fixture.store), fixedAlertClock{now: now})
	count, err := scheduler.Run(ctx, 24*time.Hour)
	if err != nil || count != 1 {
		t.Fatalf("first scheduler run: count=%d err=%v", count, err)
	}
	count, err = scheduler.Run(ctx, 24*time.Hour)
	if err != nil || count != 0 {
		t.Fatalf("repeated scheduler run: count=%d err=%v", count, err)
	}
	var firedCount int64
	if err := fixture.store.DB.Model(&AlertEventModel{}).Where("rule_id = ? and event_type = ?", ruleID, "fired").Count(&firedCount).Error; err != nil {
		t.Fatalf("count fired events: %v", err)
	}
	if firedCount != 1 {
		t.Fatalf("fired event count: got %d, want 1", firedCount)
	}
	if err := fixture.store.DB.Model(&ShiftModel{}).Where("shift_id = ?", fixture.shiftID).Update("status", "locked").Error; err != nil {
		t.Fatalf("lock shift: %v", err)
	}
	count, err = scheduler.Run(ctx, 24*time.Hour)
	if err != nil || count != 1 {
		t.Fatalf("clear scheduler run: count=%d err=%v", count, err)
	}
	var clearedCount int64
	if err := fixture.store.DB.Model(&AlertEventModel{}).Where("rule_id = ? and event_type = ?", ruleID, "cleared").Count(&clearedCount).Error; err != nil {
		t.Fatalf("count cleared events: %v", err)
	}
	if clearedCount != 1 {
		t.Fatalf("cleared event count: got %d, want 1", clearedCount)
	}
}

type fixedAlertClock struct{ now time.Time }

func (c fixedAlertClock) Now() time.Time { return c.now }
