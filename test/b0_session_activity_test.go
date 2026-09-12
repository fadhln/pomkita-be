package test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	appdb "github.com/pomkita/pomkita-be/internal/db"
	appjwt "github.com/pomkita/pomkita-be/internal/jwt"
)

func TestB0SessionActivityMigrationUpAndDown(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	resetB0SessionActivity(t, conn)

	var dataType string
	var notNull bool
	if err := conn.QueryRow(ctx, `
		select a.atttypid::regtype::text, a.attnotnull
		from pg_attribute a
		join pg_class c on c.oid = a.attrelid
		where c.relname = 'sessions' and a.attname = 'last_active_at'
	`).Scan(&dataType, &notNull); err != nil {
		t.Fatalf("read last_active_at definition: %v", err)
	}
	if dataType != "timestamp with time zone" || !notNull {
		t.Fatalf("last_active_at definition: type=%q not_null=%t", dataType, notNull)
	}

	if _, err := conn.Exec(ctx, readMigration(t, repositoryRoot(t), "000003_b0_session_activity.down.sql")); err != nil {
		t.Fatalf("reverse session activity migration: %v", err)
	}
	var columnCount int
	if err := conn.QueryRow(ctx, `
		select count(*) from information_schema.columns
		where table_schema = 'public' and table_name = 'sessions' and column_name = 'last_active_at'
	`).Scan(&columnCount); err != nil {
		t.Fatalf("check reversed session activity column: %v", err)
	}
	if columnCount != 0 {
		t.Fatalf("last_active_at still exists after down migration")
	}
}

func TestB0SessionActivityFreshRequestUpdatesLastActiveInTransaction(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	resetB0SessionActivity(t, conn)

	const (
		orgID     = "11111111-1111-4111-8111-111111111111"
		stationID = "22222222-2222-4222-8222-222222222222"
		userID    = "33333333-3333-4333-8333-333333333333"
		jti       = "44444444-4444-4444-8444-444444444444"
	)
	now := time.Now().UTC().Truncate(time.Microsecond)
	seedB0SessionActivity(t, conn, now, orgID, stationID, userID, jti, now.Add(20*time.Minute))
	token := makeToken(t, "test-secret", map[string]any{
		"alg": "HS256", "kid": "key_1",
	}, map[string]any{
		"iss": "pomkita", "sub": userID, "jti": jti,
		"iat": now.Add(-time.Minute).Unix(), "exp": now.Add(10 * time.Minute).Unix(), "aud": "spbu-recon",
	})

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin request transaction: %v", err)
	}
	if _, err := tx.Exec(ctx, `set local role pomkita_app`); err != nil {
		t.Fatalf("set application role: %v", err)
	}
	if _, err := tx.Exec(ctx, `select fn_set_request_context($1)`, token); err != nil {
		t.Fatalf("fresh session was denied: %v", err)
	}
	var contextValid string
	if err := tx.QueryRow(ctx, `select current_setting('app.context_valid', true)`).Scan(&contextValid); err != nil {
		t.Fatalf("business context call: %v", err)
	}
	if contextValid != "true" {
		t.Fatalf("context_valid: got %q, want true", contextValid)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit request transaction: %v", err)
	}

	var lastActive time.Time
	if err := conn.QueryRow(ctx, `select last_active_at from sessions where jti = $1`, jti).Scan(&lastActive); err != nil {
		t.Fatalf("read updated session: %v", err)
	}
	if !lastActive.After(now.Add(-time.Second)) {
		t.Fatalf("last_active_at was not updated: got %s, request started %s", lastActive, now)
	}
}

func TestB0DatabaseJWTStoreReadsLastActiveAt(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	resetB0SessionActivity(t, conn)

	const (
		orgID     = "11111111-1111-4111-8111-111111111111"
		stationID = "22222222-2222-4222-8222-222222222222"
		userID    = "33333333-3333-4333-8333-333333333333"
		jti       = "44444444-4444-4444-8444-444444444444"
	)
	now := time.Now().UTC().Truncate(time.Microsecond)
	activity := now.Add(-2 * time.Minute)
	seedB0SessionActivity(t, conn, now, orgID, stationID, userID, jti, now.Add(20*time.Minute))
	if _, err := conn.Exec(ctx, `update sessions set last_active_at = $2 where jti = $1`, jti, activity); err != nil {
		t.Fatalf("set session activity: %v", err)
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://pomkita:pomkita_dev@127.0.0.1:5432/pomkita?sslmode=disable"
	}
	database, err := appdb.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open database pool: %v", err)
	}
	defer database.Close()
	session, err := database.JWTStore(map[string]string{"app.jwt_secret.key_1": "test-secret"}).Session(ctx, uuid.MustParse(jti))
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if !session.LastActiveAt.Equal(activity) {
		t.Fatalf("last_active_at: got %s, want %s", session.LastActiveAt, activity)
	}
}

