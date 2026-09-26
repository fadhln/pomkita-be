// Package station contains station administration use cases.
package station

import (
	"context"
	"strings"
	"time"

	"github.com/fadhln/pomkita-be/internal/domain"
	"github.com/google/uuid"
)

// Actor is the verified session identity used for station authorization.
type Actor struct {
	UserID uuid.UUID
	OrgID  uuid.UUID
	Roles  []string
}

// StationDetail is the public station detail.
type StationDetail struct {
	OrgID     uuid.UUID  `json:"org_id"`
	StationID uuid.UUID  `json:"station_id"`
	Name      string     `json:"name"`
	Code      string     `json:"code"`
	Address   string     `json:"address"`
	Timezone  string     `json:"timezone"`
	Enabled   bool       `json:"enabled"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at"`
}

// Station is kept as a short name for service callers.
type Station = StationDetail

// StationCreateRequest contains station values for creation.
type StationCreateRequest struct {
	Name     string `json:"name"`
	Code     string `json:"code"`
	Address  string `json:"address"`
	Timezone string `json:"timezone"`
}

// CreateRequest is kept as a short name for service callers.
type CreateRequest = StationCreateRequest

// StationUpdateRequest contains station values that may change.
type StationUpdateRequest struct {
	Name     *string `json:"name,omitempty"`
	Code     *string `json:"code,omitempty"`
	Address  *string `json:"address,omitempty"`
	Timezone *string `json:"timezone,omitempty"`
	Enabled  *bool   `json:"enabled,omitempty"`
}

// UpdateRequest is kept as a short name for service callers.
type UpdateRequest = StationUpdateRequest

// Repository persists stations and audit events.
type Repository interface {
	List(context.Context, uuid.UUID) ([]Station, error)
	Read(context.Context, uuid.UUID, uuid.UUID) (Station, error)
	Create(context.Context, uuid.UUID, CreateRequest, uuid.UUID, time.Time) (Station, error)
	Update(context.Context, uuid.UUID, uuid.UUID, UpdateRequest, uuid.UUID, time.Time) (Station, error)
	Disable(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, time.Time) error
}

// Clock provides the current time to station use cases.
type Clock interface{ Now() time.Time }

// Service owns station authorization and validation rules.
type Service struct {
	repository Repository
	clock      Clock
}

// NewService creates a station service.
func NewService(repository Repository, clock Clock) *Service {
	return &Service{repository: repository, clock: clock}
}

var (
	// ErrForbidden identifies an actor without station authority.
	ErrForbidden = domain.NewError(domain.CategoryAuthorization, "station_administration_forbidden")
	// ErrEnabledChangeForbidden identifies an Owner attempt to change station state.
	ErrEnabledChangeForbidden = domain.NewError(domain.CategoryAuthorization, "station_enabled_change_forbidden")
	// ErrStationNotFound hides a station outside the permitted scope.
	ErrStationNotFound = domain.NewError(domain.CategoryNotFound, "station_not_found")
	// ErrOrganizationScopeRequired identifies a missing Superadmin organization scope.
	ErrOrganizationScopeRequired = domain.NewError(domain.CategoryValidation, "organization_scope_required")
	// ErrInvalidRequest identifies an invalid station mutation.
	ErrInvalidRequest = domain.NewError(domain.CategoryValidation, "invalid_station_request")
	// ErrCodeConflict identifies a duplicate station code in one organization.
	ErrCodeConflict = domain.NewError(domain.CategoryConflict, "station_code_conflict")
	// ErrDependencyUnavailable identifies a missing station dependency.
	ErrDependencyUnavailable = domain.NewError(domain.CategoryDependency, "dependency_unavailable")
)

// List returns stations in the actor's permitted organization.
func (s *Service) List(ctx context.Context, actor Actor, orgID uuid.UUID) ([]Station, error) {
	role, err := authorize(actor)
	if err != nil {
		return nil, err
	}
	orgID, err = permittedOrganization(actor, role, orgID)
	if err != nil {
		return nil, err
	}
	if s == nil || s.repository == nil {
		return nil, ErrDependencyUnavailable
	}
	return s.repository.List(ctx, orgID)
}

// Read returns one station if it is visible to the actor.
func (s *Service) Read(ctx context.Context, actor Actor, orgID, stationID uuid.UUID) (Station, error) {
	role, err := authorize(actor)
	if err != nil {
		return Station{}, err
	}
	orgID, err = permittedOrganization(actor, role, orgID)
	if err != nil || stationID == uuid.Nil {
		if err != nil {
			return Station{}, err
		}
		return Station{}, ErrStationNotFound
	}
	if s == nil || s.repository == nil {
		return Station{}, ErrDependencyUnavailable
	}
	return s.repository.Read(ctx, orgID, stationID)
}

