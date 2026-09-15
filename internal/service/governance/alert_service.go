package governance

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/pomkita/pomkita-be/internal/domain"
)

var (
	// ErrAlertInvalidRequest identifies an incomplete alert occurrence.
	ErrAlertInvalidRequest = domain.NewError(domain.CategoryValidation, "invalid_alert_request")
	// ErrAlertSourceVersionRequired identifies a report or amendment without a version.
	ErrAlertSourceVersionRequired = domain.NewError(domain.CategoryValidation, "alert_source_version_required")
	// ErrAlertRelatedRequired identifies a clear event without its fired event.
	ErrAlertRelatedRequired = domain.NewError(domain.CategoryValidation, "alert_related_event_required")
)

// AlertOccurrenceRequest contains one alert fire or clear operation.
type AlertOccurrenceRequest struct {
	OrgID               uuid.UUID
	StationID           uuid.UUID
	RuleID              uuid.UUID
	SubjectKind         string
	SubjectID           uuid.UUID
	EventType           string
	PeriodStart         time.Time
	RelatedFiredEventID *uuid.UUID
	SourceKind          string
	SourceID            uuid.UUID
	SourceVersionNo     *int
	SourceAt            time.Time
	CreatedBy           *uuid.UUID
}

// AlertEvent identifies the persisted alert event.
type AlertEvent struct {
	EventID        uuid.UUID
	EventType      string
	Inserted       bool
	RelatedFiredID *uuid.UUID
}

// AlertRepository persists idempotent alert occurrences.
type AlertRepository interface {
	RecordAlertOccurrence(context.Context, AlertOccurrenceRequest, time.Time) (AlertEvent, error)
}

// AlertService owns alert occurrence validation.
type AlertService struct {
	repository AlertRepository
	clock      Clock
}

// NewAlertService creates an alert service.
func NewAlertService(repository AlertRepository, clock Clock) *AlertService {
	return &AlertService{repository: repository, clock: clock}
}

// Record validates and persists one idempotent alert occurrence.
func (s *AlertService) Record(ctx context.Context, request AlertOccurrenceRequest) (AlertEvent, error) {
	if s == nil || s.repository == nil || s.clock == nil {
		return AlertEvent{}, ErrDependencyUnavailable
	}
	if request.OrgID == uuid.Nil || request.StationID == uuid.Nil || request.RuleID == uuid.Nil || request.SubjectID == uuid.Nil || request.SourceID == uuid.Nil || request.PeriodStart.IsZero() || request.SourceAt.IsZero() {
		return AlertEvent{}, ErrAlertInvalidRequest
	}
	if request.SubjectKind != "shift" && request.SubjectKind != "report" {
		return AlertEvent{}, ErrAlertInvalidRequest
	}
	if request.EventType != "fired" && request.EventType != "cleared" {
		return AlertEvent{}, ErrAlertInvalidRequest
	}
	if request.SourceKind != "shift_transition" && request.SourceKind != "report" && request.SourceKind != "scheduler" && request.SourceKind != "amendment" {
		return AlertEvent{}, ErrAlertInvalidRequest
	}
	versionRequired := request.SourceKind == "report" || request.SourceKind == "amendment"
	if versionRequired && (request.SourceVersionNo == nil || *request.SourceVersionNo < 1) {
		return AlertEvent{}, ErrAlertSourceVersionRequired
	}
	if !versionRequired && request.SourceVersionNo != nil {
		return AlertEvent{}, ErrAlertInvalidRequest
	}
	if request.EventType == "fired" && request.RelatedFiredEventID != nil {
		return AlertEvent{}, ErrAlertInvalidRequest
	}
	if request.EventType == "cleared" && request.RelatedFiredEventID == nil {
		return AlertEvent{}, ErrAlertRelatedRequired
	}
	return s.repository.RecordAlertOccurrence(ctx, request, s.clock.Now().UTC())
}