func TestB0SessionActivitySlidingExpiryNeverPassesKeyMaximum(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	resetB0SessionActivity(t, conn)

	const (
		orgID     = "11111111-1111-4111-8111-111111111111"
		stationID = "22222222-2222-4222-8222-222222222222"
		userID    = "33333333-3333-4333-8333-333333333333"
		jti       = "44444444-4444-4444-8444-444444444444"
	)
	now := time.Now().UTC().Truncate(time.Microsecond)
	seedB0SessionActivity(t, conn, now, orgID, stationID, userID, jti, now.Add(5*time.Minute))
	token := makeToken(t, "test-secret", map[string]any{
		"alg": "HS256", "kid": "key_1",
	}, map[string]any{
		"iss": "pomkita", "sub": userID, "jti": jti,
		"iat": now.Add(-time.Minute).Unix(), "exp": now.Add(10 * time.Minute).Unix(), "aud": "spbu-recon",
	})

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin near-retirement request: %v", err)
	}
	if _, err := tx.Exec(ctx, `set local role pomkita_app`); err != nil {
		t.Fatalf("set application role: %v", err)
	}
	if _, err := tx.Exec(ctx, `select fn_set_request_context($1)`, token); err != nil {
		t.Fatalf("near-retirement session was denied: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit near-retirement request: %v", err)
	}

	var sessionExpiry, storedKeyExpiry time.Time
	if err := conn.QueryRow(ctx, `
		select s.expires_at, k.max_token_expiry
		from sessions s
		join jwt_keys k on k.kid = s.kid
		where s.jti = $1
	`, jti).Scan(&sessionExpiry, &storedKeyExpiry); err != nil {
		t.Fatalf("read capped session expiry: %v", err)
	}
	if sessionExpiry.After(storedKeyExpiry) || !sessionExpiry.Equal(storedKeyExpiry) {
		t.Fatalf("session expiry passed key maximum: session=%s key=%s", sessionExpiry, storedKeyExpiry)
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://pomkita:pomkita_dev@127.0.0.1:5432/pomkita?sslmode=disable"
	}
	database, err := appdb.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open database pool: %v", err)
	}
	defer database.Close()
	service := appjwt.NewService(database.JWTStore(map[string]string{"app.jwt_secret.key_1": "test-secret"}), appjwt.Config{
		Issuer: "pomkita", Audience: "spbu-recon", Now: func() time.Time { return now },
	})
	claims, err := service.Verify(ctx, token)
	if err != nil {
		t.Fatalf("verify capped session: %v", err)
	}
	if !claims.ExpiresAt.Equal(storedKeyExpiry) {
		t.Fatalf("reissued expiry passed key maximum: got %s, want %s", claims.ExpiresAt, storedKeyExpiry)
	}
}

