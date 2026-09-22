package governance

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/fadhln/pomkita-be/internal/domain"
	"github.com/google/uuid"
)

var (
	// ErrInvalidAmendmentRequest identifies an incomplete amendment request.
	ErrInvalidAmendmentRequest = domain.NewError(domain.CategoryValidation, "invalid_amendment_request")
	// ErrAmendmentRoleRequired identifies an actor without amendment authority.
	ErrAmendmentRoleRequired = domain.NewError(domain.CategoryAuthorization, "amendment_role_required")
	// ErrAmendmentFieldForbidden identifies a field outside the amendment allowlist.
	ErrAmendmentFieldForbidden = domain.NewError(domain.CategoryValidation, "amendment_field_forbidden")
	// ErrAmendmentSeparationRequired identifies a requester approving their own amendment.
	ErrAmendmentSeparationRequired = domain.NewError(domain.CategoryAuthorization, "amendment_separation_required")
	// ErrAmendmentStale identifies a stale report or field value.
	ErrAmendmentStale = domain.NewError(domain.CategoryConflict, "stale_amendment_base")
	// ErrAmendmentBaseUnavailable identifies a report that cannot be amended.
	ErrAmendmentBaseUnavailable = domain.NewError(domain.CategoryConflict, "amendment_base_unavailable")
	// ErrAmendmentPendingExists identifies a base report with another pending request.
	ErrAmendmentPendingExists = domain.NewError(domain.CategoryConflict, "amendment_pending_exists")
	// ErrAmendmentNotFound identifies an amendment outside the actor scope.
	ErrAmendmentNotFound = domain.NewError(domain.CategoryNotFound, "amendment_not_found")
	// ErrAmendmentNotPending identifies an amendment that is already decided.
	ErrAmendmentNotPending = domain.NewError(domain.CategoryConflict, "amendment_not_pending")
	// ErrAmendmentTargetNotFound identifies a requested row outside the base report.
	ErrAmendmentTargetNotFound = domain.NewError(domain.CategoryValidation, "amendment_target_not_found")
	// ErrAmendmentReasonRequired identifies a missing rejection or break-glass reason.
	ErrAmendmentReasonRequired = domain.NewError(domain.CategoryValidation, "amendment_reason_required")
)

// AmendmentItem contains one requested report field change.
type AmendmentItem struct {
	TargetKind      string
	TargetLogicalID uuid.UUID
	Field           string
	OldValue        []byte
	NewValue        []byte
}

// AmendmentRequest contains one amendment request.
type AmendmentRequest struct {
	OrgID            uuid.UUID
	StationID        uuid.UUID
	ShiftID          uuid.UUID
	BaseReportID     uuid.UUID
	RequesterID      uuid.UUID
	Role             string
	Reason           string
	StaleCheckHash   []byte
	IsBreakGlass     bool
	BreakGlassReason string
	Items            []AmendmentItem
}

// ApproveAmendmentRequest contains an approval decision.
type ApproveAmendmentRequest struct {
	OrgID          uuid.UUID
	StationID      uuid.UUID
	AmendmentID    uuid.UUID
	ApproverID     uuid.UUID
	Role           string
	StaleCheckHash []byte
}

// RejectAmendmentRequest contains a rejection decision.
type RejectAmendmentRequest struct {
	OrgID           uuid.UUID
	StationID       uuid.UUID
	AmendmentID     uuid.UUID
	ApproverID      uuid.UUID
	Role            string
	RejectionReason string
}

// Amendment identifies an amendment state change.
type Amendment struct {
	AmendmentID     uuid.UUID
	BaseReportID    uuid.UUID
	AppliedReportID uuid.UUID
	Status          string
	VersionNo       int
	StaleCheckHash  []byte
	RequestedAt     time.Time
	DecidedAt       time.Time
	RejectionReason string
}

// AmendmentQueueRequest contains the verified actor scope for a queue read.
type AmendmentQueueRequest struct {
	OrgID      uuid.UUID
	ActorID    uuid.UUID
	Role       string
	StationIDs []uuid.UUID
}

// AmendmentQueueRequester identifies the user who requested an amendment.
type AmendmentQueueRequester struct {
	UserID      uuid.UUID
	DisplayName string
}

// AmendmentQueueItem contains one requested field diff.
type AmendmentQueueItem struct {
	ItemID          uuid.UUID
	TargetKind      string
	TargetLogicalID uuid.UUID
	Field           string
	OldValue        json.RawMessage
	NewValue        json.RawMessage
}

// AmendmentQueueView is one pending amendment visible to an approver.
type AmendmentQueueView struct {
	AmendmentID    uuid.UUID
	StationID      uuid.UUID
	ShiftID        uuid.UUID
	BaseReportID   uuid.UUID
	BaseVersionNo  int
	Status         string
	Requester      AmendmentQueueRequester
	Reason         string
	RequestedAt    string
	StaleCheckHash string
	Items          []AmendmentQueueItem
}

// AmendmentRepository persists amendment workflows.
type AmendmentRepository interface {
	RequestAmendment(context.Context, AmendmentRequest, time.Time) (Amendment, error)
	ApproveAmendment(context.Context, ApproveAmendmentRequest, time.Time) (Amendment, error)
	RejectAmendment(context.Context, RejectAmendmentRequest, time.Time) error
}

