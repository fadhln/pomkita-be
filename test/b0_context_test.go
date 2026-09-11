package test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestB0RequestContextValidAndInvalidClaims(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	resetB0Foundation(t, conn, true)

	orgID := "11111111-1111-4111-8111-111111111111"
	stationID := "22222222-2222-4222-8222-222222222222"
	userID := "33333333-3333-4333-8333-333333333333"
	validJTI := "44444444-4444-4444-8444-444444444444"
	secret := "test-secret"
	now := time.Now().UTC().Truncate(time.Microsecond)
	seedStatements := []struct {
		query string
		args  []any
	}{
		{`insert into organizations (org_id, name) values ($1, 'Test Org')`, []any{orgID}},
		{`insert into stations (org_id, station_id, timezone) values ($1, $2, 'Asia/Jakarta')`, []any{orgID, stationID}},
		{`insert into users (user_id, org_id, display_name) values ($1, $2, 'Test User')`, []any{userID, orgID}},
		{`insert into user_station_roles (org_id, station_id, user_id, role) values ($1, $2, $3, 'Owner')`, []any{orgID, stationID, userID}},
		{`insert into jwt_keys (kid, secret_ref, status, activated_at, max_token_expiry) values ('key_1', 'app.jwt_secret.key_1', 'active', $1::timestamptz, $1::timestamptz + interval '15 minutes')`, []any{now}},
		{`insert into sessions (jti, kid, issued_at, expires_at) values ($1, 'key_1', $2::timestamptz, $2::timestamptz + interval '15 minutes')`, []any{validJTI, now}},
	}
	for _, statement := range seedStatements {
		if _, err := conn.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed context data (%s): %v", statement.query, err)
		}
	}
	if _, err := conn.Exec(ctx, `select set_config('app.jwt_secret.key_1', $1, false)`, secret); err != nil {
		t.Fatalf("set test key: %v", err)
	}
	validToken := makeToken(t, secret, map[string]any{
		"alg": "HS256",
		"typ": "JWT",
		"kid": "key_1",
	}, map[string]any{
		"iss": "pomkita",
		"sub": userID,
		"jti": validJTI,
		"iat": now.Unix(),
		"exp": now.Add(10 * time.Minute).Unix(),
		"aud": "spbu-recon",
	})
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin context transaction: %v", err)
	}
	if _, err := tx.Exec(ctx, `set local role pomkita_app`); err != nil {
		t.Fatalf("set application role: %v", err)
	}
	if _, err := tx.Exec(ctx, `select fn_set_request_context($1)`, validToken); err != nil {
		t.Fatalf("valid token rejected: %v", err)
	}
	assertSetting(t, tx, "app.context_valid", "true")
	assertSetting(t, tx, "app.org_id", orgID)
	assertSetting(t, tx, "app.user_id", userID)
	assertSetting(t, tx, "app.role", "Owner")
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit context transaction: %v", err)
	}

	cases := []struct {
		name       string
		secret     string
		header     map[string]any
		claims     map[string]any
		expectCode string
	}{
		{
			name:       "algorithm",
			secret:     secret,
			header:     map[string]any{"alg": "HS512", "kid": "key_1"},
			expectCode: "jwt_invalid_algorithm",
		},
		{
			name:       "issuer",
			secret:     secret,
			header:     map[string]any{"alg": "HS256", "kid": "key_1"},
			claims:     map[string]any{"iss": "other"},
			expectCode: "jwt_invalid_issuer",
		},
		{
			name:       "audience",
			secret:     secret,
			header:     map[string]any{"alg": "HS256", "kid": "key_1"},
			claims:     map[string]any{"aud": "other"},
			expectCode: "jwt_invalid_audience",
		},
		{
			name:       "expired",
			secret:     secret,
			header:     map[string]any{"alg": "HS256", "kid": "key_1"},
			claims:     map[string]any{"exp": now.Add(-2 * time.Minute).Unix()},
			expectCode: "jwt_expired",
		},
		{
			name:       "future issued at",
			secret:     secret,
			header:     map[string]any{"alg": "HS256", "kid": "key_1"},
			claims:     map[string]any{"iat": now.Add(2 * time.Minute).Unix()},
			expectCode: "jwt_future_issued_at",
		},
		{
			name:       "unknown kid",
			secret:     secret,
			header:     map[string]any{"alg": "HS256", "kid": "missing"},
			expectCode: "jwt_unknown_kid",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			claims := map[string]any{
				"iss": "pomkita", "sub": userID, "jti": validJTI,
				"iat": now.Unix(), "exp": now.Add(10 * time.Minute).Unix(), "aud": "spbu-recon",
			}
			for key, value := range tc.claims {
				claims[key] = value
			}
			header := tc.header
			if header == nil {
				header = map[string]any{"alg": "HS256", "kid": "key_1"}
			}
			token := makeToken(t, tc.secret, header, claims)
			assertContextError(t, conn, token, tc.expectCode)
		})
	}

	if _, err := conn.Exec(ctx, `update sessions set revoked_at = clock_timestamp() where jti = $1`, validJTI); err != nil {
		t.Fatalf("revoke session: %v", err)
	}
	revoked := makeToken(t, secret, map[string]any{"alg": "HS256", "kid": "key_1"}, map[string]any{
		"iss": "pomkita", "sub": userID, "jti": validJTI,
		"iat": now.Unix(), "exp": now.Add(10 * time.Minute).Unix(), "aud": "spbu-recon",
	})
	assertContextError(t, conn, revoked, "jwt_revoked_jti")

	missingJTI := "55555555-5555-4555-8555-555555555555"
	missingSession := makeToken(t, secret, map[string]any{"alg": "HS256", "kid": "key_1"}, map[string]any{
		"iss": "pomkita", "sub": userID, "jti": missingJTI,
		"iat": now.Unix(), "exp": now.Add(10 * time.Minute).Unix(), "aud": "spbu-recon",
	})
	assertContextError(t, conn, missingSession, "jwt_session_not_found")
	assertContextError(t, conn, "", "jwt_missing_token")
	assertSetting(t, conn, "app.context_valid", "")
}

