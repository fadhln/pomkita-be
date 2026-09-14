// Package audit contains audit chain use cases.
package audit

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

// AppendRequest identifies one business event for the audit chain.
type AppendRequest struct {
	OrgID        uuid.UUID
	EventID      uuid.UUID
	EventType    string
	Payload      json.RawMessage
	Outcome      string
	OutcomeError string
}

// Event identifies one appended audit row.
type Event struct {
	EventID     uuid.UUID
	OrgSequence int64
	RowHash     []byte
}

// Repository appends and verifies audit chain rows.
type Repository interface {
	Append(context.Context, AppendRequest, time.Time) (Event, error)
	Verify(context.Context, uuid.UUID) error
}

// Clock provides the current time to audit operations.
type Clock interface{ Now() time.Time }

// Service owns audit use cases.
type Service struct {
	repository Repository
	clock      Clock
}

// NewService creates an audit service.
func NewService(repository Repository, clock Clock) *Service {
	return &Service{repository: repository, clock: clock}
}

// Append adds one event and its relay payload atomically.
func (s *Service) Append(ctx context.Context, request AppendRequest) (Event, error) {
	if s == nil || s.repository == nil || s.clock == nil {
		return Event{}, ErrDependencyUnavailable
	}
	if request.OrgID == uuid.Nil || request.EventID == uuid.Nil || strings.TrimSpace(request.EventType) == "" || len(request.Payload) == 0 || !json.Valid(request.Payload) || strings.TrimSpace(request.Outcome) == "" {
		return Event{}, ErrInvalidRequest
	}
	return s.repository.Append(ctx, request, s.clock.Now().UTC())
}

// Verify checks the complete organization audit chain.
func (s *Service) Verify(ctx context.Context, orgID uuid.UUID) error {
	if s == nil || s.repository == nil {
		return ErrDependencyUnavailable
	}
	if orgID == uuid.Nil {
		return ErrInvalidRequest
	}
	return s.repository.Verify(ctx, orgID)
}