// AmendmentQueueRepository reads pending amendments in an actor scope.
type AmendmentQueueRepository interface {
	ListAmendmentQueue(context.Context, AmendmentQueueRequest) ([]AmendmentQueueView, error)
}

// AmendmentService owns amendment validation and use cases.
type AmendmentService struct {
	repository AmendmentRepository
	clock      Clock
}

// NewAmendmentService creates an amendment service.
func NewAmendmentService(repository AmendmentRepository, clock Clock) *AmendmentService {
	return &AmendmentService{repository: repository, clock: clock}
}

// Request validates and stores a pending amendment.
func (s *AmendmentService) Request(ctx context.Context, request AmendmentRequest) (Amendment, error) {
	if s == nil || s.repository == nil || s.clock == nil {
		return Amendment{}, ErrDependencyUnavailable
	}
	if request.OrgID == uuid.Nil || request.StationID == uuid.Nil || request.ShiftID == uuid.Nil || request.BaseReportID == uuid.Nil || request.RequesterID == uuid.Nil || request.Role != "Supervisor" || strings.TrimSpace(request.Reason) == "" || len(request.StaleCheckHash) != 32 || len(request.Items) == 0 {
		if request.Role != "Supervisor" {
			return Amendment{}, ErrAmendmentRoleRequired
		}
		return Amendment{}, ErrInvalidAmendmentRequest
	}
	if request.IsBreakGlass {
		if strings.TrimSpace(request.BreakGlassReason) == "" {
			return Amendment{}, ErrAmendmentReasonRequired
		}
	} else if strings.TrimSpace(request.BreakGlassReason) != "" {
		return Amendment{}, ErrInvalidAmendmentRequest
	}
	for _, item := range request.Items {
		if !amendmentFieldAllowed(item.TargetKind, item.Field) || item.TargetLogicalID == uuid.Nil || len(item.OldValue) == 0 {
			return Amendment{}, ErrAmendmentFieldForbidden
		}
	}
	return s.repository.RequestAmendment(ctx, request, s.clock.Now().UTC())
}

// Approve validates and applies one pending amendment.
func (s *AmendmentService) Approve(ctx context.Context, request ApproveAmendmentRequest) (Amendment, error) {
	if s == nil || s.repository == nil || s.clock == nil {
		return Amendment{}, ErrDependencyUnavailable
	}
	if request.OrgID == uuid.Nil || request.StationID == uuid.Nil || request.AmendmentID == uuid.Nil || request.ApproverID == uuid.Nil || len(request.StaleCheckHash) != 32 {
		return Amendment{}, ErrInvalidAmendmentRequest
	}
	if request.Role != "Station Admin" && request.Role != "Owner" && request.Role != "Superadmin" {
		return Amendment{}, ErrAmendmentRoleRequired
	}
	return s.repository.ApproveAmendment(ctx, request, s.clock.Now().UTC())
}

// Reject records a rejection for one pending amendment.
func (s *AmendmentService) Reject(ctx context.Context, request RejectAmendmentRequest) error {
	if s == nil || s.repository == nil || s.clock == nil {
		return ErrDependencyUnavailable
	}
	if request.OrgID == uuid.Nil || request.StationID == uuid.Nil || request.AmendmentID == uuid.Nil || request.ApproverID == uuid.Nil || strings.TrimSpace(request.RejectionReason) == "" {
		return ErrInvalidAmendmentRequest
	}
	if request.Role != "Station Admin" && request.Role != "Owner" && request.Role != "Superadmin" {
		return ErrAmendmentRoleRequired
	}
	return s.repository.RejectAmendment(ctx, request, s.clock.Now().UTC())
}

// ListQueue returns pending amendments visible to the verified actor.
func (s *AmendmentService) ListQueue(ctx context.Context, request AmendmentQueueRequest) ([]AmendmentQueueView, error) {
	if s == nil || s.repository == nil {
		return nil, ErrDependencyUnavailable
	}
	if request.OrgID == uuid.Nil || request.ActorID == uuid.Nil {
		return nil, ErrInvalidAmendmentRequest
	}
	switch request.Role {
	case "Station Admin":
		if len(request.StationIDs) == 0 {
			return nil, ErrInvalidAmendmentRequest
		}
		for _, stationID := range request.StationIDs {
			if stationID == uuid.Nil {
				return nil, ErrInvalidAmendmentRequest
			}
		}
	case "Owner", "Superadmin":
	default:
		return nil, ErrAmendmentRoleRequired
	}
	repository, ok := s.repository.(AmendmentQueueRepository)
	if !ok {
		return nil, ErrDependencyUnavailable
	}
	return repository.ListAmendmentQueue(ctx, request)
}

func amendmentFieldAllowed(targetKind, field string) bool {
	switch targetKind {
	case "sales_declared":
		return field == "cash_amount" || field == "cashless_amount"
	case "loss_entry":
		return field == "liters" || field == "cash_amount" || field == "note"
	case "delivery", "dip_reading":
		return field == "reference" || field == "delivery_id" || field == "dip_id"
	default:
		return false
	}
}
