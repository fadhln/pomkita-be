package shift

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestService_OpenShift_AllocatesOneOpenShift(t *testing.T) {
	orgID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	stationID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	actorID := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	now := time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC)
	repository := &openShiftRepositoryStub{
		result: Shift{
			ShiftID:          uuid.MustParse("44444444-4444-4444-8444-444444444444"),
			StationSeq:       7,
			Status:           StatusOpen,
			PriceMapSnapshot: []byte(`{"version":1,"items":[]}`),
			SupervisorID:     actorID,
		},
	}

	service := NewService(repository, fixedClock{value: now})
	result, err := service.OpenShift(context.Background(), OpenRequest{
		OrgID:     orgID,
		StationID: stationID,
		ActorID:   actorID,
		Role:      "Supervisor",
		OpenedAt:  now,
	})
	if err != nil {
		t.Fatalf("open shift: %v", err)
	}
	if result.ShiftID == uuid.Nil {
		t.Fatal("shift ID is empty")
	}
	if result.StationSeq != 7 {
		t.Fatalf("station sequence: got %d, want 7", result.StationSeq)
	}
	if result.Status != StatusOpen {
		t.Fatalf("status: got %q, want %q", result.Status, StatusOpen)
	}
	if string(result.PriceMapSnapshot) != `{"version":1,"items":[]}` {
		t.Fatalf("snapshot: got %s", result.PriceMapSnapshot)
	}
	if repository.request.OrgID != orgID || repository.request.StationID != stationID || repository.request.ActorID != actorID {
		t.Fatalf("open request: %#v", repository.request)
	}
	if repository.created.Status != StatusOpen || repository.created.SupervisorID != actorID {
		t.Fatalf("created shift: %#v", repository.created)
	}
}

func TestService_OpenShift_RejectsOwnerOperationalAction(t *testing.T) {
	now := time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC)
	service := NewService(&openShiftRepositoryStub{}, fixedClock{value: now})
	_, err := service.OpenShift(context.Background(), OpenRequest{OrgID: uuid.New(), StationID: uuid.New(), ActorID: uuid.New(), Role: "Owner", OpenedAt: now})
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("open shift error: got %v, want %v", err, ErrUnauthorized)
	}
}

func TestService_OpenShift_RejectsIncompleteBackfillApproval(t *testing.T) {
	now := time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC)
	service := NewService(&openShiftRepositoryStub{}, fixedClock{value: now})
	_, err := service.OpenShift(context.Background(), OpenRequest{OrgID: uuid.New(), StationID: uuid.New(), ActorID: uuid.New(), Role: "Supervisor", Backfilled: true, OpenedAt: now})
	if !errors.Is(err, ErrBackfillApprovalRequired) {
		t.Fatalf("backfill error: got %v, want %v", err, ErrBackfillApprovalRequired)
	}
}

type fixedClock struct{ value time.Time }

func (c fixedClock) Now() time.Time { return c.value }

type openShiftRepositoryStub struct {
	request OpenRequest
	result  Shift
	created Shift
}

func (r *openShiftRepositoryStub) OpenShift(_ context.Context, request OpenRequest) (Shift, error) {
	r.request = request
	shift := r.result
	r.created = shift
	return shift, nil
}
