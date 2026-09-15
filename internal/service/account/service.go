// Package account contains account profile and password use cases.
package account

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/pomkita/pomkita-be/internal/domain"
	"github.com/pomkita/pomkita-be/internal/platform/mailer"
	"golang.org/x/crypto/bcrypt"
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9._-]+$`)

// Clock provides the current time to account use cases.
type Clock interface{ Now() time.Time }

// Organization is the organization shown in an account profile.
type Organization struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// Station is a station the actor may use.
type Station struct {
	StationID uuid.UUID `json:"station_id"`
	Name      string    `json:"name"`
	Timezone  string    `json:"timezone"`
}

// Profile is the own account profile.
type Profile struct {
	UserID      uuid.UUID    `json:"user_id"`
	Email       string       `json:"email"`
	Username    string       `json:"username"`
	DisplayName string       `json:"display_name"`
	Org         Organization `json:"org"`
	Roles       []string     `json:"roles"`
	Stations    []Station    `json:"stations"`
}

// UpdateRequest contains optional account fields.
type UpdateRequest struct {
	DisplayName *string `json:"display_name,omitempty"`
	Username    *string `json:"username,omitempty"`
}

// PasswordRequest contains credentials for an authenticated password change.
type PasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// ResetRequest contains a password reset token and its new password.
type ResetRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

// Credentials is the password data needed to change an account password.
type Credentials struct {
	UserID       uuid.UUID
	PasswordHash string
	Enabled      bool
}

// ResetTarget is the safe account data needed to issue a reset message.
type ResetTarget struct {
	UserID  uuid.UUID
	OrgID   uuid.UUID
	Email   string
	Enabled bool
}

// Repository persists account state and its audit events.
type Repository interface {
	ReadProfile(context.Context, uuid.UUID) (Profile, error)
	UpdateProfile(context.Context, uuid.UUID, UpdateRequest, uuid.UUID, time.Time) (Profile, error)
	Credentials(context.Context, uuid.UUID) (Credentials, error)
	ChangePassword(context.Context, uuid.UUID, string, uuid.UUID, uuid.UUID, time.Time) error
	FindResetTarget(context.Context, string) (ResetTarget, error)
	IssuePasswordReset(context.Context, uuid.UUID, uuid.UUID, []byte, time.Time, time.Time) error
	ResetPassword(context.Context, []byte, string, uuid.UUID, time.Time) error
}

var (
	// ErrInvalidUsername identifies a username outside the allowed format.
	ErrInvalidUsername = domain.NewError(domain.CategoryValidation, "invalid_username")
	// ErrUsernameConflict identifies an existing username.
	ErrUsernameConflict = domain.NewError(domain.CategoryConflict, "username_conflict")
	// ErrInvalidDisplayName identifies an empty display name.
	ErrInvalidDisplayName = domain.NewError(domain.CategoryValidation, "invalid_display_name")
	// ErrWeakPassword identifies a password below the minimum length.
	ErrWeakPassword = domain.NewError(domain.CategoryValidation, "weak_password")
	// ErrCurrentPasswordIncorrect identifies a wrong current password.
	ErrCurrentPasswordIncorrect = domain.NewError(domain.CategoryValidation, "current_password_incorrect")
	// ErrInvalidToken identifies an unknown, consumed, or expired reset token.
	ErrInvalidToken = domain.NewError(domain.CategoryValidation, "INVALID_TOKEN")
	// ErrUserNotFound identifies an absent account at the repository boundary.
	ErrUserNotFound = domain.NewError(domain.CategoryNotFound, "user_not_found")
	// ErrDisabledAccount identifies an account that cannot perform account operations.
	ErrDisabledAccount = domain.NewError(domain.CategoryAuthentication, "disabled_account")
	// ErrDependencyUnavailable identifies a missing account dependency.
	ErrDependencyUnavailable = domain.NewError(domain.CategoryDependency, "dependency_unavailable")
)

// Service owns account profile and password rules.
type Service struct {
	repository Repository
	clock      Clock
	mailer     mailer.Mailer
	baseURL    string
}

// NewService creates an account service.
func NewService(repository Repository, clock Clock, sender mailer.Mailer, publicBaseURL string) *Service {
	return &Service{repository: repository, clock: clock, mailer: sender, baseURL: strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")}
}

// Read returns the profile for one authenticated actor.
func (s *Service) Read(ctx context.Context, userID uuid.UUID) (Profile, error) {
	if s == nil || s.repository == nil || userID == uuid.Nil {
		return Profile{}, ErrDependencyUnavailable
	}
	return s.repository.ReadProfile(ctx, userID)
}

// Update changes the actor's optional profile fields and writes an audit event.
func (s *Service) Update(ctx context.Context, userID, sessionID uuid.UUID, request UpdateRequest) (Profile, error) {
	if s == nil || s.repository == nil || s.clock == nil || userID == uuid.Nil || sessionID == uuid.Nil {
		return Profile{}, ErrDependencyUnavailable
	}
	normalized := request
	if normalized.Username != nil {
		value := strings.ToLower(strings.TrimSpace(*normalized.Username))
		if !validUsername(value) {
			return Profile{}, ErrInvalidUsername
		}
		normalized.Username = &value
	}
	if normalized.DisplayName != nil {
		value := strings.TrimSpace(*normalized.DisplayName)
		if value == "" {
			return Profile{}, ErrInvalidDisplayName
		}
		normalized.DisplayName = &value
	}
	return s.repository.UpdateProfile(ctx, userID, normalized, uuid.New(), s.clock.Now().UTC().Truncate(time.Microsecond))
}

// ChangePassword verifies and changes the actor's password, keeping the current session.
func (s *Service) ChangePassword(ctx context.Context, userID, sessionID uuid.UUID, request PasswordRequest) error {
	if s == nil || s.repository == nil || s.clock == nil || userID == uuid.Nil || sessionID == uuid.Nil {
		return ErrDependencyUnavailable
	}
	if utf8.RuneCountInString(request.NewPassword) < 8 {
		return ErrWeakPassword
	}
	credentials, err := s.repository.Credentials(ctx, userID)
	if err != nil {
		return err
	}
	if !credentials.Enabled {
		return ErrDisabledAccount
	}
	if credentials.PasswordHash == "" || bcrypt.CompareHashAndPassword([]byte(credentials.PasswordHash), []byte(request.CurrentPassword)) != nil {
		return ErrCurrentPasswordIncorrect
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(request.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash account password: %w", err)
	}
	return s.repository.ChangePassword(ctx, userID, string(hash), sessionID, uuid.New(), s.clock.Now().UTC().Truncate(time.Microsecond))
}

// Forgot issues a reset token for an enabled account and sends its email.
func (s *Service) Forgot(ctx context.Context, identifier string) error {
	if s == nil || s.repository == nil || s.clock == nil {
		return ErrDependencyUnavailable
	}
	// Keep a fixed bcrypt operation in every branch of this lookup.
	_ = bcrypt.CompareHashAndPassword([]byte("$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"), []byte(identifier))
	target, err := s.repository.FindResetTarget(ctx, strings.TrimSpace(identifier))
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return nil
		}
		return err
	}
	if !target.Enabled {
		return nil
	}
	if s.mailer == nil || strings.TrimSpace(s.baseURL) == "" {
		return ErrDependencyUnavailable
	}
	rawToken, err := newToken()
	if err != nil {
		return fmt.Errorf("create password reset token: %w", err)
	}
	now := s.clock.Now().UTC().Truncate(time.Microsecond)
	digest := sha256.Sum256([]byte(rawToken))
	if err := s.repository.IssuePasswordReset(ctx, target.UserID, uuid.New(), digest[:], now, now.Add(60*time.Minute)); err != nil {
		return err
	}
	if err := s.mailer.Send(ctx, mailer.Message{To: target.Email, Subject: "Reset sandi PomKita", TextBody: s.baseURL + "/reset-sandi?token=" + rawToken}); err != nil {
		return fmt.Errorf("send password reset email: %w", err)
	}
	return nil
}

// Reset changes a password, consumes the token, and revokes all sessions.
func (s *Service) Reset(ctx context.Context, request ResetRequest) error {
	if s == nil || s.repository == nil || s.clock == nil {
		return ErrDependencyUnavailable
	}
	if strings.TrimSpace(request.Token) == "" {
		return ErrInvalidToken
	}
	if utf8.RuneCountInString(request.Password) < 8 {
		return ErrWeakPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(request.Password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash reset password: %w", err)
	}
	digest := sha256.Sum256([]byte(request.Token))
	return s.repository.ResetPassword(ctx, digest[:], string(hash), uuid.New(), s.clock.Now().UTC().Truncate(time.Microsecond))
}

func validUsername(username string) bool {
	return utf8.RuneCountInString(username) >= 3 && utf8.RuneCountInString(username) <= 32 && usernamePattern.MatchString(username)
}

func newToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
