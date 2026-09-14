// Package shift contains shift and draft use cases.
package shift

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pomkita/pomkita-be/internal/domain"
)

// Status is the server-owned lifecycle state of a shift.
type Status string

const (
	// StatusOpen identifies a shift that accepts draft input.
	StatusOpen Status = "open"
)

var (
	// ErrInvalidRequest identifies an incomplete shift request.
	ErrInvalidRequest = domain.NewError(domain.CategoryValidation, "invalid_shift_request")
	// ErrDependencyUnavailable identifies a missing shift dependency.
	ErrDependencyUnavailable = domain.NewError(domain.CategoryDependency, "dependency_unavailable")
	// ErrUnauthorized identifies an actor without operational shift authority.
	ErrUnauthorized = domain.NewError(domain.CategoryAuthorization, "shift_open_forbidden")
	// ErrBackfillApprovalRequired identifies incomplete or unexpected backfill data.
	ErrBackfillApprovalRequired = domain.NewError(domain.CategoryValidation, "backfill_approval_required")
	// ErrBackfillOutOfOrder identifies a backfill that would invalidate a later meter chain.
	ErrBackfillOutOfOrder = domain.NewError(domain.CategoryConflict, "backfill_out_of_order")
)

// Clock provides the current time to the service.
type Clock interface {
	Now() time.Time
}

// OpenRequest contains actor and station scope for a new shift.
type OpenRequest struct {
	OrgID             uuid.UUID
	StationID         uuid.UUID
	ActorID           uuid.UUID
	Role              string
	OpenedAt          time.Time
	Backfilled        bool
	OriginalEventDate string
	ShiftKE           int
	BackfillApprover  uuid.UUID
	BackfillReason    string
}

// Shift is the immutable result of opening a shift.
type Shift struct {
	ShiftID          uuid.UUID `json:"shift_id"`
	OrgID            uuid.UUID `json:"org_id"`
	StationID        uuid.UUID `json:"station_id"`
	StationSeq       int64     `json:"station_seq"`
	SupervisorID     uuid.UUID `json:"supervisor_id"`
	OpenedAt         time.Time `json:"opened_at"`
	TimezoneSnapshot string    `json:"timezone_snapshot"`
	BusinessDate     string    `json:"business_date"`
	Status           Status    `json:"status"`
	PriceMapSnapshot []byte    `json:"price_map_snapshot,omitempty"`
	PriceMapHash     []byte    `json:"price_map_hash,omitempty"`
}

// Repository persists one shift opening as one transaction.
type Repository interface {
	OpenShift(context.Context, OpenRequest) (Shift, error)
}

// Service owns shift opening use cases.
type Service struct {
	repository Repository
	clock      Clock
}

// NewService creates a shift service.
func NewService(repository Repository, clock Clock) *Service {
	return &Service{repository: repository, clock: clock}
}

// OpenShift creates one open shift and lets the repository allocate its station sequence.
func (s *Service) OpenShift(ctx context.Context, request OpenRequest) (Shift, error) {
	if s == nil || s.repository == nil || s.clock == nil {
		return Shift{}, ErrDependencyUnavailable
	}
	if request.OrgID == uuid.Nil || request.StationID == uuid.Nil || request.ActorID == uuid.Nil {
		return Shift{}, ErrInvalidRequest
	}
	if request.Role != "Supervisor" {
		return Shift{}, ErrUnauthorized
	}
	if request.Backfilled {
		if request.OriginalEventDate == "" || request.ShiftKE < 1 || request.BackfillApprover == uuid.Nil || strings.TrimSpace(request.BackfillReason) == "" {
			return Shift{}, ErrBackfillApprovalRequired
		}
	} else if request.OriginalEventDate != "" || request.ShiftKE != 0 || request.BackfillApprover != uuid.Nil || strings.TrimSpace(request.BackfillReason) != "" {
		return Shift{}, ErrBackfillApprovalRequired
	}
	if request.OpenedAt.IsZero() {
		request.OpenedAt = s.clock.Now().UTC()
	}
	if request.OpenedAt.Location() == nil {
		return Shift{}, ErrInvalidRequest
	}
	result, err := s.repository.OpenShift(ctx, request)
	if err != nil {
		return Shift{}, err
	}
	if result.Status != StatusOpen || result.ShiftID == uuid.Nil || result.StationSeq < 1 {
		return Shift{}, errors.New("shift repository returned an invalid open shift")
	}
	return result, nil
}
