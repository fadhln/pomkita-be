package gormstore

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
	if err := fixture.store.db.Table("user_station_roles").Create(map[string]any{"org_id": fixture.orgID, "station_id": fixture.stationID, "user_id": fixture.actorID, "role": "Owner"}).Error; err != nil {
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
	if err := fixture.store.db.Where("rev_id = ?", result.RevisionID).First(&revision).Error; err != nil {
		t.Fatalf("load revision: %v", err)
	}
	if revision.LossLiterThreshold.String() != "1.25" || revision.RolloverThreshold.String() != "100000.0" {
		t.Fatalf("exact thresholds: loss=%q rollover=%q", revision.LossLiterThreshold, revision.RolloverThreshold)
	}
}

func TestPolicyRepository_CreateRevision_RejectsOverlapAndRequiresForwardSupersession(t *testing.T) {
	ctx := context.Background()
	fixture := newGovernanceFixture(t, ctx)
	defer fixture.cleanup()
	if err := fixture.store.db.Table("user_station_roles").Create(map[string]any{"org_id": fixture.orgID, "station_id": fixture.stationID, "user_id": fixture.actorID, "role": "Owner"}).Error; err != nil {
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
