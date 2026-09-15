package draft

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestService_WriteReading_RejectsExpiredClaim(t *testing.T) {
	service := NewService(&draftRepositoryStub{err: ErrClaimExpired}, fixedClock{value: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
	_, err := service.WriteReading(context.Background(), WriteReadingRequest{
		DraftID:    uuid.New(),
		ClaimToken: uuid.New(),
		Revision:   3,
		NozzleID:   uuid.New(),
		MeterStart: "10.0",
		MeterEnd:   "12.0",
		ActorID:    uuid.New(),
	})
	if !errors.Is(err, ErrClaimExpired) {
		t.Fatalf("write reading error: got %v, want %v", err, ErrClaimExpired)
	}
}

func TestService_WriteReading_ReturnsNextRevision(t *testing.T) {
	service := NewService(&draftRepositoryStub{nextRevision: 4}, fixedClock{value: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
	nextRevision, err := service.WriteReading(context.Background(), WriteReadingRequest{
		DraftID:    uuid.New(),
		ClaimToken: uuid.New(),
		Revision:   3,
		NozzleID:   uuid.New(),
		MeterStart: "10.0",
		MeterEnd:   "12.0",
		ActorID:    uuid.New(),
	})
	if err != nil {
		t.Fatalf("write reading: %v", err)
	}
	if nextRevision != 4 {
		t.Fatalf("next revision: got %d, want 4", nextRevision)
	}
}

func TestService_StageEvidence_RejectsInvalidHash(t *testing.T) {
	service := NewService(&draftRepositoryStub{}, fixedClock{value: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
	_, err := service.StageEvidence(context.Background(), StageEvidenceRequest{
		DraftID: uuid.New(), ClaimToken: uuid.New(), Revision: 1, LossRowID: uuid.New(),
		EvidenceType: "photo", ObjectKey: "evidence/object", ContentHash: make([]byte, 31), SizeBytes: 10,
		MIME: "image/jpeg", ActorID: uuid.New(),
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("stage evidence error: got %v, want %v", err, ErrInvalidRequest)
	}
}

type fixedClock struct{ value time.Time }

func (c fixedClock) Now() time.Time { return c.value }

type draftRepositoryStub struct {
	err          error
	nextRevision int
}

func (r *draftRepositoryStub) Claim(context.Context, ClaimRequest, time.Time) (ClaimResult, error) {
	return ClaimResult{}, r.err
}

func (r *draftRepositoryStub) Heartbeat(context.Context, HeartbeatRequest, time.Time) error {
	return r.err
}

func (r *draftRepositoryStub) WriteReading(context.Context, WriteReadingRequest, time.Time) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	return r.nextRevision, nil
}