func TestB0SessionActivityIdleSessionReturns28000AndNeverRevives(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	resetB0SessionActivity(t, conn)

	const (
		orgID     = "11111111-1111-4111-8111-111111111111"
		stationID = "22222222-2222-4222-8222-222222222222"
		userID    = "33333333-3333-4333-8333-333333333333"
		jti       = "44444444-4444-4444-8444-444444444444"
	)
	now := time.Now().UTC().Truncate(time.Microsecond)
	seedB0SessionActivity(t, conn, now, orgID, stationID, userID, jti, now.Add(20*time.Minute))
	if _, err := conn.Exec(ctx, `update sessions set last_active_at = $2 where jti = $1`, jti, now.Add(-16*time.Minute)); err != nil {
		t.Fatalf("back-date session activity: %v", err)
	}
	token := makeToken(t, "test-secret", map[string]any{
		"alg": "HS256", "kid": "key_1",
	}, map[string]any{
		"iss": "pomkita", "sub": userID, "jti": jti,
		"iat": now.Add(-time.Minute).Unix(), "exp": now.Add(10 * time.Minute).Unix(), "aud": "spbu-recon",
	})

	for attempt := 1; attempt <= 2; attempt++ {
		tx, err := conn.Begin(ctx)
		if err != nil {
			t.Fatalf("begin idle request %d: %v", attempt, err)
		}
		if _, err := tx.Exec(ctx, `set local role pomkita_app`); err != nil {
			t.Fatalf("set application role for request %d: %v", attempt, err)
		}
		_, err = tx.Exec(ctx, `select fn_set_request_context($1)`, token)
		if err == nil {
			_ = tx.Rollback(ctx)
			t.Fatalf("idle session was accepted on attempt %d", attempt)
		}
		_ = tx.Rollback(ctx)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) {
			t.Fatalf("request %d returned %T, want PostgreSQL error: %v", attempt, err, err)
		}
		if pgErr.Code != "28000" || pgErr.Message != "session_idle" {
			t.Fatalf("request %d error: code=%s message=%q", attempt, pgErr.Code, pgErr.Message)
		}
	}

	var lastActive time.Time
	if err := conn.QueryRow(ctx, `select last_active_at from sessions where jti = $1`, jti).Scan(&lastActive); err != nil {
		t.Fatalf("read idle session: %v", err)
	}
	if !lastActive.Equal(now.Add(-16 * time.Minute)) {
		t.Fatalf("idle session was revived: got %s", lastActive)
	}
}

func resetB0SessionActivity(t *testing.T, conn *pgx.Conn) {
	t.Helper()
	ctx := context.Background()
	root := repositoryRoot(t)
	if _, err := conn.Exec(ctx, `drop table if exists schema_migrations`); err != nil {
		t.Fatalf("reset migration metadata: %v", err)
	}
	var sessionsTable *string
	if err := conn.QueryRow(ctx, `select to_regclass('public.sessions')::text`).Scan(&sessionsTable); err != nil {
		t.Fatalf("check existing sessions table: %v", err)
	}
	resetMigrations := []string{"000002_b0_request_context.down.sql", "000009_b1_recovery.down.sql", "000008_b1_submit.down.sql", "000007_b1_drafts.down.sql", "000006_b1_shifts.down.sql", "000005_b1_catalog.down.sql", "000001_b0_foundation.down.sql"}
	if sessionsTable != nil {
		resetMigrations = append([]string{"000003_b0_session_activity.down.sql"}, resetMigrations...)
	}
	for _, name := range resetMigrations {
		if _, err := conn.Exec(ctx, readMigration(t, root, name)); err != nil {
			t.Fatalf("reset with %s: %v", name, err)
		}
	}
	for _, name := range []string{
		"000001_b0_foundation.up.sql",
		"000002_b0_request_context.up.sql",
		"000003_b0_session_activity.up.sql",
	} {
		if _, err := conn.Exec(ctx, readMigration(t, root, name)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
}

func seedB0SessionActivity(t *testing.T, conn *pgx.Conn, now time.Time, orgID, stationID, userID, jti string, keyExpiry time.Time) {
	t.Helper()
	ctx := context.Background()
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`insert into organizations (org_id, name) values ($1, 'Test Org')`, []any{orgID}},
		{`insert into stations (org_id, station_id, timezone) values ($1, $2, 'Asia/Jakarta')`, []any{orgID, stationID}},
		{`insert into users (user_id, org_id, display_name) values ($1, $2, 'Test User')`, []any{userID, orgID}},
		{`insert into user_station_roles (org_id, station_id, user_id, role) values ($1, $2, $3, 'Owner')`, []any{orgID, stationID, userID}},
		{`insert into jwt_keys (kid, secret_ref, status, activated_at, max_token_expiry) values ('key_1', 'app.jwt_secret.key_1', 'active', $1, $2)`, []any{now.Add(-time.Hour), keyExpiry}},
		{`insert into sessions (jti, kid, issued_at, expires_at, last_active_at) values ($1, 'key_1', $2, $3, $2)`, []any{jti, now.Add(-time.Minute), now.Add(10 * time.Minute)}},
	} {
		if _, err := conn.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed session activity data: %v", err)
		}
	}
	if _, err := conn.Exec(ctx, `select set_config('app.jwt_secret.key_1', 'test-secret', false)`); err != nil {
		t.Fatalf("set session activity test secret: %v", err)
	}
}
