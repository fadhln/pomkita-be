package organization

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeRepository struct {
	organizations []Organization
	createRequest *CreateRequest
	updateRequest *UpdateRequest
}

func (f *fakeRepository) List(context.Context, uuid.UUID, bool) ([]Organization, error) {
	return f.organizations, nil
}
func (f *fakeRepository) Read(context.Context, uuid.UUID) (Organization, error) {
	return Organization{}, nil
}
func (f *fakeRepository) Create(_ context.Context, request CreateRequest, _ uuid.UUID, _ time.Time) (Organization, error) {
	f.createRequest = &request
	return Organization{}, nil
}
func (f *fakeRepository) Update(_ context.Context, _ uuid.UUID, request UpdateRequest, _ uuid.UUID, _ time.Time) (Organization, error) {
	f.updateRequest = &request
	return Organization{}, nil
}
func (f *fakeRepository) Disable(context.Context, uuid.UUID, uuid.UUID, time.Time) error { return nil }

func actor(role string, orgID uuid.UUID) Actor {
	return Actor{UserID: uuid.New(), OrgID: orgID, Roles: []string{role}}
}

func validCreateRequest() CreateRequest {
	return CreateRequest{Name: "Pom Org", LegalName: "Pom Org PT", Address: "Jakarta", ContactEmail: "admin@example.test", Timezone: "Asia/Jakarta", FirstStation: &FirstStationRequest{Name: "Main", Timezone: "Asia/Jakarta"}}
}

func TestService_OrganizationAuthorityAndVisibility(t *testing.T) {
	orgID, otherID := uuid.New(), uuid.New()
	tests := []struct {
		name   string
		actor  Actor
		target uuid.UUID
		err    error
	}{
		{"wrong role", actor("Operator", orgID), orgID, ErrForbidden},
		{"owner wrong organization", actor("Owner", orgID), otherID, ErrOrganizationNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := NewService(&fakeRepository{}, fixedClock{})
			if _, err := service.Read(context.Background(), test.actor, test.target); err != test.err {
				t.Fatalf("error: got %v, want %v", err, test.err)
			}
		})
	}
}

func TestService_OwnerCannotChangeEnabled(t *testing.T) {
	enabled := false
	request := UpdateRequest{Enabled: &enabled}
	orgID := uuid.New()
	service := NewService(&fakeRepository{}, fixedClock{})
	if _, err := service.Update(context.Background(), actor("Owner", orgID), orgID, request); err != ErrEnabledChangeForbidden {
		t.Fatalf("error: got %v, want %v", err, ErrEnabledChangeForbidden)
	}
}

func TestService_CreateRequiresFirstStation(t *testing.T) {
	request := validCreateRequest()
	request.FirstStation = nil
	service := NewService(&fakeRepository{}, fixedClock{})
	if _, err := service.Create(context.Background(), actor("Superadmin", uuid.Nil), request); err != ErrFirstStationRequired {
		t.Fatalf("error: got %v, want %v", err, ErrFirstStationRequired)
	}
}

type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC) }
