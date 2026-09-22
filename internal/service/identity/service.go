// Package identity contains user invitation and activation use cases.
package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/fadhln/pomkita-be/internal/domain"
	"github.com/fadhln/pomkita-be/internal/platform/mailer"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9._-]+$`)

// Clock provides the current time to identity use cases.
type Clock interface{ Now() time.Time }

// InvitationRequest contains actor and target scope for a new user.
type InvitationRequest struct {
	ActorID     uuid.UUID
	ActorOrgID  uuid.UUID
	ActorRole   string
	TargetOrgID uuid.UUID
	Email       string
	DisplayName string
	Role        string
	StationID   uuid.UUID
}

// AcceptanceRequest contains the values selected by an invited user.
type AcceptanceRequest struct {
	Token       string
	Username    string
	Password    string
	DisplayName string
}

// Repository persists identity state and its audit event in one transaction.
type Repository interface {
	CreateInvitation(context.Context, InvitationRequest, uuid.UUID, []byte, time.Time) (uuid.UUID, error)
	AcceptInvitation(context.Context, AcceptanceRequest, string, []byte, time.Time) error
}

var (
	// ErrInvalidRequest identifies an incomplete identity request.
	ErrInvalidRequest = domain.NewError(domain.CategoryValidation, "invalid_identity_request")
	// ErrForbidden identifies an actor without user administration authority.
	ErrForbidden = domain.NewError(domain.CategoryAuthorization, "user_administration_forbidden")
	// ErrWrongOrganization identifies a target outside the actor organization.
	ErrWrongOrganization = domain.NewError(domain.CategoryAuthorization, "organization_scope_forbidden")
	// ErrNoStation identifies an organization without a station.
	ErrNoStation = domain.NewError(domain.CategoryValidation, "organization_has_no_station")
	// ErrEmailConflict identifies an existing email address.
	ErrEmailConflict = domain.NewError(domain.CategoryConflict, "email_conflict")
	// ErrUsernameConflict identifies an existing username.
	ErrUsernameConflict = domain.NewError(domain.CategoryConflict, "username_conflict")
	// ErrInvalidToken identifies an unknown, consumed, or expired token.
	ErrInvalidToken = domain.NewError(domain.CategoryValidation, "INVALID_TOKEN")
	// ErrInvalidUsername identifies a username outside the allowed format.
	ErrInvalidUsername = domain.NewError(domain.CategoryValidation, "invalid_username")
	// ErrWeakPassword identifies a password below the minimum length.
	ErrWeakPassword = domain.NewError(domain.CategoryValidation, "weak_password")
	// ErrDependencyUnavailable identifies a missing identity dependency.
	ErrDependencyUnavailable = domain.NewError(domain.CategoryDependency, "dependency_unavailable")
	// ErrUserNotFound identifies a user outside the permitted organization scope.
	ErrUserNotFound = domain.NewError(domain.CategoryNotFound, "user_not_found")
)

// Service owns invitation and activation rules.
type Service struct {
	repository      Repository
	adminRepository AdminRepository
	mailer          mailer.Mailer
	clock           Clock
	baseURL         string
}

// NewService creates an identity service.
func NewService(repository Repository, adminRepository AdminRepository, sender mailer.Mailer, clock Clock, publicBaseURL string) *Service {
	return &Service{repository: repository, adminRepository: adminRepository, mailer: sender, clock: clock, baseURL: strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")}
}

// Invite creates an invitation and sends its activation message.
func (s *Service) Invite(ctx context.Context, request InvitationRequest) error {
	if s == nil || s.repository == nil || s.mailer == nil || s.clock == nil {
		return ErrDependencyUnavailable
	}
	if !validInvitationRequest(request) || strings.TrimSpace(s.baseURL) == "" {
		return ErrInvalidRequest
	}
	if request.ActorRole != "Owner" && request.ActorRole != "Superadmin" {
		return ErrForbidden
	}
	if request.ActorRole == "Owner" {
		if request.TargetOrgID != request.ActorOrgID {
			return ErrWrongOrganization
		}
		if request.Role == "Owner" || request.Role == "Superadmin" {
			return ErrForbidden
		}
	} else if request.TargetOrgID == uuid.Nil {
		return ErrInvalidRequest
	}
	if request.Role != "Operator" && request.Role != "Supervisor" && request.Role != "Station Admin" && request.Role != "Owner" {
		return ErrInvalidRequest
	}
	if request.ActorRole == "Owner" && request.Role == "Owner" {
		return ErrForbidden
	}
	rawToken, err := newToken()
	if err != nil {
		return fmt.Errorf("create invitation token: %w", err)
	}
	digest := sha256.Sum256([]byte(rawToken))
	now := s.clock.Now().UTC().Truncate(time.Microsecond)
	_, err = s.repository.CreateInvitation(ctx, request, uuid.New(), digest[:], now)
	if err != nil {
		return err
	}
	message := mailer.Message{
		To: request.Email, Subject: "Aktivasi akun PomKita",
		TextBody: s.baseURL + "/aktivasi?token=" + rawToken,
	}
	if err := s.mailer.Send(ctx, message); err != nil {
		return fmt.Errorf("send invitation email: %w", err)
	}
	return nil
}

// Accept activates an invitation and consumes its token in one transaction.
func (s *Service) Accept(ctx context.Context, request AcceptanceRequest) error {
	if s == nil || s.repository == nil || s.clock == nil {
		return ErrDependencyUnavailable
	}
	username := strings.ToLower(strings.TrimSpace(request.Username))
	if !validUsername(username) {
		return ErrInvalidUsername
	}
	if utf8.RuneCountInString(request.Password) < 8 {
		return ErrWeakPassword
	}
	if strings.TrimSpace(request.DisplayName) == "" || strings.TrimSpace(request.Token) == "" {
		return ErrInvalidRequest
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(request.Password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash invitation password: %w", err)
	}
	digest := sha256.Sum256([]byte(request.Token))
	request.Username = username
	request.DisplayName = strings.TrimSpace(request.DisplayName)
	return s.repository.AcceptInvitation(ctx, request, string(hash), digest[:], s.clock.Now().UTC().Truncate(time.Microsecond))
}

func validInvitationRequest(request InvitationRequest) bool {
	_, err := mail.ParseAddress(strings.TrimSpace(request.Email))
	return request.ActorID != uuid.Nil && request.ActorOrgID != uuid.Nil && request.TargetOrgID != uuid.Nil && request.StationID != uuid.Nil && strings.TrimSpace(request.DisplayName) != "" && err == nil && strings.TrimSpace(request.Email) != ""
}

func validUsername(username string) bool {
	return utf8.RuneCountInString(username) >= 3 && utf8.RuneCountInString(username) <= 32 && usernamePattern.MatchString(username)
}

func newToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}