// Create creates a station in the permitted organization.
func (s *Service) Create(ctx context.Context, actor Actor, orgID uuid.UUID, request CreateRequest) (Station, error) {
	role, err := authorize(actor)
	if err != nil {
		return Station{}, err
	}
	orgID, err = permittedOrganization(actor, role, orgID)
	if err != nil {
		return Station{}, err
	}
	if s == nil || s.repository == nil || s.clock == nil {
		return Station{}, ErrDependencyUnavailable
	}
	if err := validateCreate(&request); err != nil {
		return Station{}, err
	}
	return s.repository.Create(ctx, orgID, request, actor.UserID, timestamp(s.clock))
}

// Update changes a station and writes an audit event.
func (s *Service) Update(ctx context.Context, actor Actor, orgID, stationID uuid.UUID, request UpdateRequest) (Station, error) {
	role, err := authorize(actor)
	if err != nil {
		return Station{}, err
	}
	orgID, err = permittedOrganization(actor, role, orgID)
	if err != nil || stationID == uuid.Nil {
		if err != nil {
			return Station{}, err
		}
		return Station{}, ErrStationNotFound
	}
	if s == nil || s.repository == nil || s.clock == nil {
		return Station{}, ErrDependencyUnavailable
	}
	if err := validateUpdate(&request); err != nil {
		return Station{}, err
	}
	if request.Enabled != nil && role != "Superadmin" {
		return Station{}, ErrEnabledChangeForbidden
	}
	return s.repository.Update(ctx, orgID, stationID, request, actor.UserID, timestamp(s.clock))
}

// Disable disables a station without deleting it.
func (s *Service) Disable(ctx context.Context, actor Actor, orgID, stationID uuid.UUID) error {
	role, err := authorize(actor)
	if err != nil {
		return err
	}
	orgID, err = permittedOrganization(actor, role, orgID)
	if err != nil {
		return err
	}
	if stationID == uuid.Nil {
		return ErrStationNotFound
	}
	if s == nil || s.repository == nil || s.clock == nil {
		return ErrDependencyUnavailable
	}
	return s.repository.Disable(ctx, orgID, stationID, actor.UserID, timestamp(s.clock))
}

func authorize(actor Actor) (string, error) {
	for _, role := range actor.Roles {
		if role == "Superadmin" {
			return role, nil
		}
	}
	for _, role := range actor.Roles {
		if role == "Owner" {
			return role, nil
		}
	}
	return "", ErrForbidden
}

func permittedOrganization(actor Actor, role string, requested uuid.UUID) (uuid.UUID, error) {
	if role == "Superadmin" {
		if requested == uuid.Nil {
			return uuid.Nil, ErrOrganizationScopeRequired
		}
		return requested, nil
	}
	if actor.OrgID == uuid.Nil || (requested != uuid.Nil && requested != actor.OrgID) {
		return uuid.Nil, ErrStationNotFound
	}
	return actor.OrgID, nil
}

func validateCreate(request *CreateRequest) error {
	if strings.TrimSpace(request.Name) == "" || strings.TrimSpace(request.Timezone) == "" {
		return ErrInvalidRequest
	}
	if _, err := time.LoadLocation(strings.TrimSpace(request.Timezone)); err != nil {
		return ErrInvalidRequest
	}
	request.Name = strings.TrimSpace(request.Name)
	request.Code = strings.TrimSpace(request.Code)
	request.Address = strings.TrimSpace(request.Address)
	request.Timezone = strings.TrimSpace(request.Timezone)
	return nil
}

func validateUpdate(request *UpdateRequest) error {
	if request.Name == nil && request.Code == nil && request.Address == nil && request.Timezone == nil && request.Enabled == nil {
		return ErrInvalidRequest
	}
	if request.Name != nil {
		*request.Name = strings.TrimSpace(*request.Name)
		if *request.Name == "" {
			return ErrInvalidRequest
		}
	}
	if request.Code != nil {
		*request.Code = strings.TrimSpace(*request.Code)
	}
	if request.Address != nil {
		*request.Address = strings.TrimSpace(*request.Address)
	}
	if request.Timezone != nil {
		*request.Timezone = strings.TrimSpace(*request.Timezone)
		if *request.Timezone == "" {
			return ErrInvalidRequest
		}
		if _, err := time.LoadLocation(*request.Timezone); err != nil {
			return ErrInvalidRequest
		}
	}
	return nil
}

func timestamp(clock Clock) time.Time { return clock.Now().UTC().Truncate(time.Microsecond) }
