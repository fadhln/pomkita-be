// Package submission contains draft submission and report creation use cases.
package submission

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/pomkita/pomkita-be/internal/domain"
)

var (
	// ErrInvalidRequest identifies an invalid submission request.
	ErrInvalidRequest = domain.NewError(domain.CategoryValidation, "invalid_submit_request")
	// ErrIdempotencyConflict identifies a reused key with a different or failed request.
	ErrIdempotencyConflict = domain.NewError(domain.CategoryConflict, "idempotency_conflict")
	// ErrDependencyUnavailable identifies a missing submission dependency.
	ErrDependencyUnavailable = domain.NewError(domain.CategoryDependency, "dependency_unavailable")
)

// Clock provides the current time to submission operations.
type Clock interface {
	Now() time.Time
}

// Request contains the immutable request identity and JSON payload.
type Request struct {
	OrgID            uuid.UUID
	StationID        uuid.UUID
	ShiftID          uuid.UUID
	DraftID          uuid.UUID
	ClaimToken       uuid.UUID
	ExpectedRevision int
	ActorID          uuid.UUID
	IdempotencyKey   string
	Payload          []byte
}

// Result identifies the created report and whether the request was replayed.
type Result struct {
	ReportID    uuid.UUID
	Replay      bool
	RequestHash []byte
}

// Repository performs the complete submission transaction.
type Repository interface {
	Submit(context.Context, Request, []byte, []byte, time.Time) (Result, error)
}

// Service owns canonical request hashing and submission validation.
type Service struct {
	repository Repository
	clock      Clock
}

// NewService creates a submission service.
func NewService(repository Repository, clock Clock) *Service {
	return &Service{repository: repository, clock: clock}
}

// Submit canonicalizes the payload and delegates one atomic report transaction.
func (s *Service) Submit(ctx context.Context, request Request) (Result, error) {
	if s == nil || s.repository == nil || s.clock == nil {
		return Result{}, ErrDependencyUnavailable
	}
	if request.OrgID == uuid.Nil || request.StationID == uuid.Nil || request.ShiftID == uuid.Nil || request.DraftID == uuid.Nil || request.ClaimToken == uuid.Nil || request.ActorID == uuid.Nil || request.ExpectedRevision < 1 || len(bytes.TrimSpace(request.Payload)) == 0 || len(bytes.TrimSpace([]byte(request.IdempotencyKey))) == 0 {
		return Result{}, ErrInvalidRequest
	}
	payload, err := canonicalJSON(request.Payload)
	if err != nil {
		return Result{}, ErrInvalidRequest
	}
	hash := sha256.Sum256(payload)
	result, err := s.repository.Submit(ctx, request, hash[:], payload, s.clock.Now().UTC())
	if err != nil {
		return Result{}, err
	}
	result.RequestHash = append([]byte(nil), hash[:]...)
	return result, nil
}

func canonicalJSON(payload []byte) ([]byte, error) {
	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		return nil, err
	}
	if value == nil {
		return nil, ErrInvalidRequest
	}
	return json.Marshal(value)
}
