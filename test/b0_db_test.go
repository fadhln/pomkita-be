package test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	appdb "github.com/pomkita/pomkita-be/internal/db"
	appjwt "github.com/pomkita/pomkita-be/internal/jwt"
)

func TestB0DatabaseJWTStorePersistsAndRevokesSessions(t *testing.T) {
	ctx := context.Background()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://pomkita:pomkita_dev@127.0.0.1:5432/pomkita?sslmode=disable"
	}
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to database: %v", err)
	}
	defer admin.Close(ctx)
	root := repositoryRoot(t)
	if _, err := admin.Exec(ctx, readMigration(t, root, "000002_b0_request_context.down.sql")); err != nil {
		t.Fatalf("reset request context: %v", err)
	}
	if _, err := admin.Exec(ctx, readMigration(t, root, "000001_b0_foundation.down.sql")); err != nil {
		t.Fatalf("reset foundation: %v", err)
	}
	if _, err := admin.Exec(ctx, `drop table if exists schema_migrations`); err != nil {
		t.Fatalf("remove migration metadata: %v", err)
	}

	migrator, err := appdb.NewMigrator(dsn, filepath.Join(root, "migrations"))
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	if err := migrator.Up(ctx); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	defer migrator.Down(ctx)

	keyExpiry := time.Now().UTC().Add(15 * time.Minute).Truncate(time.Microsecond)
	if _, err := admin.Exec(ctx, `
		insert into jwt_keys (kid, secret_ref, status, activated_at, max_token_expiry)
		values ('key_1', 'JWT_SECRET_KEY_1', 'active', clock_timestamp(), $1)
	`, keyExpiry); err != nil {
		t.Fatalf("seed signing key: %v", err)
	}

	database, err := appdb.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open database pool: %v", err)
	}
	defer database.Close()
	store := database.JWTStore(map[string]string{"JWT_SECRET_KEY_1": "test-secret"})
	service := appjwt.NewService(store, appjwt.Config{Issuer: "pomkita", Audience: "spbu-recon"})
	userID := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	token, claims, err := service.Issue(ctx, userID)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	if _, err := service.Verify(ctx, token); err != nil {
		t.Fatalf("verify issued token: %v", err)
	}
	if err := service.Logout(ctx, claims.JTI); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, err := service.Verify(ctx, token); err != appjwt.ErrRevokedJTI {
		t.Fatalf("verify logged out token: got %v, want %v", err, appjwt.ErrRevokedJTI)
	}
}
