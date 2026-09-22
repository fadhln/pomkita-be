package identity

import (
	"context"
	"testing"
	"time"

	"github.com/fadhln/pomkita-be/internal/platform/mailer"
	"github.com/google/uuid"
)

type fixedClock struct{ value time.Time }

func (c fixedClock) Now() time.Time { return c.value }

type repositoryStub struct {
	userID    uuid.UUID
	err       error
	acceptErr error
	invite    InvitationRequest
	accept    AcceptanceRequest
}

func (r *repositoryStub) CreateInvitation(_ context.Context, request InvitationRequest, _ uuid.UUID, _ []byte, _ time.Time) (uuid.UUID, error) {
	r.invite = request
	return r.userID, r.err
}

func (r *repositoryStub) AcceptInvitation(_ context.Context, request AcceptanceRequest, _ string, _ []byte, _ time.Time) error {
	r.accept = request
	return r.acceptErr
}

type mailerStub struct {
	message mailer.Message
	err     error
}

func (m *mailerStub) Send(_ context.Context, message mailer.Message) error {
	m.message = message
	return m.err
}

func validInvite(role string) InvitationRequest {
	orgID, stationID, actorID := uuid.New(), uuid.New(), uuid.New()
	return InvitationRequest{ActorID: actorID, ActorOrgID: orgID, ActorRole: "Owner", TargetOrgID: orgID, StationID: stationID, Email: "new@example.test", DisplayName: "New User", Role: role}
}

func TestService_Invite_OwnerCannotCreateOwner(t *testing.T) {
	service := NewService(&repositoryStub{userID: uuid.New()}, nil, &mailerStub{}, fixedClock{value: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)}, "https://app.example.test")
	if err := service.Invite(context.Background(), validInvite("Owner")); err != ErrForbidden {
		t.Fatalf("error: got %v, want %v", err, ErrForbidden)
	}
}

func TestService_Invite_SuperadminRequiresTargetOrganization(t *testing.T) {
	request := validInvite("Owner")
	request.ActorRole = "Superadmin"
	request.TargetOrgID = uuid.Nil
	service := NewService(&repositoryStub{userID: uuid.New()}, nil, &mailerStub{}, fixedClock{value: time.Now()}, "https://app.example.test")
	if err := service.Invite(context.Background(), request); err != ErrInvalidRequest {
		t.Fatalf("error: got %v, want %v", err, ErrInvalidRequest)
	}
}

func TestService_Invite_RejectsWrongRoleAndOrganization(t *testing.T) {
	request := validInvite("Operator")
	request.ActorRole = "Supervisor"
	service := NewService(&repositoryStub{userID: uuid.New()}, nil, &mailerStub{}, fixedClock{value: time.Now()}, "https://app.example.test")
	if err := service.Invite(context.Background(), request); err != ErrForbidden {
		t.Fatalf("wrong role error: got %v, want %v", err, ErrForbidden)
	}
	request.ActorRole = "Owner"
	request.TargetOrgID = uuid.New()
	if err := service.Invite(context.Background(), request); err != ErrWrongOrganization {
		t.Fatalf("wrong organization error: got %v, want %v", err, ErrWrongOrganization)
	}
}

func TestService_Accept_RejectsInvalidUsernameAndWeakPassword(t *testing.T) {
	service := NewService(&repositoryStub{}, nil, nil, fixedClock{value: time.Now()}, "https://app.example.test")
	request := AcceptanceRequest{Token: "token", Username: "bad name", Password: "strong-password", DisplayName: "User"}
	if err := service.Accept(context.Background(), request); err != ErrInvalidUsername {
		t.Fatalf("username error: got %v, want %v", err, ErrInvalidUsername)
	}
	request.Username = "valid-user"
	request.Password = "short"
	if err := service.Accept(context.Background(), request); err != ErrWeakPassword {
		t.Fatalf("password error: got %v, want %v", err, ErrWeakPassword)
	}
}

func TestService_Invite_SendsActivationLinkWithoutPersistingRawToken(t *testing.T) {
	repository := &repositoryStub{userID: uuid.New()}
	sender := &mailerStub{}
	service := NewService(repository, nil, sender, fixedClock{value: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)}, "https://app.example.test")
	if err := service.Invite(context.Background(), validInvite("Operator")); err != nil {
		t.Fatalf("invite: %v", err)
	}
	if sender.message.To != "new@example.test" || len(sender.message.TextBody) <= len("https://app.example.test/aktivasi?token=") {
		t.Fatalf("activation message: %+v", sender.message)
	}
}
