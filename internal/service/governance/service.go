// Package governance contains report acknowledgement and approval use cases.
package governance

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pomkita/pomkita-be/internal/domain"
)

var (
	// ErrInvalidAckRequest identifies an incomplete acknowledgement request.
	ErrInvalidAckRequest = domain.NewError(domain.CategoryValidation, "invalid_ack_request")
	// ErrAckRoleRequired identifies an actor without acknowledgement authority.
	ErrAckRoleRequired = domain.NewError(domain.CategoryAuthorization, "ack_role_required")
	// ErrAckSeparationRequired identifies an actor who created the report data.
	ErrAckSeparationRequired = domain.NewError(domain.CategoryAuthorization, "ack_separation_required")
	// ErrAckAlreadyDecided identifies a report version with an active decision.
	ErrAckAlreadyDecided = domain.NewError(domain.CategoryConflict, "ack_already_decided")
	// ErrAckReportNotFound identifies a report outside the actor scope.
	ErrAckReportNotFound = domain.NewError(domain.CategoryNotFound, "ack_report_not_found")
	// ErrAckReportUnavailable identifies a report that is not awaiting acknowledgement.
	ErrAckReportUnavailable = domain.NewError(domain.CategoryConflict, "ack_report_unavailable")
	// ErrBreakGlassReasonRequired identifies a missing break-glass reason.
	ErrBreakGlassReasonRequired = domain.NewError(domain.CategoryValidation, "break_glass_reason_required")
	// ErrUnexpectedBreakGlassReason identifies a reason on a normal acknowledgement.
	ErrUnexpectedBreakGlassReason = domain.NewError(domain.CategoryValidation, "unexpected_break_glass_reason")
	// ErrRejectionReasonRequired identifies a rejected acknowledgement without a reason.
	ErrRejectionReasonRequired = domain.NewError(domain.CategoryValidation, "rejection_reason_required")
	// ErrUnexpectedRejectionReason identifies a reason on an accepted acknowledgement.
	ErrUnexpectedRejectionReason = domain.NewError(domain.CategoryValidation, "unexpected_rejection_reason")
	// ErrDependencyUnavailable identifies a missing governance dependency.
	ErrDependencyUnavailable = domain.NewError(domain.CategoryDependency, "dependency_unavailable")
)

// Clock provides the current time to governance operations.
type Clock interface {
	Now() time.Time
}

// AcknowledgeRequest contains the report decision and actor scope.
type AcknowledgeRequest struct {
	OrgID            uuid.UUID
	StationID        uuid.UUID
	ShiftID          uuid.UUID
	ReportID         uuid.UUID
	ActorID          uuid.UUID
	VersionNo        int
	Role             string
	Decision         string
	RejectionReason  string
	IsBreakGlass     bool
	BreakGlassReason string
}

// Acknowledgement identifies the decision created by the repository.
type Acknowledgement struct {
	AckID        uuid.UUID
	ReportID     uuid.UUID
	VersionNo    int
	Decision     string
	ShiftStatus  string
	IsBreakGlass bool
}

// Repository persists one acknowledgement transaction.
type Repository interface {
	Acknowledge(context.Context, AcknowledgeRequest, time.Time) (Acknowledgement, error)
}

// Service owns acknowledgement validation and use cases.
type Service struct {
	repository Repository
	clock      Clock
}

// NewService creates a governance service.
func NewService(repository Repository, clock Clock) *Service {
	return &Service{repository: repository, clock: clock}
}

// Acknowledge validates and records one report decision.
func (s *Service) Acknowledge(ctx context.Context, request AcknowledgeRequest) (Acknowledgement, error) {
	if s == nil || s.repository == nil || s.clock == nil {
		return Acknowledgement{}, ErrDependencyUnavailable
	}
	if request.OrgID == uuid.Nil || request.StationID == uuid.Nil || request.ShiftID == uuid.Nil || request.ReportID == uuid.Nil || request.ActorID == uuid.Nil || request.VersionNo < 1 {
		return Acknowledgement{}, ErrInvalidAckRequest
	}
	if request.Role != "Station Admin" && request.Role != "Owner" && request.Role != "Superadmin" {
		return Acknowledgement{}, ErrAckRoleRequired
	}
	if request.Decision != "acked" && request.Decision != "rejected" {
		return Acknowledgement{}, ErrInvalidAckRequest
	}
	if request.Decision == "rejected" && strings.TrimSpace(request.RejectionReason) == "" {
		return Acknowledgement{}, ErrRejectionReasonRequired
	}
	if request.Decision == "acked" && strings.TrimSpace(request.RejectionReason) != "" {
		return Acknowledgement{}, ErrUnexpectedRejectionReason
	}
	if request.IsBreakGlass {
		if request.Role != "Owner" && request.Role != "Superadmin" {
			return Acknowledgement{}, ErrAckRoleRequired
		}
		if strings.TrimSpace(request.BreakGlassReason) == "" {
			return Acknowledgement{}, ErrBreakGlassReasonRequired
		}
	} else if strings.TrimSpace(request.BreakGlassReason) != "" {
		return Acknowledgement{}, ErrUnexpectedBreakGlassReason
	}
	return s.repository.Acknowledge(ctx, request, s.clock.Now().UTC())
}
