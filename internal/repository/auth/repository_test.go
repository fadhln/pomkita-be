package auth

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	appjwt "github.com/pomkita/pomkita-be/internal/jwt"
	"github.com/pomkita/pomkita-be/internal/repository/testsupport"
)

func TestAuthRepository_LoadsUserScopeAndPersistsJWTSession(t *testing.T) {
	ctx := context.Background()
	store, cleanup := newAuthTestStore(t, ctx)
	defer cleanup()

	orgID := uuid.New()
	stationID := uuid.New()
	userID := uuid.New()
	now := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	if err := store.DB.Create(&OrganizationModel{OrgID: orgID, Name: "Test Org", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create organization: %v", err)
	}
	if err := store.DB.Create(&StationModel{Name: "Station", OrgID: orgID, StationID: stationID, Timezone: "Asia/Jakarta", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := store.DB.Create(&UserModel{UserID: userID, OrgID: orgID, DisplayName: "Test User", Email: "User@Example.com", Username: "test-user", PasswordHash: "hash", Enabled: true, CreatedAt: now}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := store.DB.Table("user_station_roles").Create(map[string]any{
		"org_id": orgID, "station_id": stationID, "user_id": userID, "role": "Supervisor",
	}).Error; err != nil {
		t.Fatalf("create user role: %v", err)
	}
	if err := store.DB.Create(&JWTKeyModel{KID: "key-1", SecretRef: "key-ref", Status: "active", ActivatedAt: now.Add(-time.Hour), MaxTokenExpiry: now.Add(time.Hour)}).Error; err != nil {
		t.Fatalf("create JWT key: %v", err)
	}

	repository := NewAuthRepository(store, map[string]string{"key-ref": "test-secret"})
	user, err := repository.FindUserByEmail(ctx, "user@example.com")
	if err != nil {
		t.Fatalf("find user: %v", err)
	}
	if user.UserID != userID || user.OrgID != orgID || user.Email != "User@Example.com" {
		t.Fatalf("user: got %+v", user)
	}
	byUsername, err := repository.FindUserByUsername(ctx, "TEST-USER")
	if err != nil || byUsername.UserID != userID || byUsername.Username != "test-user" {
		t.Fatalf("username lookup: user=%+v err=%v", byUsername, err)
	}

	tokens := appjwt.NewService(repository, appjwt.Config{Issuer: "test", Audience: "test", Now: func() time.Time { return now }})
	_, claims, err := tokens.Issue(ctx, userID)
	if err != nil {
		t.Fatalf("issue session: %v", err)
	}
	view, err := repository.ReadSession(ctx, claims.JTI, userID)
	if err != nil {
		t.Fatalf("read session view: %v", err)
	}
	if view.UserID != userID || view.Username != "test-user" || view.OrgID != orgID || len(view.Roles) != 1 || view.Roles[0] != "Supervisor" || len(view.StationIDs) != 1 || view.StationIDs[0] != stationID {
		t.Fatalf("session view: got %+v", view)
	}
	if err := store.DB.Table("user_station_roles").Create(map[string]any{
		"org_id": orgID, "station_id": stationID, "user_id": userID, "role": "Owner",
	}).Error; err != nil {
		t.Fatalf("change user role: %v", err)
	}
	nextRequest, err := repository.ReadSession(ctx, claims.JTI, userID)
	if err != nil || len(nextRequest.Roles) != 2 || nextRequest.Roles[0] != "Owner" || nextRequest.Roles[1] != "Supervisor" {
		t.Fatalf("next request session roles: view=%+v err=%v", nextRequest, err)
	}
	if err := tokens.Logout(ctx, claims.JTI); err != nil {
		t.Fatalf("logout: %v", err)
	}
	session, err := repository.Session(ctx, claims.JTI)
	if err != nil {
		t.Fatalf("read revoked session: %v", err)
	}
	if session.RevokedAt == nil {
		t.Fatal("session was not revoked")
	}
}

var newAuthTestStore = testsupport.NewStore
