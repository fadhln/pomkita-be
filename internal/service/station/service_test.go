package station

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

type stationRepositoryStub struct {
	createdOrg     uuid.UUID
	updatedOrg     uuid.UUID
	readOrg        uuid.UUID
	updatedEnabled *bool
}

func (s *stationRepositoryStub) List(context.Context, uuid.UUID) ([]Station, error) {
	return []Station{}, nil
}

func (s *stationRepositoryStub) Read(_ context.Context, orgID, _ uuid.UUID) (Station, error) {
	s.readOrg = orgID
	return Station{}, nil
}

func (s *stationRepositoryStub) Create(_ context.Context, orgID uuid.UUID, _ CreateRequest, _ uuid.UUID, _ time.Time) (Station, error) {
	s.createdOrg = orgID
	return Station{}, nil
}

func (s *stationRepositoryStub) Update(_ context.Context, orgID, _ uuid.UUID, request UpdateRequest, _ uuid.UUID, _ time.Time) (Station, error) {
	s.updatedOrg = orgID
	s.updatedEnabled = request.Enabled
	return Station{Enabled: request.Enabled != nil && *request.Enabled}, nil
}

func (s *stationRepositoryStub) Disable(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, time.Time) error {
	return nil
}

type stationClock struct{}

func (stationClock) Now() time.Time { return time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC) }

func stationActor(role string, orgID uuid.UUID) Actor {
	return Actor{UserID: uuid.New(), OrgID: orgID, Roles: []string{role}}
}

func validStationCreate() CreateRequest {
	return CreateRequest{Name: "Main", Code: "JKT-01", Address: "Jakarta", Timezone: "Asia/Jakarta"}
}

func TestService_StationAuthorityAndScope(t *testing.T) {
	orgID, otherID := uuid.New(), uuid.New()
	repository := &stationRepositoryStub{}
	service := NewService(repository, stationClock{})

	if _, err := service.List(context.Background(), stationActor("Operator", orgID), orgID); err != ErrForbidden {
		t.Fatalf("wrong role error: got %v, want %v", err, ErrForbidden)
	}
	if _, err := service.Read(context.Background(), stationActor("Owner", orgID), otherID, uuid.New()); err != ErrStationNotFound {
		t.Fatalf("wrong organization error: got %v, want %v", err, ErrStationNotFound)
	}
	if _, err := service.List(context.Background(), stationActor("Superadmin", orgID), uuid.Nil); err != ErrOrganizationScopeRequired {
		t.Fatalf("missing superadmin organization error: got %v, want %v", err, ErrOrganizationScopeRequired)
	}
	if _, err := service.Create(context.Background(), stationActor("Owner", orgID), otherID, validStationCreate()); err != ErrStationNotFound {
		t.Fatalf("create wrong organization error: got %v, want %v", err, ErrStationNotFound)
	}
	if _, err := service.Update(context.Background(), stationActor("Owner", orgID), otherID, uuid.New(), UpdateRequest{Name: stringPointer("Updated")}); err != ErrStationNotFound {
		t.Fatalf("update wrong organization error: got %v, want %v", err, ErrStationNotFound)
	}
	if err := service.Disable(context.Background(), stationActor("Owner", orgID), otherID, uuid.New()); err != ErrStationNotFound {
		t.Fatalf("disable wrong organization error: got %v, want %v", err, ErrStationNotFound)
	}
}

func TestService_SuperadminCanReenableStation(t *testing.T) {
	orgID, stationID := uuid.New(), uuid.New()
	repository := &stationRepositoryStub{}
	service := NewService(repository, stationClock{})
	enabled := true

	updated, err := service.Update(context.Background(), stationActor("Superadmin", uuid.New()), orgID, stationID, UpdateRequest{Enabled: &enabled})

	if err != nil || !updated.Enabled || repository.updatedOrg != orgID || repository.updatedEnabled == nil || !*repository.updatedEnabled {
		t.Fatalf("re-enable station: result=%+v err=%v enabled=%v", updated, err, repository.updatedEnabled)
	}
}

func TestService_OwnerCannotChangeStationEnabledState(t *testing.T) {
	orgID, stationID := uuid.New(), uuid.New()
	repository := &stationRepositoryStub{}
	service := NewService(repository, stationClock{})
	enabled := true

	_, err := service.Update(context.Background(), stationActor("Owner", orgID), orgID, stationID, UpdateRequest{Enabled: &enabled})

	if err != ErrEnabledChangeForbidden {
		t.Fatalf("enabled change error: got %v, want %v", err, ErrEnabledChangeForbidden)
	}
	if repository.updatedOrg != uuid.Nil {
		t.Fatal("owner enabled change reached repository")
	}
}

func TestService_SuperadminUsesRequestedOrganization(t *testing.T) {
	requestedOrg := uuid.New()
	repository := &stationRepositoryStub{}
	service := NewService(repository, stationClock{})
	actor := stationActor("Superadmin", uuid.New())

	if _, err := service.Create(context.Background(), actor, requestedOrg, validStationCreate()); err != nil {
		t.Fatalf("create: %v", err)
	}
	if repository.createdOrg != requestedOrg {
		t.Fatalf("created organization: got %s, want %s", repository.createdOrg, requestedOrg)
	}
}

func TestService_StationCreateRequiresName(t *testing.T) {
	request := validStationCreate()
	request.Name = "  "
	service := NewService(&stationRepositoryStub{}, stationClock{})

	orgID := uuid.New()
	if _, err := service.Create(context.Background(), stationActor("Owner", orgID), orgID, request); err != ErrInvalidRequest {
		t.Fatalf("missing name error: got %v, want %v", err, ErrInvalidRequest)
	}
}

func stringPointer(value string) *string { return &value }
