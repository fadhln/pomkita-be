// Package policy contains policy revision use cases.
package policy

import (
	"context"
	"math/big"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pomkita/pomkita-be/internal/domain"
)

var (
	// ErrInvalidRequest identifies an invalid policy revision request.
	ErrInvalidRequest = domain.NewError(domain.CategoryValidation, "invalid_policy_revision_request")
	// ErrPolicyRoleRequired identifies an actor without policy authority.
	ErrPolicyRoleRequired = domain.NewError(domain.CategoryAuthorization, "policy_role_required")
	// ErrTombstoneReasonRequired identifies a disabled revision without a reason.
	ErrTombstoneReasonRequired = domain.NewError(domain.CategoryValidation, "tombstone_reason_required")
	// ErrPolicyOverlap identifies a revision that overlaps an existing scope.
	ErrPolicyOverlap = domain.NewError(domain.CategoryConflict, "policy_revision_overlap")
)

// Clock provides the current time to policy operations.
type Clock interface{ Now() time.Time }

// PolicyRevisionRequest contains one append-only policy revision.
type PolicyRevisionRequest struct {
	OrgID                   uuid.UUID
	StationID               uuid.UUID
	ActorID                 uuid.UUID
	Role                    string
	PolicyKind              string
	PolicyID                uuid.UUID
	SupersedesRevisionID    *uuid.UUID
	ValidFrom               time.Time
	Disabled                bool
	TombstoneReason         string
	LossLiterThreshold      string
	GainLiterThreshold      string
	LossRupiahThreshold     string
	GainRupiahThreshold     string
	VarianceRupiahThreshold string
	RolloverThreshold       string
	EvidenceMode            string
}

// PolicyRevision identifies a persisted revision.
type PolicyRevision struct {
	RevisionID uuid.UUID `json:"revision_id"`
	PolicyID   uuid.UUID `json:"policy_id"`
	PolicyKind string    `json:"policy_kind"`
	ValidFrom  time.Time `json:"valid_from"`
	Disabled   bool      `json:"disabled"`
}

// RevisionView is one immutable policy history row with decimal values as strings.
type RevisionView struct {
	RevisionID          uuid.UUID  `json:"revision_id"`
	PolicyID            uuid.UUID  `json:"policy_id"`
	PolicyKind          string     `json:"policy_kind"`
	StationID           *uuid.UUID `json:"station_id,omitempty"`
	ValidFrom           string     `json:"valid_from"`
	Disabled            bool       `json:"disabled"`
	TombstoneReason     *string    `json:"tombstone_reason,omitempty"`
	LossLiterThreshold  *string    `json:"loss_liter_threshold,omitempty"`
	GainLiterThreshold  *string    `json:"gain_liter_threshold,omitempty"`
	LossRupiahThreshold *string    `json:"loss_rupiah_threshold,omitempty"`
	GainRupiahThreshold *string    `json:"gain_rupiah_threshold,omitempty"`
	VarianceThreshold   *string    `json:"variance_rupiah_threshold,omitempty"`
	RolloverThreshold   *string    `json:"rollover_threshold,omitempty"`
	EvidenceMode        *string    `json:"evidence_mode,omitempty"`
}

// Repository persists append-only policy revisions.
type Repository interface {
	CreateRevision(context.Context, PolicyRevisionRequest, time.Time) (PolicyRevision, error)
}

// ReadRepository reads policy history within a tenant scope.
type ReadRepository interface {
	History(context.Context, uuid.UUID, *uuid.UUID) ([]RevisionView, error)
}

// Service owns policy validation.
type Service struct {
	repository Repository
	clock      Clock
}

// NewService creates a policy service.
func NewService(repository Repository, clock Clock) *Service {
	return &Service{repository: repository, clock: clock}
}

// CreateRevision validates and appends one policy revision.
func (s *Service) CreateRevision(ctx context.Context, request PolicyRevisionRequest) (PolicyRevision, error) {
	if s == nil || s.repository == nil || s.clock == nil {
		return PolicyRevision{}, ErrInvalidRequest
	}
	if request.OrgID == uuid.Nil || request.PolicyID == uuid.Nil || request.ValidFrom.IsZero() || request.PolicyKind == "" {
		return PolicyRevision{}, ErrInvalidRequest
	}
	if request.Role != "Owner" && request.Role != "Superadmin" {
		return PolicyRevision{}, ErrPolicyRoleRequired
	}
	if request.Disabled && strings.TrimSpace(request.TombstoneReason) == "" {
		return PolicyRevision{}, ErrTombstoneReasonRequired
	}
	if !request.Disabled && strings.TrimSpace(request.TombstoneReason) != "" {
		return PolicyRevision{}, ErrInvalidRequest
	}
	switch request.PolicyKind {
	case "threshold":
		for _, value := range []string{request.LossLiterThreshold, request.GainLiterThreshold, request.LossRupiahThreshold, request.GainRupiahThreshold, request.VarianceRupiahThreshold, request.RolloverThreshold} {
			if !nonNegativeDecimal(value) {
				return PolicyRevision{}, ErrInvalidRequest
			}
		}
	case "evidence":
		if request.EvidenceMode != "wajib" && request.EvidenceMode != "opsional" {
			return PolicyRevision{}, ErrInvalidRequest
		}
	default:
		return PolicyRevision{}, ErrInvalidRequest
	}
	return s.repository.CreateRevision(ctx, request, s.clock.Now().UTC())
}

// History returns append-only policy revisions in valid-from order.
func (s *Service) History(ctx context.Context, orgID uuid.UUID, stationID *uuid.UUID) ([]RevisionView, error) {
	if s == nil || s.repository == nil || orgID == uuid.Nil || (stationID != nil && *stationID == uuid.Nil) {
		return nil, ErrInvalidRequest
	}
	repository, ok := s.repository.(ReadRepository)
	if !ok {
		return nil, ErrInvalidRequest
	}
	return repository.History(ctx, orgID, stationID)
}

func nonNegativeDecimal(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	if !fixedDecimalPattern.MatchString(value) {
		return false
	}
	number, ok := new(big.Rat).SetString(value)
	return ok && number.Sign() >= 0
}

var fixedDecimalPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?$`)
