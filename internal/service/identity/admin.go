package identity

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pomkita/pomkita-be/internal/domain"
)

// Actor is the verified session identity used by user administration.
type Actor struct {
	UserID uuid.UUID
	OrgID  uuid.UUID
	Roles  []string
}

// RoleView is one role granted to a user at one station.
type RoleView struct {
	Role        string    `json:"role"`
	StationID   uuid.UUID `json:"station_id"`
	StationName string    `json:"station_name"`
}

// StationView is one station available to a user through a role.
type StationView struct {
	StationID uuid.UUID `json:"station_id"`
	Name      string    `json:"name"`
}

// UserView is the user administration representation.
type UserView struct {
	UserID      uuid.UUID     `json:"user_id"`
	Email       string        `json:"email"`
	Username    *string       `json:"username"`
	DisplayName string        `json:"display_name"`
	Enabled     bool          `json:"enabled"`
	Roles       []RoleView    `json:"roles"`
	Stations    []StationView `json:"stations"`
}

// UserUpdateRequest contains the editable administration fields.
type UserUpdateRequest struct {
	DisplayName string `json:"display_name"`
	Enabled     bool   `json:"enabled"`
}

// RoleRequest identifies one station role.
type RoleRequest struct {
	StationID uuid.UUID `json:"station_id"`
	Role      string    `json:"role"`
}

// RoleHistoryEvent is one role assignment or removal from the audit chain.
type RoleHistoryEvent struct {
	Sequence  int64     `json:"sequence"`
	Actor     uuid.UUID `json:"actor"`
	Target    uuid.UUID `json:"target"`
	StationID uuid.UUID `json:"station_id"`
	Station   string    `json:"station"`
	Role      string    `json:"role"`
	Before    bool      `json:"before"`
	After     bool      `json:"after"`
	CreatedAt string    `json:"created_at"`
}

// PasswordResetResult contains the one-time reset link.
type PasswordResetResult struct {
	Link string `json:"link"`
}

// AdminRepository persists user administration operations.
type AdminRepository interface {
	ListUsers(context.Context, uuid.UUID) ([]UserView, error)
	ReadUser(context.Context, uuid.UUID, uuid.UUID) (UserView, error)
	UpdateUser(context.Context, Actor, uuid.UUID, UserUpdateRequest, uuid.UUID, time.Time) (UserView, error)
	AssignRole(context.Context, Actor, uuid.UUID, RoleRequest, uuid.UUID, time.Time) ([]RoleView, error)
	RemoveRole(context.Context, Actor, uuid.UUID, RoleRequest, uuid.UUID, time.Time) error
	RoleHistory(context.Context, uuid.UUID, uuid.UUID) ([]RoleHistoryEvent, error)
	IssuePasswordReset(context.Context, Actor, uuid.UUID, uuid.UUID, []byte, time.Time, time.Time) error
}

var (
	// ErrInvalidAdminRequest identifies an incomplete administration request.
	ErrInvalidAdminRequest = domain.NewError(domain.CategoryValidation, "invalid_user_administration_request")
	// ErrRoleExists identifies a role that is already granted.
	ErrRoleExists = domain.NewError(domain.CategoryConflict, "role_exists")
	// ErrRoleNotFound identifies a role that is not granted.
	ErrRoleNotFound = domain.NewError(domain.CategoryNotFound, "role_not_found")
	// ErrLastOwner identifies an organization with only one remaining Owner.
	ErrLastOwner = domain.NewError(domain.CategoryValidation, "last_owner_forbidden")
	// ErrProtectedUser identifies a privileged user that an Owner cannot edit.
	ErrProtectedUser = domain.NewError(domain.CategoryAuthorization, "protected_user_forbidden")
	// ErrSelfRoleChange identifies an Owner changing the own roles.
	ErrSelfRoleChange = domain.NewError(domain.CategoryAuthorization, "self_role_change_forbidden")
	// ErrRoleSeparation identifies a role combination that breaks separation of duties.
	ErrRoleSeparation = domain.NewError(domain.CategoryAuthorization, "role_separation_forbidden")
	// ErrDisabledTarget identifies a disabled user that cannot receive a reset.
	ErrDisabledTarget = domain.NewError(domain.CategoryValidation, "disabled_user_forbidden")
)

// ListUsers lists users in the actor's permitted organization.
func (s *Service) ListUsers(ctx context.Context, actor Actor, orgID uuid.UUID) ([]UserView, error) {
	role, err := authorizeAdmin(actor)
	if err != nil {
		return nil, err
	}
	if role == "Owner" {
		if orgID == uuid.Nil {
			orgID = actor.OrgID
		}
		if orgID != actor.OrgID {
			return nil, ErrWrongOrganization
		}
	} else if orgID == uuid.Nil {
		return nil, ErrInvalidAdminRequest
	}
	if s == nil || s.adminRepository == nil {
		return nil, ErrDependencyUnavailable
	}
	return s.adminRepository.ListUsers(ctx, orgID)
}

// ReadUser reads one user in the actor's permitted scope.
func (s *Service) ReadUser(ctx context.Context, actor Actor, userID uuid.UUID) (UserView, error) {
	if _, err := authorizeAdmin(actor); err != nil {
		return UserView{}, err
	}
	if userID == uuid.Nil {
		return UserView{}, ErrInvalidAdminRequest
	}
	if s == nil || s.adminRepository == nil {
		return UserView{}, ErrDependencyUnavailable
	}
	orgID := actor.OrgID
	if isSuperadmin(actor) {
		orgID = uuid.Nil
	}
	return s.adminRepository.ReadUser(ctx, orgID, userID)
}

