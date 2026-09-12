package test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	appdb "github.com/pomkita/pomkita-be/internal/db"
	appjwt "github.com/pomkita/pomkita-be/internal/jwt"
)

func TestB01SessionManagerUsesDatabaseProcedures(t *testing.T) {
	ctx := context.Background()
	conn := openB0Connection(t)
	defer conn.Close(ctx)
	resetB01Database(t, conn)
	seedB01User(t, conn, "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222", "33333333-3333-4333-8333-333333333333")

	database, err := appdb.New(ctx, "postgres://pomkita:pomkita_dev@127.0.0.1:5432/pomkita?sslmode=disable")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()
	database.SetJWTSecrets(map[string]string{"app.jwt_secret.key_1": "test-secret"})
	database.SetJWTAudience("pomkita")
	clock := time.Now().UTC().Truncate(time.Second)
	tokens := appjwt.NewService(database.JWTStore(map[string]string{"app.jwt_secret.key_1": "test-secret"}), appjwt.Config{
		Issuer: "pomkita", Audience: "pomkita", Now: func() time.Time { return clock },
	})
	manager := appdb.NewSessionManager(database, tokens)

	token, claims, err := manager.Login(ctx, "user@example.com", "correct-password")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if _, err := tokens.Verify(ctx, token); err != nil {
		t.Fatalf("verify login token: %v", err)
	}
	var sessionJTI uuid.UUID
	if err := conn.QueryRow(ctx, `select jti from sessions where jti = $1`, claims.JTI).Scan(&sessionJTI); err != nil {
		t.Fatalf("read created session: %v", err)
	}
	if sessionJTI != claims.JTI {
		t.Fatalf("session JTI: got %s, want %s", sessionJTI, claims.JTI)
	}

	view, err := manager.ReadSession(ctx, token)
	if err != nil {
		t.Fatalf("read session: %v", err)
	}
	if view.DisplayName != "Test User" || len(view.Roles) != 1 || view.Roles[0] != "Owner" {
		t.Fatalf("session view: %+v", view)
	}

	if _, _, err := manager.Login(ctx, "user@example.com", "wrong-password"); !errors.Is(err, appdb.ErrInvalidCredentials) {
		t.Fatalf("wrong password error: got %v", err)
	}
	if _, _, err := manager.Login(ctx, "missing@example.com", "wrong-password"); !errors.Is(err, appdb.ErrInvalidCredentials) {
		t.Fatalf("unknown email error: got %v", err)
	}

	if err := manager.Logout(ctx, claims.JTI); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, err := tokens.Verify(ctx, token); !errors.Is(err, appjwt.ErrRevokedJTI) {
		t.Fatalf("revoked token error: got %v", err)
	}
}
