// Package draft contains draft lease and revision-fenced input use cases.
package draft

import (
	"context"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/pomkita/pomkita-be/internal/domain"
)

var decimalPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?$`)

var (
	// ErrInvalidRequest identifies an invalid draft request.
	ErrInvalidRequest = domain.NewError(domain.CategoryValidation, "invalid_draft_request")
	// ErrClaimExpired identifies a write made after the draft lease expired.
	ErrClaimExpired = domain.NewError(domain.CategoryConflict, "draft_claim_expired")
	// ErrDraftNotFound identifies a draft outside the actor scope.
	ErrDraftNotFound = domain.NewError(domain.CategoryNotFound, "draft_not_found")
	// ErrRevisionConflict identifies a stale draft revision.
	ErrRevisionConflict = domain.NewError(domain.CategoryConflict, "draft_revision_conflict")
	// ErrDependencyUnavailable identifies a missing draft dependency.
	ErrDependencyUnavailable = domain.NewError(domain.CategoryDependency, "dependency_unavailable")
)

// Clock provides the current time to draft operations.
type Clock interface {
	Now() time.Time
}

// ClaimRequest identifies the draft lease owner.
type ClaimRequest struct {
	DraftID uuid.UUID
	ShiftID uuid.UUID
	ActorID uuid.UUID
}

// ClaimResult contains the new lease and revision.
type ClaimResult struct {
	DraftID        uuid.UUID
	ClaimToken     uuid.UUID
	ClaimExpiresAt time.Time
	Revision       int
}

// HeartbeatRequest identifies an existing draft lease.
type HeartbeatRequest struct {
	DraftID    uuid.UUID
	ClaimToken uuid.UUID
	ActorID    uuid.UUID
}

// WriteReadingRequest contains one exact decimal meter reading.
type WriteReadingRequest struct {
	DraftID    uuid.UUID
	ClaimToken uuid.UUID
	Revision   int
	NozzleID   uuid.UUID
	MeterStart string
	MeterEnd   string
	ActorID    uuid.UUID
}

// WriteSalesRequest contains one exact decimal dispenser sales row.
type WriteSalesRequest struct {
	DraftID        uuid.UUID
	ClaimToken     uuid.UUID
	Revision       int
	DispenserID    uuid.UUID
	CashAmount     string
	CashlessAmount string
	ActorID        uuid.UUID
}

// WriteLossRequest contains one exact decimal loss or gain row.
type WriteLossRequest struct {
	DraftID    uuid.UUID
	ClaimToken uuid.UUID
	Revision   int
	LossID     uuid.UUID
	Direction  string
	ReasonCode string
	Liters     string
	CashAmount string
	Note       string
	ActorID    uuid.UUID
}

// StageEvidenceRequest contains one staged evidence object reference.
type StageEvidenceRequest struct {
	DraftID      uuid.UUID
	ClaimToken   uuid.UUID
	Revision     int
	LossRowID    uuid.UUID
	EvidenceType string
	ObjectKey    string
	ContentHash  []byte
	SizeBytes    int64
	MIME         string
	ActorID      uuid.UUID
}

// Repository persists lease and revision-fenced draft operations.
type Repository interface {
	Claim(context.Context, ClaimRequest, time.Time) (ClaimResult, error)
	Heartbeat(context.Context, HeartbeatRequest, time.Time) error
	WriteReading(context.Context, WriteReadingRequest, time.Time) (int, error)
}

// SalesRepository persists revision-fenced sales rows.
type SalesRepository interface {
	WriteSales(context.Context, WriteSalesRequest, time.Time) (int, error)
}

// LossRepository persists revision-fenced loss rows.
type LossRepository interface {
	WriteLoss(context.Context, WriteLossRequest, time.Time) (int, error)
}

// EvidenceRepository persists revision-fenced staged evidence.
type EvidenceRepository interface {
	StageEvidence(context.Context, StageEvidenceRequest, time.Time) (int, error)
}

// Service owns draft operations.
type Service struct {
	repository Repository
	clock      Clock
}

// NewService creates a draft service.
func NewService(repository Repository, clock Clock) *Service {
	return &Service{repository: repository, clock: clock}
}

// Claim claims a draft for one actor.
func (s *Service) Claim(ctx context.Context, request ClaimRequest) (ClaimResult, error) {
	if s == nil || s.repository == nil || s.clock == nil {
		return ClaimResult{}, ErrDependencyUnavailable
	}
	if request.DraftID == uuid.Nil || request.ShiftID == uuid.Nil || request.ActorID == uuid.Nil {
		return ClaimResult{}, ErrInvalidRequest
	}
	return s.repository.Claim(ctx, request, s.clock.Now().UTC())
}

// Heartbeat extends an active draft lease.
func (s *Service) Heartbeat(ctx context.Context, request HeartbeatRequest) error {
	if s == nil || s.repository == nil || s.clock == nil {
		return ErrDependencyUnavailable
	}
	if request.DraftID == uuid.Nil || request.ClaimToken == uuid.Nil || request.ActorID == uuid.Nil {
		return ErrInvalidRequest
	}
	return s.repository.Heartbeat(ctx, request, s.clock.Now().UTC())
}

// WriteReading writes one reading only when the lease and revision are current.
func (s *Service) WriteReading(ctx context.Context, request WriteReadingRequest) (int, error) {
	if s == nil || s.repository == nil || s.clock == nil {
		return 0, ErrDependencyUnavailable
	}
	if request.DraftID == uuid.Nil || request.ClaimToken == uuid.Nil || request.NozzleID == uuid.Nil || request.Revision < 1 || request.ActorID == uuid.Nil || !validDecimal(request.MeterStart) || !validDecimal(request.MeterEnd) {
		return 0, ErrInvalidRequest
	}
	return s.repository.WriteReading(ctx, request, s.clock.Now().UTC())
}

// WriteSales writes one sales row only when the lease and revision are current.
func (s *Service) WriteSales(ctx context.Context, request WriteSalesRequest) (int, error) {
	if s == nil || s.repository == nil || s.clock == nil {
		return 0, ErrDependencyUnavailable
	}
	if request.DraftID == uuid.Nil || request.ClaimToken == uuid.Nil || request.DispenserID == uuid.Nil || request.Revision < 1 || request.ActorID == uuid.Nil || !validDecimal(request.CashAmount) || !validDecimal(request.CashlessAmount) {
		return 0, ErrInvalidRequest
	}
	repository, ok := s.repository.(SalesRepository)
	if !ok {
		return 0, ErrDependencyUnavailable
	}
	return repository.WriteSales(ctx, request, s.clock.Now().UTC())
}

// WriteLoss writes one loss or gain row only when the lease and revision are current.
func (s *Service) WriteLoss(ctx context.Context, request WriteLossRequest) (int, error) {
	if s == nil || s.repository == nil || s.clock == nil {
		return 0, ErrDependencyUnavailable
	}
	if request.DraftID == uuid.Nil || request.ClaimToken == uuid.Nil || request.LossID == uuid.Nil || request.Revision < 1 || request.ActorID == uuid.Nil || (request.Direction != "loss" && request.Direction != "gain") || request.ReasonCode == "" || !validDecimal(request.Liters) || (request.CashAmount != "" && !validDecimal(request.CashAmount)) {
		return 0, ErrInvalidRequest
	}
	repository, ok := s.repository.(LossRepository)
	if !ok {
		return 0, ErrDependencyUnavailable
	}
	return repository.WriteLoss(ctx, request, s.clock.Now().UTC())
}

// StageEvidence stages one evidence object only when the lease and revision are current.
func (s *Service) StageEvidence(ctx context.Context, request StageEvidenceRequest) (int, error) {
	if s == nil || s.repository == nil || s.clock == nil {
		return 0, ErrDependencyUnavailable
	}
	if request.DraftID == uuid.Nil || request.ClaimToken == uuid.Nil || request.LossRowID == uuid.Nil || request.Revision < 1 || request.ActorID == uuid.Nil || request.EvidenceType == "" || request.ObjectKey == "" || len(request.ContentHash) != 32 || request.SizeBytes <= 0 || request.MIME == "" {
		return 0, ErrInvalidRequest
	}
	repository, ok := s.repository.(EvidenceRepository)
	if !ok {
		return 0, ErrDependencyUnavailable
	}
	return repository.StageEvidence(ctx, request, s.clock.Now().UTC())
}

func validDecimal(value string) bool {
	return decimalPattern.MatchString(value)
}
