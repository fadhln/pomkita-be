// Package auth contains authentication and session use cases.
package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fadhln/pomkita-be/internal/domain"
	appjwt "github.com/fadhln/pomkita-be/internal/jwt"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// User is the credential record required by the authentication use case.
type User struct {
	UserID       uuid.UUID
	OrgID        uuid.UUID
	DisplayName  string
	Email        string
	Username     string
	PasswordHash string
	Enabled      bool
}

// Repository provides user and verified-session data to the authentication service.
type Repository interface {
	FindUserByUsername(context.Context, string) (User, error)
	ReadSession(context.Context, uuid.UUID, uuid.UUID) (appjwt.SessionView, error)
	OrganizationExists(context.Context, uuid.UUID) (bool, error)
	StationInOrganization(context.Context, uuid.UUID, uuid.UUID) (bool, error)
	SetActiveContext(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, time.Time) error
	ReadContextPreference(context.Context, uuid.UUID) (*appjwt.ActiveContext, error)
}

var (
	// ErrInvalidCredentials is returned for an unknown, disabled, or mismatched login.
	ErrInvalidCredentials = domain.NewError(domain.CategoryAuthentication, "invalid_credentials")
	// ErrUserNotFound identifies an absent user at the repository boundary.
	ErrUserNotFound = domain.NewError(domain.CategoryAuthentication, "user_not_found")
	// ErrDependencyUnavailable identifies a missing service dependency.
	ErrDependencyUnavailable = domain.NewError(domain.CategoryDependency, "dependency_unavailable")
	// ErrActiveContextForbidden identifies an actor that cannot change session context.
	ErrActiveContextForbidden = domain.NewError(domain.CategoryAuthorization, "active_context_forbidden")
	// ErrActiveContextNotFound identifies an absent organization or station outside the organization.
	ErrActiveContextNotFound = domain.NewError(domain.CategoryNotFound, "active_context_target_not_found")
	// ErrActiveContextInvalid identifies missing context identifiers.
	ErrActiveContextInvalid = domain.NewError(domain.CategoryValidation, "invalid_active_context")
)

// Service owns authentication and session use cases.
type Service struct {
	repository Repository
	tokens     *appjwt.Service
}

// NewService creates an authentication service.
func NewService(repository Repository, tokens *appjwt.Service) *Service {
	return &Service{repository: repository, tokens: tokens}
}

// Login verifies an enabled user's password and creates a session token.
func (s *Service) Login(ctx context.Context, username, password string) (string, appjwt.Claims, error) {
	if s == nil || s.repository == nil || s.tokens == nil {
		return "", appjwt.Claims{}, ErrDependencyUnavailable
	}
	username = strings.ToLower(strings.TrimSpace(username))
	user, err := s.repository.FindUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return "", appjwt.Claims{}, ErrInvalidCredentials
		}
		return "", appjwt.Claims{}, fmt.Errorf("find user for login: %w", err)
	}
	if !user.Enabled || user.PasswordHash == "" || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return "", appjwt.Claims{}, ErrInvalidCredentials
	}
	token, claims, err := s.tokens.Issue(ctx, user.UserID)
	if err != nil {
		return "", appjwt.Claims{}, err
	}
	preference, err := s.repository.ReadContextPreference(ctx, user.UserID)
	if err != nil {
		return "", appjwt.Claims{}, fmt.Errorf("read account context preference: %w", err)
	}
	if preference == nil {
		return token, claims, nil
	}
	orgExists, err := s.repository.OrganizationExists(ctx, preference.OrgID)
	if err != nil {
		return "", appjwt.Claims{}, fmt.Errorf("check preferred organization: %w", err)
	}
	if !orgExists {
		return token, claims, nil
	}
	stationExists, err := s.repository.StationInOrganization(ctx, preference.OrgID, preference.StationID)
	if err != nil {
		return "", appjwt.Claims{}, fmt.Errorf("check preferred station: %w", err)
	}
	if !stationExists {
		return token, claims, nil
	}
	if err := s.repository.SetActiveContext(ctx, claims.JTI, preference.OrgID, preference.StationID, time.Now().UTC()); err != nil {
		return "", appjwt.Claims{}, fmt.Errorf("restore account context preference: %w", err)
	}
	return token, claims, nil
}

// Logout revokes a session by its verified token identifier.
func (s *Service) Logout(ctx context.Context, jti uuid.UUID) error {
	if s == nil || s.tokens == nil {
		return ErrDependencyUnavailable
	}
	return s.tokens.Logout(ctx, jti)
}

// SetActiveContext stores an existing organization and station for one session.
func (s *Service) SetActiveContext(ctx context.Context, jti uuid.UUID, roles []string, orgID, stationID uuid.UUID) error {
	if s == nil || s.repository == nil {
		return ErrDependencyUnavailable
	}
	roleAllowed := false
	for _, role := range roles {
		if role == "Superadmin" {
			roleAllowed = true
			break
		}
	}
	if !roleAllowed {
		return ErrActiveContextForbidden
	}
	if jti == uuid.Nil || orgID == uuid.Nil || stationID == uuid.Nil {
		return ErrActiveContextInvalid
	}
	orgExists, err := s.repository.OrganizationExists(ctx, orgID)
	if err != nil {
		return fmt.Errorf("check active organization: %w", err)
	}
	if !orgExists {
		return ErrActiveContextNotFound
	}
	stationExists, err := s.repository.StationInOrganization(ctx, orgID, stationID)
	if err != nil {
		return fmt.Errorf("check active station: %w", err)
	}
	if !stationExists {
		return ErrActiveContextNotFound
	}
	if err := s.repository.SetActiveContext(ctx, jti, orgID, stationID, time.Now().UTC()); err != nil {
		return fmt.Errorf("set active context: %w", err)
	}
	return nil
}

// ReadSession verifies a raw token and loads its identity and station scope.
func (s *Service) ReadSession(ctx context.Context, rawToken string) (appjwt.SessionView, error) {
	if s == nil || s.repository == nil || s.tokens == nil {
		return appjwt.SessionView{}, ErrDependencyUnavailable
	}
	claims, err := s.tokens.Verify(ctx, rawToken)
	if err != nil {
		return appjwt.SessionView{}, err
	}
	view, err := s.repository.ReadSession(ctx, claims.JTI, claims.Subject)
	if err != nil {
		return appjwt.SessionView{}, fmt.Errorf("read session view: %w", err)
	}
	return view, nil
}
