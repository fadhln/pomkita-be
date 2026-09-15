package identity

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pomkita/pomkita-be/internal/repository/store"
	"github.com/pomkita/pomkita-be/internal/repository/testsupport"
	appidentity "github.com/pomkita/pomkita-be/internal/service/identity"
)

func TestIdentityRepository_AdminRolesAndHistoryUseOneAuditChain(t *testing.T) {
	ctx := context.Background()
	database, cleanup := testsupport.NewStore(t, ctx)
	defer cleanup()
	now := time.Date(2026, 9, 15, 10, 0, 0, 123456000, time.UTC)
	orgID, stationID := uuid.New(), uuid.New()
	actorID, targetID, otherID := uuid.New(), uuid.New(), uuid.New()
	if err := database.DB.Create(&store.OrganizationModel{OrgID: orgID, Name: "Test Org", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create organization: %v", err)
	}
	if err := database.DB.Create(&store.StationModel{OrgID: orgID, StationID: stationID, Name: "Main", Timezone: "UTC", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	for id, name := range map[uuid.UUID]string{actorID: "Owner", targetID: "Target", otherID: "Other"} {
		if err := database.DB.Create(&store.UserModel{UserID: id, OrgID: orgID, Email: name + "@example.test", Username: name, DisplayName: name, PasswordHash: "hash", Enabled: true, CreatedAt: now}).Error; err != nil {
			t.Fatalf("create user: %v", err)
		}
	}
	for id, role := range map[uuid.UUID]string{actorID: "Owner", targetID: "Operator", otherID: "Owner"} {
		if err := database.DB.Table("user_station_roles").Create(map[string]any{"org_id": orgID, "station_id": stationID, "user_id": id, "role": role}).Error; err != nil {
			t.Fatalf("create role: %v", err)
		}
	}
	repository := NewIdentityRepository(database)
	actor := appidentity.Actor{UserID: actorID, OrgID: orgID, Roles: []string{"Owner"}}

	view, err := repository.ReadUser(ctx, orgID, targetID)
	if err != nil || view.Username == nil || *view.Username != "Target" || len(view.Roles) != 1 || view.Roles[0].StationName != "Main" || len(view.Stations) != 1 {
		t.Fatalf("user view: view=%+v err=%v", view, err)
	}
	if _, err := repository.AssignRole(ctx, actor, targetID, appidentity.RoleRequest{StationID: stationID, Role: "Operator"}, uuid.New(), now); !errors.Is(err, appidentity.ErrRoleExists) {
		t.Fatalf("duplicate role: got %v, want %v", err, appidentity.ErrRoleExists)
	}
	if err := repository.RemoveRole(ctx, actor, targetID, appidentity.RoleRequest{StationID: stationID, Role: "Supervisor"}, uuid.New(), now); !errors.Is(err, appidentity.ErrRoleNotFound) {
		t.Fatalf("missing role: got %v, want %v", err, appidentity.ErrRoleNotFound)
	}
	roles, err := repository.AssignRole(ctx, actor, targetID, appidentity.RoleRequest{StationID: stationID, Role: "Supervisor"}, uuid.New(), now)
	if err != nil || len(roles) != 2 {
		t.Fatalf("assign role: roles=%+v err=%v", roles, err)
	}
	if err := repository.RemoveRole(ctx, actor, targetID, appidentity.RoleRequest{StationID: stationID, Role: "Supervisor"}, uuid.New(), now.Add(time.Microsecond)); err != nil {
		t.Fatalf("remove role: %v", err)
	}
	if _, err := repository.UpdateUser(ctx, actor, otherID, appidentity.UserUpdateRequest{DisplayName: "Blocked", Enabled: true}, uuid.New(), now); !errors.Is(err, appidentity.ErrProtectedUser) {
		t.Fatalf("protected user update: got %v, want %v", err, appidentity.ErrProtectedUser)
	}
	if err := repository.RemoveRole(ctx, actor, otherID, appidentity.RoleRequest{StationID: stationID, Role: "Owner"}, uuid.New(), now); !errors.Is(err, appidentity.ErrForbidden) {
		t.Fatalf("owner protected role removal: got %v, want %v", err, appidentity.ErrForbidden)
	}
	separationID := uuid.New()
	if err := database.DB.Create(&store.UserModel{UserID: separationID, OrgID: orgID, Email: "separation@example.test", Username: "separation", DisplayName: "Separation", PasswordHash: "hash", Enabled: true, CreatedAt: now}).Error; err != nil {
		t.Fatalf("create separation user: %v", err)
	}
	if err := database.DB.Table("user_station_roles").Create(map[string]any{"org_id": orgID, "station_id": stationID, "user_id": separationID, "role": "Supervisor"}).Error; err != nil {
		t.Fatalf("create requester role: %v", err)
	}
	if _, err := repository.AssignRole(ctx, actor, separationID, appidentity.RoleRequest{StationID: stationID, Role: "Station Admin"}, uuid.New(), now); !errors.Is(err, appidentity.ErrRoleSeparation) {
		t.Fatalf("separation rule: got %v, want %v", err, appidentity.ErrRoleSeparation)
	}
	digest := sha256.Sum256([]byte("administrator-reset-token"))
	if err := repository.IssuePasswordReset(ctx, actor, targetID, uuid.New(), digest[:], now, now.Add(time.Hour)); err != nil {
		t.Fatalf("issue password reset: %v", err)
	}
	var token store.AccountTokenModel
	if err := database.DB.Where("user_id = ? and purpose = ?", targetID, "password_reset").First(&token).Error; err != nil {
		t.Fatalf("read reset token: %v", err)
	}
	if string(token.TokenHash) == "administrator-reset-token" || token.CreatedBy == nil || *token.CreatedBy != actorID {
		t.Fatalf("reset token storage: %+v", token)
	}
	history, err := repository.RoleHistory(ctx, orgID, targetID)
	if err != nil || len(history) != 2 || history[0].Before || !history[0].After || !history[1].Before || history[1].After || history[0].Station != "Main" || history[0].Target != targetID || history[0].Actor != actorID || history[0].CreatedAt != "2026-09-15T10:00:00.123456Z" {
		t.Fatalf("role history: history=%+v err=%v", history, err)
	}

	superadminID := uuid.New()
	if err := database.DB.Create(&store.UserModel{UserID: superadminID, OrgID: orgID, Email: "super@example.test", Username: "super", DisplayName: "Super", PasswordHash: "hash", Enabled: true, CreatedAt: now}).Error; err != nil {
		t.Fatalf("create superadmin: %v", err)
	}
	if err := database.DB.Table("user_station_roles").Create(map[string]any{"org_id": orgID, "station_id": stationID, "user_id": superadminID, "role": "Superadmin"}).Error; err != nil {
		t.Fatalf("create superadmin role: %v", err)
	}
	otherOrgID, otherUserID := uuid.New(), uuid.New()
	if err := database.DB.Create(&store.OrganizationModel{OrgID: otherOrgID, Name: "Other Org", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create other organization: %v", err)
	}
	if err := database.DB.Create(&store.UserModel{UserID: otherUserID, OrgID: otherOrgID, Email: "other-org@example.test", Username: "other-org", DisplayName: "Other Org User", PasswordHash: "hash", Enabled: true, CreatedAt: now}).Error; err != nil {
		t.Fatalf("create other organization user: %v", err)
	}
	if _, err := repository.ReadUser(ctx, orgID, otherUserID); !errors.Is(err, appidentity.ErrUserNotFound) {
		t.Fatalf("out of scope user: got %v, want %v", err, appidentity.ErrUserNotFound)
	}
	if _, err := repository.ReadUser(ctx, uuid.Nil, otherUserID); err != nil {
		t.Fatalf("superadmin target read: %v", err)
	}
	superadmin := appidentity.Actor{UserID: superadminID, OrgID: orgID, Roles: []string{"Superadmin"}}
	if err := repository.RemoveRole(ctx, superadmin, otherID, appidentity.RoleRequest{StationID: stationID, Role: "Owner"}, uuid.New(), now); err != nil {
		t.Fatalf("remove second owner: %v", err)
	}
	if err := repository.RemoveRole(ctx, superadmin, actorID, appidentity.RoleRequest{StationID: stationID, Role: "Owner"}, uuid.New(), now); !errors.Is(err, appidentity.ErrLastOwner) {
		t.Fatalf("last owner: got %v, want %v", err, appidentity.ErrLastOwner)
	}
	noHistory, err := repository.RoleHistory(ctx, orgID, superadminID)
	if err != nil || len(noHistory) != 0 {
		t.Fatalf("empty role history: history=%+v err=%v", noHistory, err)
	}
}
