// Package organization contains organization administration use cases.
package organization

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pomkita/pomkita-be/internal/domain"
)

// Actor is the verified session identity used for authorization.
type Actor struct {
	UserID uuid.UUID
	OrgID  uuid.UUID
	Roles  []string
}

// OrganizationDetail is the public organization detail.
type OrganizationDetail struct {
	OrgID        uuid.UUID  `json:"org_id"`
	Name         string     `json:"name"`
	LegalName    string     `json:"legal_name"`
	Address      string     `json:"address"`
	ContactEmail string     `json:"contact_email"`
	Timezone     string     `json:"timezone"`
	Enabled      bool       `json:"enabled"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    *time.Time `json:"updated_at"`
}

// Organization is kept as a short name for service callers.
type Organization = OrganizationDetail

// FirstStationRequest contains the station required when an organization is created.
type FirstStationRequest struct {
	Name     string `json:"name"`
	Timezone string `json:"timezone"`
}

// CreateRequest contains organization and first-station values.
type CreateRequest struct {
	Name         string               `json:"name"`
	LegalName    string               `json:"legal_name"`
	Address      string               `json:"address"`
	ContactEmail string               `json:"contact_email"`
	Timezone     string               `json:"timezone"`
	FirstStation *FirstStationRequest `json:"first_station"`
}

// OrganizationUpdateRequest contains fields that may change on an organization.
type OrganizationUpdateRequest struct {
	Name         *string `json:"name,omitempty"`
	LegalName    *string `json:"legal_name,omitempty"`
	Address      *string `json:"address,omitempty"`
	ContactEmail *string `json:"contact_email,omitempty"`
	Timezone     *string `json:"timezone,omitempty"`
	Enabled      *bool   `json:"enabled,omitempty"`
}

// UpdateRequest is kept as a short name for service callers.
type UpdateRequest = OrganizationUpdateRequest

// Repository persists organizations, stations, and audit events.
type Repository interface {
	List(context.Context, uuid.UUID, bool) ([]Organization, error)
	Read(context.Context, uuid.UUID) (Organization, error)
	Create(context.Context, CreateRequest, uuid.UUID, time.Time) (Organization, error)
	Update(context.Context, uuid.UUID, UpdateRequest, uuid.UUID, time.Time) (Organization, error)
	Disable(context.Context, uuid.UUID, uuid.UUID, time.Time) error
}

// Clock provides the current time to organization use cases.
type Clock interface{ Now() time.Time }

// Service owns organization authorization and validation rules.
type Service struct {
	repository Repository
	clock      Clock
}

// NewService creates an organization service.
func NewService(repository Repository, clock Clock) *Service {
	return &Service{repository: repository, clock: clock}
}

var (
	// ErrForbidden identifies an actor without organization authority.
	ErrForbidden = domain.NewError(domain.CategoryAuthorization, "organization_administration_forbidden")
	// ErrOrganizationNotFound hides an organization outside the actor scope.
	ErrOrganizationNotFound = domain.NewError(domain.CategoryNotFound, "organization_not_found")
	// ErrFirstStationRequired identifies a create request without its required station.
	ErrFirstStationRequired = domain.NewError(domain.CategoryValidation, "first_station_required")
	// ErrNoStation identifies a repository create request that cannot create its station.
	ErrNoStation = domain.NewError(domain.CategoryValidation, "organization_has_no_station")
	// ErrEnabledChangeForbidden identifies an Owner attempt to change enabled.
	ErrEnabledChangeForbidden = domain.NewError(domain.CategoryAuthorization, "organization_enabled_change_forbidden")
	// ErrInvalidRequest identifies an invalid organization mutation.
	ErrInvalidRequest = domain.NewError(domain.CategoryValidation, "invalid_organization_request")
	// ErrDependencyUnavailable identifies a missing organization dependency.
	ErrDependencyUnavailable = domain.NewError(domain.CategoryDependency, "dependency_unavailable")
)

// List returns organizations visible to the actor.
func (s *Service) List(ctx context.Context, actor Actor) ([]Organization, error) {
	role, err := authorize(actor)
	if err != nil || s == nil || s.repository == nil {
		return nil, serviceError(err, s)
	}
	return s.repository.List(ctx, actor.OrgID, role == "Superadmin")
}

// Read returns one organization if it is visible to the actor.
func (s *Service) Read(ctx context.Context, actor Actor, orgID uuid.UUID) (Organization, error) {
	role, err := authorize(actor)
	if err != nil {
		return Organization{}, err
	}
	if orgID == uuid.Nil || (role == "Owner" && actor.OrgID != orgID) {
		return Organization{}, ErrOrganizationNotFound
	}
	if s == nil || s.repository == nil {
		return Organization{}, ErrDependencyUnavailable
	}
	return s.repository.Read(ctx, orgID)
}

// Create creates an organization and its first station in one transaction.
func (s *Service) Create(ctx context.Context, actor Actor, request CreateRequest) (Organization, error) {
	if role, err := authorize(actor); err != nil || role != "Superadmin" {
		if err != nil {
			return Organization{}, err
		}
		return Organization{}, ErrForbidden
	}
	if s == nil || s.repository == nil || s.clock == nil {
		return Organization{}, ErrDependencyUnavailable
	}
	if err := validateCreate(&request); err != nil {
		return Organization{}, err
	}
	return s.repository.Create(ctx, request, actor.UserID, timestamp(s.clock))
}

// Update changes visible organization fields and writes an audit event.
func (s *Service) Update(ctx context.Context, actor Actor, orgID uuid.UUID, request UpdateRequest) (Organization, error) {
	role, err := authorize(actor)
	if err != nil {
		return Organization{}, err
	}
	if orgID == uuid.Nil || (role == "Owner" && actor.OrgID != orgID) {
		return Organization{}, ErrOrganizationNotFound
	}
	if role == "Owner" && request.Enabled != nil {
		return Organization{}, ErrEnabledChangeForbidden
	}
	if s == nil || s.repository == nil || s.clock == nil {
		return Organization{}, ErrDependencyUnavailable
	}
	if err := validateUpdate(&request); err != nil {
		return Organization{}, err
	}
	return s.repository.Update(ctx, orgID, request, actor.UserID, timestamp(s.clock))
}

// Disable disables an organization without deleting it.
func (s *Service) Disable(ctx context.Context, actor Actor, orgID uuid.UUID) error {
	role, err := authorize(actor)
	if err != nil {
		return err
	}
	if role != "Superadmin" {
		return ErrForbidden
	}
	if orgID == uuid.Nil {
		return ErrOrganizationNotFound
	}
	if s == nil || s.repository == nil || s.clock == nil {
		return ErrDependencyUnavailable
	}
	return s.repository.Disable(ctx, orgID, actor.UserID, timestamp(s.clock))
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

func validateCreate(request *CreateRequest) error {
	if request.FirstStation == nil {
		return ErrFirstStationRequired
	}
	for _, value := range []string{request.Name, request.LegalName, request.Address, request.ContactEmail, request.Timezone, request.FirstStation.Name, request.FirstStation.Timezone} {
		if strings.TrimSpace(value) == "" {
			return ErrInvalidRequest
		}
	}
	if _, err := time.LoadLocation(request.Timezone); err != nil {
		return ErrInvalidRequest
	}
	if _, err := time.LoadLocation(request.FirstStation.Timezone); err != nil {
		return ErrInvalidRequest
	}
	request.Name = strings.TrimSpace(request.Name)
	request.LegalName = strings.TrimSpace(request.LegalName)
	request.Address = strings.TrimSpace(request.Address)
	request.ContactEmail = strings.TrimSpace(request.ContactEmail)
	request.Timezone = strings.TrimSpace(request.Timezone)
	request.FirstStation.Name = strings.TrimSpace(request.FirstStation.Name)
	request.FirstStation.Timezone = strings.TrimSpace(request.FirstStation.Timezone)
	return nil
}

func validateUpdate(request *UpdateRequest) error {
	if request.Name == nil && request.LegalName == nil && request.Address == nil && request.ContactEmail == nil && request.Timezone == nil && request.Enabled == nil {
		return ErrInvalidRequest
	}
	for _, value := range []*string{request.Name, request.LegalName, request.Address, request.ContactEmail, request.Timezone} {
		if value != nil {
			trimmed := strings.TrimSpace(*value)
			if trimmed == "" {
				return ErrInvalidRequest
			}
			*value = trimmed
		}
	}
	if request.Timezone != nil {
		if _, err := time.LoadLocation(*request.Timezone); err != nil {
			return ErrInvalidRequest
		}
	}
	return nil
}

func timestamp(clock Clock) time.Time { return clock.Now().UTC().Truncate(time.Microsecond) }

func serviceError(err error, service *Service) error {
	if err != nil {
		return err
	}
	if service == nil || service.repository == nil {
		return ErrDependencyUnavailable
	}
	return nil
}