func resetB0Foundation(t *testing.T, conn *pgx.Conn, withContextFunction bool) {
	t.Helper()
	ctx := context.Background()
	root := repositoryRoot(t)
	if _, err := conn.Exec(ctx, `drop table if exists schema_migrations`); err != nil {
		t.Fatalf("reset migration metadata: %v", err)
	}
	if withContextFunction {
		if _, err := conn.Exec(ctx, readMigration(t, root, "000002_b0_request_context.down.sql")); err != nil {
			t.Fatalf("reset request context: %v", err)
		}
	}
	if _, err := conn.Exec(ctx, readMigration(t, root, "000001_b0_foundation.down.sql")); err != nil {
		t.Fatalf("reset foundation: %v", err)
	}
	if _, err := conn.Exec(ctx, readMigration(t, root, "000001_b0_foundation.up.sql")); err != nil {
		t.Fatalf("apply foundation: %v", err)
	}
	if withContextFunction {
		if _, err := conn.Exec(ctx, readMigration(t, root, "000002_b0_request_context.up.sql")); err != nil {
			t.Fatalf("apply request context: %v", err)
		}
	}
}

func openB0Connection(t *testing.T) *pgx.Conn {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://pomkita:pomkita_dev@127.0.0.1:5432/pomkita?sslmode=disable"
	}
	conn, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect to database: %v", err)
	}
	return conn
}

type queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func assertSetting(t *testing.T, conn queryer, name, expected string) {
	t.Helper()
	var actual string
	if err := conn.QueryRow(context.Background(), `select coalesce(current_setting($1, true), '')`, name).Scan(&actual); err != nil {
		t.Fatalf("read setting %s: %v", name, err)
	}
	if actual != expected {
		t.Fatalf("setting %s: got %q, want %q", name, actual, expected)
	}
}

func assertContextError(t *testing.T, conn *pgx.Conn, token, expected string) {
	t.Helper()
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin invalid context transaction: %v", err)
	}
	if _, err := tx.Exec(ctx, `set local role pomkita_app`); err != nil {
		t.Fatalf("set application role: %v", err)
	}
	_, err = tx.Exec(ctx, `select fn_set_request_context($1)`, token)
	if err == nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("expected %s", expected)
	}
	_ = tx.Rollback(ctx)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("expected PostgreSQL error, got %T: %v", err, err)
	}
	if pgErr.Message != expected {
		t.Fatalf("got error %q, want %q", pgErr.Message, expected)
	}
}

func makeToken(t *testing.T, secret string, header, claims map[string]any) string {
	t.Helper()
	encode := func(value any) string {
		bytes, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("marshal token part: %v", err)
		}
		return base64.RawURLEncoding.EncodeToString(bytes)
	}
	encodedHeader := encode(header)
	encodedClaims := encode(claims)
	input := encodedHeader + "." + encodedClaims
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(input))
	return input + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
