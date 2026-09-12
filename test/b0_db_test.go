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
	if _, err := admin.Exec(ctx, readMigration(t, root, "000007_b1_submit.down.sql")); err != nil {
		t.Fatalf("reset B1 submit: %v", err)
	}
	if _, err := admin.Exec(ctx, readMigration(t, root, "000006_b1_drafts.down.sql")); err != nil {
		t.Fatalf("reset B1 drafts: %v", err)
	}
	if _, err := admin.Exec(ctx, readMigration(t, root, "000005_b1_shifts.down.sql")); err != nil {
		t.Fatalf("reset B1 shifts: %v", err)
	}
	if _, err := admin.Exec(ctx, readMigration(t, root, "000005_b1_catalog.down.sql")); err != nil {
		t.Fatalf("reset B1 catalog: %v", err)
	}
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
		values ('key_1', 'app.jwt_secret.key_1', 'active', clock_timestamp(), $1)
	`, keyExpiry); err != nil {
		t.Fatalf("seed signing key: %v", err)
	}
	orgID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	stationID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	userID := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`insert into organizations (org_id, name) values ($1, 'Test Org')`, []any{orgID}},
		{`insert into stations (org_id, station_id, timezone) values ($1, $2, 'Asia/Jakarta')`, []any{orgID, stationID}},
		{`insert into users (user_id, org_id, display_name) values ($1, $2, 'Test User')`, []any{userID, orgID}},
		{`insert into user_station_roles (org_id, station_id, user_id, role) values ($1, $2, $3, 'Owner')`, []any{orgID, stationID, userID}},
	} {
		if _, err := admin.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed context user: %v", err)
		}
	}

	database, err := appdb.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open database pool: %v", err)
	}
	defer database.Close()
	store := database.JWTStore(map[string]string{"app.jwt_secret.key_1": "test-secret"})
	if err := database.Ping(ctx); err != nil {
		t.Fatalf("ping database: %v", err)
	}
	current, err := database.MigrationsCurrent(ctx, 7)
	if err != nil || !current {
		t.Fatalf("migration state: current=%t error=%v", current, err)
	}
	transaction, err := database.Begin(ctx)
	if err != nil {
		t.Fatalf("begin context transaction: %v", err)
	}
	if err := database.SetRequestContext(ctx, transaction, ""); err == nil {
		_ = transaction.Rollback(ctx)
		t.Fatal("empty token was accepted by database context call")
	}
	if err := transaction.Rollback(ctx); err != nil {
		t.Fatalf("rollback context transaction: %v", err)
	}
	database.SetJWTSecrets(map[string]string{"app.jwt_secret.key_1": "test-secret"})
	service := appjwt.NewService(store, appjwt.Config{Issuer: "pomkita", Audience: "spbu-recon"})
	token, claims, err := service.Issue(ctx, userID)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	transaction, err = database.Begin(ctx)
	if err != nil {
		t.Fatalf("begin valid context transaction: %v", err)
	}
	if err := database.SetRequestContext(ctx, transaction, token); err != nil {
		_ = transaction.Rollback(ctx)
		t.Fatalf("set valid request context: %v", err)
	}
	var contextValid string
	if err := transaction.QueryRow(ctx, `select current_setting('app.context_valid', true)`).Scan(&contextValid); err != nil {
		t.Fatalf("read valid request context: %v", err)
	}
	if contextValid != "true" {
		t.Fatalf("context_valid: got %q, want true", contextValid)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatalf("commit valid context transaction: %v", err)
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

func TestB0DatabaseJWTStoreRotatesPreviousKeyAtExpiryCutoff(t *testing.T) {
	ctx := context.Background()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://pomkita:pomkita_dev@127.0.0.1:5432/pomkita?sslmode=disable"
	}
	admin := openB0Connection(t)
	defer admin.Close(ctx)
	resetB0Foundation(t, admin, true)

	now := time.Now().UTC().Truncate(time.Second)
	cutoff := now.Add(5 * time.Minute)
	jti := uuid.MustParse("44444444-4444-4444-8444-444444444444")
	userID := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	if _, err := admin.Exec(ctx, `
		insert into jwt_keys (kid, secret_ref, status, activated_at, max_token_expiry)
		values ('old_key', 'OLD_KEY', 'previous', $1, $2)
	`, now.Add(-time.Hour), cutoff); err != nil {
		t.Fatalf("seed previous key: %v", err)
	}
	if _, err := admin.Exec(ctx, `
		insert into sessions (jti, kid, issued_at, expires_at) values ($1, 'old_key', $2, $3)
	`, jti, now.Add(-time.Minute), cutoff); err != nil {
		t.Fatalf("seed previous session: %v", err)
	}

	database, err := appdb.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open database pool: %v", err)
	}
	defer database.Close()
	store := database.JWTStore(map[string]string{"OLD_KEY": "old-secret"})
	clock := now
	service := appjwt.NewService(store, appjwt.Config{Issuer: "pomkita", Audience: "spbu-recon", Now: func() time.Time { return clock }})
	token := makeToken(t, "old-secret", map[string]any{"alg": "HS256", "kid": "old_key"}, map[string]any{
		"iss": "pomkita", "sub": userID.String(), "jti": jti.String(),
		"iat": now.Add(-time.Minute).Unix(), "exp": cutoff.Unix(), "aud": "spbu-recon",
	})
	if _, err := service.Verify(ctx, token); err != nil {
		t.Fatalf("previous key rejected before cutoff: %v", err)
	}
	clock = cutoff.Add(time.Nanosecond)
	if _, err := service.Verify(ctx, token); err != appjwt.ErrRetiredKey {
		t.Fatalf("previous key after cutoff: got %v, want %v", err, appjwt.ErrRetiredKey)
	}
}
