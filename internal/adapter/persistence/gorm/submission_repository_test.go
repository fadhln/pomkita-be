package gormstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	appsubmission "github.com/pomkita/pomkita-be/internal/service/submission"
)

func TestSubmissionRepository_Submit_IsIdempotentByRequestHash(t *testing.T) {
	ctx := context.Background()
	store, cleanup := newAuthTestStore(t, ctx)
	defer cleanup()

	now := time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC)
	orgID, stationID, userID := uuid.New(), uuid.New(), uuid.New()
	shiftID, draftID, claimToken, policySetID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	if err := store.db.Create(&OrganizationModel{OrgID: orgID, Name: "Test Org", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create organization: %v", err)
	}
	if err := store.db.Create(&StationModel{OrgID: orgID, StationID: stationID, Timezone: "UTC", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := store.db.Create(&UserModel{UserID: userID, OrgID: orgID, DisplayName: "Supervisor", Email: "submit@example.com", PasswordHash: "hash", Enabled: true, CreatedAt: now}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := store.db.Create(&ShiftModel{ShiftID: shiftID, OrgID: orgID, StationID: stationID, StationSeq: 1, SupervisorID: userID, OpenedAt: now, TimezoneSnapshot: "UTC", BusinessDate: "2026-01-02", Status: "open", Backfilled: false, PriceMapSnapshot: []byte(`{"hash_version":1,"items":[]}`), PriceMapHash: make([]byte, 32)}).Error; err != nil {
		t.Fatalf("create shift: %v", err)
	}
	if err := store.db.Create(&ShiftDraftModel{DraftID: draftID, OrgID: orgID, StationID: stationID, ShiftID: shiftID, OwnedBy: &userID, ClaimToken: &claimToken, ClaimExpiresAt: ptrTime(now.Add(time.Hour)), Status: "editing", Revision: 1, UpdatedBy: &userID, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if err := store.db.Create(&PolicySnapshotSetModel{SetID: policySetID, OrgID: orgID, StationID: stationID, ShiftID: &shiftID, CreatedAt: now}).Error; err != nil {
		t.Fatalf("create policy snapshot set: %v", err)
	}

	service := appsubmission.NewService(NewSubmissionRepository(store), submissionClock{value: now})
	request := appsubmission.Request{OrgID: orgID, StationID: stationID, ShiftID: shiftID, DraftID: draftID, ClaimToken: claimToken, ExpectedRevision: 1, ActorID: userID, IdempotencyKey: "submit-1", Payload: []byte(`{"readings":[],"sales":[],"losses":[]}`)}
	first, err := service.Submit(ctx, request)
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}
	second, err := service.Submit(ctx, request)
	if err != nil {
		t.Fatalf("replay submit: %v", err)
	}
	if !second.Replay || first.ReportID != second.ReportID {
		t.Fatalf("replay result: first=%+v second=%+v", first, second)
	}
	request.Payload = []byte(`{"readings":[{"nozzle_id":"different"}],"sales":[],"losses":[]}`)
	if _, err := service.Submit(ctx, request); !errors.Is(err, appsubmission.ErrIdempotencyConflict) {
		t.Fatalf("hash conflict: got %v, want %v", err, appsubmission.ErrIdempotencyConflict)
	}
	var reportCount int64
	if err := store.db.Model(&ShiftReportModel{}).Where("shift_id = ?", shiftID).Count(&reportCount).Error; err != nil {
		t.Fatalf("count reports: %v", err)
	}
	if reportCount != 1 {
		t.Fatalf("report count: got %d, want 1", reportCount)
	}
}

type submissionClock struct{ value time.Time }

func (c submissionClock) Now() time.Time { return c.value }

func ptrTime(value time.Time) *time.Time { return &value }
