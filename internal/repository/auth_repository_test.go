package repository

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	appjwt "github.com/pomkita/pomkita-be/internal/jwt"
	cleanmigrations "github.com/pomkita/pomkita-be/internal/platform/migrations"
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
	if err := store.DB.Create(&StationModel{OrgID: orgID, StationID: stationID, Timezone: "Asia/Jakarta", CreatedAt: now}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := store.DB.Create(&UserModel{UserID: userID, OrgID: orgID, DisplayName: "Test User", Email: "User@Example.com", PasswordHash: "hash", Enabled: true, CreatedAt: now}).Error; err != nil {
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

	tokens := appjwt.NewService(repository, appjwt.Config{Issuer: "test", Audience: "test", Now: func() time.Time { return now }})
	_, claims, err := tokens.Issue(ctx, userID)
	if err != nil {
		t.Fatalf("issue session: %v", err)
	}
	view, err := repository.ReadSession(ctx, claims.JTI, userID)
	if err != nil {
		t.Fatalf("read session view: %v", err)
	}
	if view.UserID != userID || view.OrgID != orgID || len(view.Roles) != 1 || view.Roles[0] != "Supervisor" || len(view.StationIDs) != 1 || view.StationIDs[0] != stationID {
		t.Fatalf("session view: got %+v", view)
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

func newAuthTestStore(t *testing.T, ctx context.Context) (*Store, func()) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://pomkita:pomkita_dev@127.0.0.1:5432/pomkita?sslmode=disable"
	}
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to database: %v", err)
	}
	schema := "phase2_auth_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(ctx, `create schema `+schema); err != nil {
		admin.Close(ctx)
		t.Fatalf("create isolated schema: %v", err)
	}

	scopedURL, err := url.Parse(dsn)
	if err != nil {
		admin.Close(ctx)
		t.Fatalf("parse database URL: %v", err)
	}
	query := scopedURL.Query()
	query.Set("options", "-c search_path="+schema)
	scopedURL.RawQuery = query.Encode()
	migrationsPath, err := filepath.Abs(filepath.Join(repositoryRoot(t), "migrations"))
	if err != nil {
		admin.Close(ctx)
		t.Fatalf("resolve migrations: %v", err)
	}
	runner, err := cleanmigrations.New(scopedURL.String(), migrationsPath)
	if err != nil {
		admin.Close(ctx)
		t.Fatalf("create migration runner: %v", err)
	}
	if err := runner.Up(ctx); err != nil {
		admin.Close(ctx)
		t.Fatalf("apply migrations: %v", err)
	}
	store, err := Open(ctx, scopedURL.String())
	if err != nil {
		admin.Close(ctx)
		t.Fatalf("open GORM store: %v", err)
	}
	cleanup := func() {
		_ = store.Close()
		_, _ = admin.Exec(ctx, `drop schema if exists `+schema+` cascade`)
		_ = admin.Close(ctx)
	}
	return store, cleanup
}
