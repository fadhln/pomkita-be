package policy

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	apppolicy "github.com/pomkita/pomkita-be/internal/service/policy"
)

func TestPolicyRepository_CreateRevision_PersistsExactThresholds(t *testing.T) {
	ctx := context.Background()
	fixture := newGovernanceFixture(t, ctx)
	defer fixture.cleanup()
	if err := fixture.store.DB.Table("user_station_roles").Create(map[string]any{"org_id": fixture.orgID, "station_id": fixture.stationID, "user_id": fixture.actorID, "role": "Owner"}).Error; err != nil {
		t.Fatalf("create owner role: %v", err)
	}
	request := apppolicy.PolicyRevisionRequest{OrgID: fixture.orgID, StationID: fixture.stationID, ActorID: fixture.actorID, Role: "Owner", PolicyKind: "threshold", PolicyID: uuid.New(), ValidFrom: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC), LossLiterThreshold: "1.25", GainLiterThreshold: "2.50", LossRupiahThreshold: "100", GainRupiahThreshold: "200", VarianceRupiahThreshold: "300", RolloverThreshold: "100000.0"}
	result, err := NewPolicyRepository(fixture.store).CreateRevision(ctx, request, fixture.now)
	if err != nil {
		t.Fatalf("create revision: %v", err)
	}
	if result.RevisionID == uuid.Nil || result.PolicyKind != "threshold" {
		t.Fatalf("revision result: %+v", result)
	}
	var revision ThresholdPolicyRevisionModel
	if err := fixture.store.DB.Where("rev_id = ?", result.RevisionID).First(&revision).Error; err != nil {
		t.Fatalf("load revision: %v", err)
	}
	if revision.LossLiterThreshold.String() != "1.25" || revision.RolloverThreshold.String() != "100000.0" {
		t.Fatalf("exact thresholds: loss=%q rollover=%q", revision.LossLiterThreshold, revision.RolloverThreshold)
	}
	var auditCount int64
	if err := fixture.store.DB.Model(&AuditLogModel{}).Where("org_id = ? and event_id = ?", fixture.orgID, result.RevisionID).Count(&auditCount).Error; err != nil {
		t.Fatalf("count policy audit events: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("policy audit count: got %d, want 1", auditCount)
	}
}

func TestPolicyRepository_CreateRevision_RejectsOverlapAndRequiresForwardSupersession(t *testing.T) {
	ctx := context.Background()
	fixture := newGovernanceFixture(t, ctx)
	defer fixture.cleanup()
	if err := fixture.store.DB.Table("user_station_roles").Create(map[string]any{"org_id": fixture.orgID, "station_id": fixture.stationID, "user_id": fixture.actorID, "role": "Owner"}).Error; err != nil {
		t.Fatalf("create owner role: %v", err)
	}
	request := apppolicy.PolicyRevisionRequest{OrgID: fixture.orgID, StationID: fixture.stationID, ActorID: fixture.actorID, Role: "Owner", PolicyKind: "threshold", PolicyID: uuid.New(), ValidFrom: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC), LossLiterThreshold: "1.00", GainLiterThreshold: "1.00", LossRupiahThreshold: "100", GainRupiahThreshold: "100", VarianceRupiahThreshold: "100", RolloverThreshold: "100000.0"}
	first, err := NewPolicyRepository(fixture.store).CreateRevision(ctx, request, fixture.now)
	if err != nil {
		t.Fatalf("create first revision: %v", err)
	}
	if _, err := NewPolicyRepository(fixture.store).CreateRevision(ctx, request, fixture.now); err != apppolicy.ErrPolicyOverlap {
		t.Fatalf("duplicate revision error: got %v, want %v", err, apppolicy.ErrPolicyOverlap)
	}
	request.SupersedesRevisionID = &first.RevisionID
	request.ValidFrom = time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	if _, err := NewPolicyRepository(fixture.store).CreateRevision(ctx, request, fixture.now); err != apppolicy.ErrPolicyOverlap {
		t.Fatalf("backward revision error: got %v, want %v", err, apppolicy.ErrPolicyOverlap)
	}
}

func TestPolicyRepository_CreateRevision_PersistsTombstoneReason(t *testing.T) {
	ctx := context.Background()
	fixture := newGovernanceFixture(t, ctx)
	defer fixture.cleanup()
	if err := fixture.store.DB.Table("user_station_roles").Create(map[string]any{"org_id": fixture.orgID, "station_id": fixture.stationID, "user_id": fixture.actorID, "role": "Owner"}).Error; err != nil {
		t.Fatalf("create owner role: %v", err)
	}
	result, err := NewPolicyRepository(fixture.store).CreateRevision(ctx, apppolicy.PolicyRevisionRequest{
		OrgID: fixture.orgID, StationID: fixture.stationID, ActorID: fixture.actorID, Role: "Owner",
		PolicyKind: "threshold", PolicyID: uuid.New(), ValidFrom: fixture.now, Disabled: true,
		TombstoneReason: "replaced by approved revision", LossLiterThreshold: "1", GainLiterThreshold: "1",
		LossRupiahThreshold: "1", GainRupiahThreshold: "1", VarianceRupiahThreshold: "1", RolloverThreshold: "1",
	}, fixture.now)
	if err != nil {
		t.Fatalf("create tombstone: %v", err)
	}
	var reason string
	if err := fixture.store.DB.Table("threshold_policy_revisions").Where("rev_id = ?", result.RevisionID).Pluck("tombstone_reason", &reason).Error; err != nil {
		t.Fatalf("load tombstone reason: %v", err)
	}
	if reason != "replaced by approved revision" {
		t.Fatalf("tombstone reason: got %q", reason)
	}
}

func TestPolicyRepository_History_DoesNotCrossOrganizationForGlobalRevisions(t *testing.T) {
	ctx := context.Background()
	fixture := newGovernanceFixture(t, ctx)
	defer fixture.cleanup()
	otherOrg := uuid.New()
	if err := fixture.store.DB.Table("organizations").Create(map[string]any{"org_id": otherOrg, "name": "Other org"}).Error; err != nil {
		t.Fatalf("create other organization: %v", err)
	}
	otherActor := uuid.New()
	if err := fixture.store.DB.Create(&UserModel{UserID: otherActor, OrgID: otherOrg, DisplayName: "Other owner", Email: "other-owner@example.com", Username: "other-owner", PasswordHash: "hash", Enabled: true, CreatedAt: fixture.now}).Error; err != nil {
		t.Fatalf("create other user: %v", err)
	}
	otherRevision := ThresholdPolicyRevisionModel{RevID: uuid.New(), PolicyID: uuid.New(), OrgID: otherOrg, ValidFrom: fixture.now, LossLiterThreshold: Decimal("1"), GainLiterThreshold: Decimal("1"), LossRupiahThreshold: Decimal("1"), GainRupiahThreshold: Decimal("1"), VarianceThreshold: Decimal("1"), RolloverThreshold: Decimal("1"), CreatedBy: otherActor, CreatedAt: fixture.now}
	if err := fixture.store.DB.Create(&otherRevision).Error; err != nil {
		t.Fatalf("create other global revision: %v", err)
	}
	rows, err := NewPolicyRepository(fixture.store).History(ctx, fixture.orgID, &fixture.stationID)
	if err != nil {
		t.Fatalf("read policy history: %v", err)
	}
	for _, row := range rows {
		if row.RevisionID == otherRevision.RevID {
			t.Fatalf("history returned revision from another organization: %s", row.RevisionID)
		}
	}
}
