package identity

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type adminRepositoryStub struct {
	assigned bool
	removed  bool
	reset    bool
}

func (s *adminRepositoryStub) ListUsers(context.Context, uuid.UUID) ([]UserView, error) {
	return []UserView{}, nil
}

func (s *adminRepositoryStub) ReadUser(context.Context, uuid.UUID, uuid.UUID) (UserView, error) {
	return UserView{}, nil
}

func (s *adminRepositoryStub) UpdateUser(context.Context, Actor, uuid.UUID, UserUpdateRequest, uuid.UUID, time.Time) (UserView, error) {
	return UserView{}, nil
}

func (s *adminRepositoryStub) AssignRole(context.Context, Actor, uuid.UUID, RoleRequest, uuid.UUID, time.Time) ([]RoleView, error) {
	s.assigned = true
	return []RoleView{}, nil
}

func (s *adminRepositoryStub) RemoveRole(context.Context, Actor, uuid.UUID, RoleRequest, uuid.UUID, time.Time) error {
	s.removed = true
	return nil
}

func (s *adminRepositoryStub) RoleHistory(context.Context, uuid.UUID, uuid.UUID) ([]RoleHistoryEvent, error) {
	return []RoleHistoryEvent{}, nil
}

func (s *adminRepositoryStub) IssuePasswordReset(context.Context, Actor, uuid.UUID, uuid.UUID, []byte, time.Time, time.Time) error {
	s.reset = true
	return nil
}

func TestAdminService_RejectsWrongRoleBeforeRepository(t *testing.T) {
	repository := &adminRepositoryStub{}
	service := NewService(nil, repository, nil, fixedClock{value: time.Now()}, "https://app.example.test")
	actor := Actor{UserID: uuid.New(), OrgID: uuid.New(), Roles: []string{"Supervisor"}}

	_, err := service.AssignRole(context.Background(), actor, uuid.New(), RoleRequest{StationID: uuid.New(), Role: "Operator"})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("wrong role error: got %v, want %v", err, ErrForbidden)
	}
	if repository.assigned {
		t.Fatal("repository was called for a forbidden actor")
	}
}

func TestAdminService_OwnerCannotAssignOwnerOrChangeOwnRole(t *testing.T) {
	repository := &adminRepositoryStub{}
	service := NewService(nil, repository, nil, fixedClock{value: time.Now()}, "https://app.example.test")
	actor := Actor{UserID: uuid.New(), OrgID: uuid.New(), Roles: []string{"Owner"}}

	if _, err := service.AssignRole(context.Background(), actor, uuid.New(), RoleRequest{StationID: uuid.New(), Role: "Owner"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("owner assignment error: got %v, want %v", err, ErrForbidden)
	}
	if _, err := service.AssignRole(context.Background(), actor, actor.UserID, RoleRequest{StationID: uuid.New(), Role: "Operator"}); !errors.Is(err, ErrSelfRoleChange) {
		t.Fatalf("self role error: got %v, want %v", err, ErrSelfRoleChange)
	}
	if repository.assigned {
		t.Fatal("repository was called for a forbidden owner operation")
	}
}

func TestAdminService_OwnerCannotListAnotherOrganization(t *testing.T) {
	service := NewService(nil, &adminRepositoryStub{}, nil, fixedClock{value: time.Now()}, "https://app.example.test")
	actor := Actor{UserID: uuid.New(), OrgID: uuid.New(), Roles: []string{"Owner"}}
	if _, err := service.ListUsers(context.Background(), actor, uuid.New()); !errors.Is(err, ErrWrongOrganization) {
		t.Fatalf("wrong organization error: got %v, want %v", err, ErrWrongOrganization)
	}
}

func TestAdminService_IssuesOneTimeResetLinkForOwnerTarget(t *testing.T) {
	repository := &adminRepositoryStub{}
	clock := fixedClock{value: time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)}
	service := NewService(nil, repository, nil, clock, "https://app.example.test/")
	actor := Actor{UserID: uuid.New(), OrgID: uuid.New(), Roles: []string{"Owner"}}

	result, err := service.IssuePasswordReset(context.Background(), actor, uuid.New())
	if err != nil || !strings.HasPrefix(result.Link, "https://app.example.test/reset-sandi?token=") || len(result.Link) != len("https://app.example.test/reset-sandi?token=")+43 || !repository.reset {
		t.Fatalf("reset result: result=%+v err=%v reset=%t", result, err, repository.reset)
	}
}