// UpdateUser changes a user's display name and enabled state.
func (s *Service) UpdateUser(ctx context.Context, actor Actor, userID uuid.UUID, request UserUpdateRequest) (UserView, error) {
	role, err := authorizeAdmin(actor)
	if err != nil {
		return UserView{}, err
	}
	if userID == uuid.Nil || strings.TrimSpace(request.DisplayName) == "" {
		return UserView{}, ErrInvalidAdminRequest
	}
	if role == "Owner" && userID == actor.UserID {
		return UserView{}, ErrProtectedUser
	}
	if s == nil || s.adminRepository == nil || s.clock == nil {
		return UserView{}, ErrDependencyUnavailable
	}
	request.DisplayName = strings.TrimSpace(request.DisplayName)
	return s.adminRepository.UpdateUser(ctx, actor, userID, request, uuid.New(), adminNow(s.clock))
}

// AssignRole grants one role at one station.
func (s *Service) AssignRole(ctx context.Context, actor Actor, userID uuid.UUID, request RoleRequest) ([]RoleView, error) {
	role, err := authorizeAdmin(actor)
	if err != nil {
		return nil, err
	}
	if !validRoleRequest(userID, request) {
		return nil, ErrInvalidAdminRequest
	}
	if role == "Owner" {
		if userID == actor.UserID {
			return nil, ErrSelfRoleChange
		}
		if request.Role == "Owner" || request.Role == "Superadmin" {
			return nil, ErrForbidden
		}
	}
	if s == nil || s.adminRepository == nil || s.clock == nil {
		return nil, ErrDependencyUnavailable
	}
	return s.adminRepository.AssignRole(ctx, actor, userID, request, uuid.New(), adminNow(s.clock))
}

// RemoveRole removes one role at one station.
func (s *Service) RemoveRole(ctx context.Context, actor Actor, userID uuid.UUID, request RoleRequest) error {
	role, err := authorizeAdmin(actor)
	if err != nil {
		return err
	}
	if !validRoleRequest(userID, request) {
		return ErrInvalidAdminRequest
	}
	if role == "Owner" {
		if userID == actor.UserID {
			return ErrSelfRoleChange
		}
		if request.Role == "Owner" || request.Role == "Superadmin" {
			return ErrForbidden
		}
	}
	if s == nil || s.adminRepository == nil || s.clock == nil {
		return ErrDependencyUnavailable
	}
	return s.adminRepository.RemoveRole(ctx, actor, userID, request, uuid.New(), adminNow(s.clock))
}

// RoleHistory reads role events for one user in audit sequence order.
func (s *Service) RoleHistory(ctx context.Context, actor Actor, userID uuid.UUID) ([]RoleHistoryEvent, error) {
	if _, err := authorizeAdmin(actor); err != nil {
		return nil, err
	}
	if userID == uuid.Nil {
		return nil, ErrInvalidAdminRequest
	}
	if s == nil || s.adminRepository == nil {
		return nil, ErrDependencyUnavailable
	}
	orgID := actor.OrgID
	if isSuperadmin(actor) {
		orgID = uuid.Nil
	}
	return s.adminRepository.RoleHistory(ctx, orgID, userID)
}

// IssuePasswordReset issues one administrator reset link for a permitted user.
func (s *Service) IssuePasswordReset(ctx context.Context, actor Actor, userID uuid.UUID) (PasswordResetResult, error) {
	if s == nil {
		return PasswordResetResult{}, ErrDependencyUnavailable
	}
	role, err := authorizeAdmin(actor)
	if err != nil {
		return PasswordResetResult{}, err
	}
	if userID == uuid.Nil || strings.TrimSpace(s.baseURL) == "" {
		return PasswordResetResult{}, ErrInvalidAdminRequest
	}
	if role == "Owner" && userID == actor.UserID {
		return PasswordResetResult{}, ErrProtectedUser
	}
	if s.adminRepository == nil || s.clock == nil {
		return PasswordResetResult{}, ErrDependencyUnavailable
	}
	rawToken, err := newToken()
	if err != nil {
		return PasswordResetResult{}, fmt.Errorf("create administrator reset token: %w", err)
	}
	now := adminNow(s.clock)
	digest := sha256.Sum256([]byte(rawToken))
	if err := s.adminRepository.IssuePasswordReset(ctx, actor, userID, uuid.New(), digest[:], now, now.Add(60*time.Minute)); err != nil {
		return PasswordResetResult{}, err
	}
	return PasswordResetResult{Link: s.baseURL + "/reset-sandi?token=" + rawToken}, nil
}

func authorizeAdmin(actor Actor) (string, error) {
	if actor.UserID == uuid.Nil || actor.OrgID == uuid.Nil {
		return "", ErrForbidden
	}
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

func validRoleRequest(userID uuid.UUID, request RoleRequest) bool {
	if userID == uuid.Nil || request.StationID == uuid.Nil {
		return false
	}
	switch request.Role {
	case "Operator", "Supervisor", "Station Admin", "Owner", "Superadmin":
		return true
	default:
		return false
	}
}

func adminNow(clock Clock) time.Time { return clock.Now().UTC().Truncate(time.Microsecond) }

func isSuperadmin(actor Actor) bool {
	for _, role := range actor.Roles {
		if role == "Superadmin" {
			return true
		}
	}
	return false
}
