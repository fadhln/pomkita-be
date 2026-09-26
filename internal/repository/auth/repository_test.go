package auth

import (
	"context"
	"testing"
	"time"

	appjwt "github.com/fadhln/pomkita-be/internal/jwt"
	"github.com/fadhln/pomkita-be/internal/repository/testsupport"
	"github.com/google/uuid"
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
	if err := repository.SetActiveContext(ctx, claims.JTI, orgID, stationID, now); err != nil {
		t.Fatalf("set session active context: %v", err)
	}
	view, err := repository.ReadSession(ctx, claims.JTI, userID)
	if err != nil {
		t.Fatalf("read session view: %v", err)
	}
	if view.UserID != userID || view.Username != "test-user" || view.OrgID != orgID || len(view.Roles) != 1 || view.Roles[0] != "Supervisor" || len(view.StationIDs) != 1 || view.StationIDs[0] != stationID {
		t.Fatalf("session view: got %+v", view)
	}
	if view.ActiveContext == nil || view.ActiveContext.OrgID != orgID || view.ActiveContext.StationID != stationID {
		t.Fatalf("active context: got %+v", view.ActiveContext)
	}
	var preference struct {
		OrgID     uuid.UUID `gorm:"column:preferred_org_id"`
		StationID uuid.UUID `gorm:"column:preferred_station_id"`
	}
	if err := store.DB.Table("users").Select("preferred_org_id, preferred_station_id").Where("user_id = ?", userID).Take(&preference).Error; err != nil {
		t.Fatalf("read saved context preference: %v", err)
	}
	if preference.OrgID != orgID || preference.StationID != stationID {
		t.Fatalf("saved context preference: got org=%s station=%s", preference.OrgID, preference.StationID)
	}
	_, otherClaims, err := tokens.Issue(ctx, userID)
	if err != nil {
		t.Fatalf("issue second session: %v", err)
	}
	otherView, err := repository.ReadSession(ctx, otherClaims.JTI, userID)
	if err != nil || otherView.ActiveContext != nil {
		t.Fatalf("second session context: view=%+v err=%v", otherView.ActiveContext, err)
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

func TestAuthRepository_FindsDisabledTargetsForActiveContext(t *testing.T) {
	ctx := context.Background()
	store, cleanup := newAuthTestStore(t, ctx)
	defer cleanup()
	now := time.Now().UTC()
	orgID, stationID := uuid.New(), uuid.New()
	if err := store.DB.Table("organizations").Create(map[string]any{"org_id": orgID, "name": "Disabled Org", "enabled": false, "created_at": now}).Error; err != nil {
		t.Fatalf("create disabled organization: %v", err)
	}
	if err := store.DB.Table("stations").Create(map[string]any{"org_id": orgID, "station_id": stationID, "name": "Disabled Station", "enabled": false, "timezone": "UTC", "created_at": now}).Error; err != nil {
		t.Fatalf("create disabled station: %v", err)
	}
	repository := NewAuthRepository(store, nil)
	orgExists, err := repository.OrganizationExists(ctx, orgID)
	if err != nil || !orgExists {
		t.Fatalf("disabled organization exists: got %t, err=%v", orgExists, err)
	}
	stationExists, err := repository.StationInOrganization(ctx, orgID, stationID)
	if err != nil || !stationExists {
		t.Fatalf("disabled station belongs to organization: got %t, err=%v", stationExists, err)
	}
}

var newAuthTestStore = testsupport.NewStore
