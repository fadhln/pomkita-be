package test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestB01LoginProcedureAuthenticatesWithAppRole(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	resetB01Database(t, conn)

	const (
		orgID     = "11111111-1111-4111-8111-111111111111"
		stationID = "22222222-2222-4222-8222-222222222222"
		userID    = "33333333-3333-4333-8333-333333333333"
	)
	seedB01User(t, conn, orgID, stationID, userID)

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin login transaction: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // the transaction is committed only after all assertions pass
	if _, err := tx.Exec(ctx, `set local role pomkita_app`); err != nil {
		t.Fatalf("set application role: %v", err)
	}

	var gotUserID uuid.UUID
	if err := tx.QueryRow(ctx, `select user_id from public.fn_login_user($1, $2)`, "user@example.com", "correct-password").Scan(&gotUserID); err != nil {
		t.Fatalf("login procedure: %v", err)
	}
	if gotUserID != uuid.MustParse(userID) {
		t.Fatalf("user ID: got %s, want %s", gotUserID, userID)
	}

	var jti = uuid.MustParse("44444444-4444-4444-8444-444444444444")
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := tx.Exec(ctx, `select public.fn_create_session($1, $2, $3, $4, $5)`, jti, "key_1", now, now.Add(15*time.Minute), now); err != nil {
		t.Fatalf("create session procedure: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit login transaction: %v", err)
	}

	var sessionJTI uuid.UUID
	if err := conn.QueryRow(ctx, `select jti from public.fn_read_session_record($1)`, jti).Scan(&sessionJTI); err != nil {
		t.Fatalf("read session procedure: %v", err)
	}
	if sessionJTI != jti {
		t.Fatalf("session JTI: got %s, want %s", sessionJTI, jti)
	}
}

func resetB01Database(t *testing.T, conn *pgx.Conn) {
	t.Helper()
	resetB0Foundation(t, conn, true)
}

func seedB01User(t *testing.T, conn *pgx.Conn, orgID, stationID, userID string) {
	t.Helper()
	ctx := context.Background()
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`insert into organizations (org_id, name) values ($1, 'Test Org')`, []any{orgID}},
		{`insert into stations (org_id, station_id, timezone) values ($1, $2, 'Asia/Jakarta')`, []any{orgID, stationID}},
		{`insert into users (user_id, org_id, email, display_name, password_hash) values ($1, $2, $3, 'Test User', app.crypt($4, app.gen_salt('bf')))`, []any{userID, orgID, "user@example.com", "correct-password"}},
		{`insert into user_station_roles (org_id, station_id, user_id, role) values ($1, $2, $3, 'Owner')`, []any{orgID, stationID, userID}},
		{`insert into jwt_keys (kid, secret_ref, status, activated_at, max_token_expiry) values ('key_1', 'app.jwt_secret.key_1', 'active', clock_timestamp(), clock_timestamp() + interval '15 minutes')`, nil},
	} {
		if _, err := conn.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed session data: %v", err)
		}
	}
}
