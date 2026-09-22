package account

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/fadhln/pomkita-be/internal/platform/mailer"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type accountClock struct{ now time.Time }

func (c accountClock) Now() time.Time { return c.now }

type accountRepositoryStub struct {
	err         error
	profile     Profile
	credentials Credentials
	resetTarget ResetTarget
	updated     UpdateRequest
	password    string
	resetHash   string
	resetExpiry time.Time
}

func (r *accountRepositoryStub) ReadProfile(context.Context, uuid.UUID) (Profile, error) {
	return r.profile, r.err
}

func (r *accountRepositoryStub) UpdateProfile(_ context.Context, _ uuid.UUID, request UpdateRequest, _ uuid.UUID, _ time.Time) (Profile, error) {
	r.updated = request
	return r.profile, r.err
}

func (r *accountRepositoryStub) Credentials(context.Context, uuid.UUID) (Credentials, error) {
	return r.credentials, r.err
}

func (r *accountRepositoryStub) ChangePassword(_ context.Context, _ uuid.UUID, hash string, _ uuid.UUID, _ uuid.UUID, _ time.Time) error {
	r.password = hash
	return r.err
}

func (r *accountRepositoryStub) FindResetTarget(context.Context, string) (ResetTarget, error) {
	return r.resetTarget, r.err
}

func (r *accountRepositoryStub) IssuePasswordReset(_ context.Context, _ uuid.UUID, _ uuid.UUID, hash []byte, _ time.Time, expiry time.Time) error {
	r.resetHash = string(hash)
	r.resetExpiry = expiry
	return r.err
}

func (r *accountRepositoryStub) ResetPassword(_ context.Context, _ []byte, hash string, _ uuid.UUID, _ time.Time) error {
	r.password = hash
	return r.err
}

type accountMailerStub struct{ message mailer.Message }

func (m *accountMailerStub) Send(_ context.Context, message mailer.Message) error {
	m.message = message
	return nil
}

func TestService_UpdateRejectsInvalidUsername(t *testing.T) {
	service := NewService(&accountRepositoryStub{}, accountClock{now: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)}, nil, "")
	username := "Bad Name"
	if _, err := service.Update(context.Background(), uuid.New(), uuid.New(), UpdateRequest{Username: &username}); err != ErrInvalidUsername {
		t.Fatalf("error: got %v, want %v", err, ErrInvalidUsername)
	}
}

func TestService_UpdateKeepsMissingFieldsUnchanged(t *testing.T) {
	displayName := "New Name"
	stub := &accountRepositoryStub{profile: Profile{Username: "old-user"}}
	service := NewService(stub, accountClock{now: time.Now()}, nil, "")
	if _, err := service.Update(context.Background(), uuid.New(), uuid.New(), UpdateRequest{DisplayName: &displayName}); err != nil {
		t.Fatal(err)
	}
	if stub.updated.Username != nil || stub.updated.DisplayName == nil || *stub.updated.DisplayName != displayName {
		t.Fatalf("update request: %+v", stub.updated)
	}
}

func TestService_ChangePasswordRejectsWrongCurrentAndWeakNewPassword(t *testing.T) {
	userID := uuid.New()
	stub := &accountRepositoryStub{credentials: Credentials{UserID: userID, PasswordHash: mustHash(t, "current-password"), Enabled: true}}
	service := NewService(stub, accountClock{now: time.Now()}, nil, "")
	if err := service.ChangePassword(context.Background(), userID, uuid.New(), PasswordRequest{CurrentPassword: "wrong-password", NewPassword: "strong-password"}); err != ErrCurrentPasswordIncorrect {
		t.Fatalf("current password error: got %v, want %v", err, ErrCurrentPasswordIncorrect)
	}
	if err := service.ChangePassword(context.Background(), userID, uuid.New(), PasswordRequest{CurrentPassword: "current-password", NewPassword: "short"}); err != ErrWeakPassword {
		t.Fatalf("new password error: got %v, want %v", err, ErrWeakPassword)
	}
}

func TestService_ForgotUnknownIdentifierReturnsWithoutIssuingToken(t *testing.T) {
	stub := &accountRepositoryStub{err: ErrUserNotFound}
	service := NewService(stub, accountClock{now: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)}, nil, "https://app.example.test")
	if err := service.Forgot(context.Background(), "unknown@example.test"); err != nil {
		t.Fatalf("forgot: %v", err)
	}
	if stub.resetHash != "" {
		t.Fatal("unknown identifier issued a reset token")
	}
}

func TestService_ForgotEnabledAccountUsesSixtyMinuteTokenAndSafeLink(t *testing.T) {
	now := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	stub := &accountRepositoryStub{resetTarget: ResetTarget{UserID: uuid.New(), OrgID: uuid.New(), Email: "user@example.test", Enabled: true}}
	sender := &accountMailerStub{}
	service := NewService(stub, accountClock{now: now}, sender, "https://app.example.test/")
	if err := service.Forgot(context.Background(), "user@example.test"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sender.message.TextBody, "https://app.example.test/reset-sandi?token=") || sender.message.To != "user@example.test" {
		t.Fatalf("message: %+v", sender.message)
	}
	if stub.resetExpiry != now.Add(60*time.Minute) || len(stub.resetHash) != 32 {
		t.Fatalf("reset token state: expiry=%v hash length=%d", stub.resetExpiry, len(stub.resetHash))
	}
	if strings.Contains(sender.message.TextBody, stub.resetHash) {
		t.Fatal("email contains stored token digest")
	}
}

func TestService_ForgotDisabledAccountDoesNotIssueOrSend(t *testing.T) {
	stub := &accountRepositoryStub{resetTarget: ResetTarget{UserID: uuid.New(), Enabled: false}}
	sender := &accountMailerStub{}
	service := NewService(stub, accountClock{now: time.Now()}, sender, "https://app.example.test")
	if err := service.Forgot(context.Background(), "disabled@example.test"); err != nil {
		t.Fatal(err)
	}
	if stub.resetHash != "" || sender.message.To != "" {
		t.Fatal("disabled account caused reset side effects")
	}
}

func TestService_ResetRejectsWeakPasswordBeforeTokenUse(t *testing.T) {
	service := NewService(&accountRepositoryStub{}, accountClock{now: time.Now()}, nil, "https://app.example.test")
	if err := service.Reset(context.Background(), ResetRequest{Token: "token", Password: "short"}); err != ErrWeakPassword {
		t.Fatalf("error: got %v, want %v", err, ErrWeakPassword)
	}
}

func mustHash(t *testing.T, password string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return string(hash)
}
