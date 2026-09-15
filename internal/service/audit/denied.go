package audit

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
)

// DeniedRequest contains non-secret metadata for one denied request.
type DeniedRequest struct {
	RequestID   uuid.UUID
	SubjectID   *uuid.UUID
	JTI         *uuid.UUID
	OrgID       *uuid.UUID
	StationID   *uuid.UUID
	Action      string
	Target      string
	Reason      string
	Outcome     string
	ErrorDetail string
}

// DeniedRepository persists denied request metadata outside business transactions.
type DeniedRepository interface {
	RecordDenied(context.Context, DeniedRequest, time.Time) error
}

// DeniedService owns denied request validation and persistence.
type DeniedService struct {
	repository DeniedRepository
	clock      Clock
}

// NewDeniedService creates a denied request audit service.
func NewDeniedService(repository DeniedRepository, clock Clock) *DeniedService {
	return &DeniedService{repository: repository, clock: clock}
}

// Record stores safe metadata for one denied request.
func (s *DeniedService) Record(ctx context.Context, request DeniedRequest) error {
	if s == nil || s.repository == nil || s.clock == nil {
		return ErrDependencyUnavailable
	}
	if request.RequestID == uuid.Nil || strings.TrimSpace(request.Action) == "" || strings.TrimSpace(request.Target) == "" || strings.TrimSpace(request.Reason) == "" || strings.TrimSpace(request.Outcome) == "" {
		return ErrInvalidDeniedRequest
	}
	return s.repository.RecordDenied(ctx, request, s.clock.Now().UTC())
}

// RecordDenied implements the HTTP denied-audit port.
func (s *DeniedService) RecordDenied(ctx context.Context, request DeniedRequest) error {
	return s.Record(ctx, request)
}
