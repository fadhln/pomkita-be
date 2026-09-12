package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	appdb "github.com/pomkita/pomkita-be/internal/db"
	"github.com/pomkita/pomkita-be/internal/httpapi"
	appjwt "github.com/pomkita/pomkita-be/internal/jwt"
)

func TestB01HTTPSessionEndpointsUseSessionCookieAndRevokeIt(t *testing.T) {
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
	tokens := appjwt.NewService(database.JWTStore(map[string]string{"app.jwt_secret.key_1": "test-secret"}), appjwt.Config{
		Issuer: "pomkita", Audience: "pomkita", Now: time.Now,
	})
	manager := appdb.NewSessionManager(database, tokens)
	router := httpapi.NewRouterWithDependencies("test", nil, database, tokens, manager)

	loginResponse := httptest.NewRecorder()
	loginRequest := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"email":"user@example.com","password":"correct-password"}`))
	loginRequest.Header.Set("Content-Type", "application/json")
	loginRequest.Header.Set("X-Requested-With", "XMLHttpRequest")
	router.ServeHTTP(loginResponse, loginRequest)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("login status: got %d body=%s", loginResponse.Code, loginResponse.Body.String())
	}
	cookies := loginResponse.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "pomkita_session" {
		t.Fatalf("login cookies: %#v", cookies)
	}
	rawToken := cookies[0].Value
	verifiedClaims, err := tokens.Verify(ctx, rawToken)
	if err != nil {
		t.Fatalf("middleware token verification: %v", err)
	}

	sessionResponse := httptest.NewRecorder()
	sessionRequest := httptest.NewRequest(http.MethodGet, "/session", nil)
	sessionRequest.AddCookie(cookies[0])
	router.ServeHTTP(sessionResponse, sessionRequest)
	if sessionResponse.Code != http.StatusOK {
		t.Fatalf("session status: got %d body=%s", sessionResponse.Code, sessionResponse.Body.String())
	}
	var view map[string]any
	if err := json.Unmarshal(sessionResponse.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode session response: %v", err)
	}
	if view["display_name"] != "Test User" {
		t.Fatalf("display name: got %v", view["display_name"])
	}
	if roles, ok := view["roles"].([]any); !ok || len(roles) != 1 || roles[0] != "Owner" {
		t.Fatalf("roles: got %v", view["roles"])
	}

	logoutResponse := httptest.NewRecorder()
	logoutRequest := httptest.NewRequest(http.MethodDelete, "/logout", nil)
	logoutRequest.AddCookie(cookies[0])
	logoutRequest.Header.Set("X-Requested-With", "XMLHttpRequest")
	router.ServeHTTP(logoutResponse, logoutRequest)
	if logoutResponse.Code != http.StatusNoContent {
		t.Fatalf("logout status: got %d body=%s", logoutResponse.Code, logoutResponse.Body.String())
	}
	var revokedAt *time.Time
	if _, err := tokens.Verify(ctx, rawToken); err == nil {
		t.Fatal("revoked token was accepted")
	}
	if err := conn.QueryRow(ctx, `select revoked_at from sessions where jti = $1`, verifiedClaims.JTI).Scan(&revokedAt); err != nil {
		t.Fatalf("read revoked session: %v", err)
	}
	if revokedAt == nil {
		t.Fatal("session revoked_at is null")
	}

	secondSessionResponse := httptest.NewRecorder()
	secondSessionRequest := httptest.NewRequest(http.MethodGet, "/session", nil)
	secondSessionRequest.AddCookie(cookies[0])
	router.ServeHTTP(secondSessionResponse, secondSessionRequest)
	if secondSessionResponse.Code != http.StatusUnauthorized || !strings.Contains(secondSessionResponse.Body.String(), `"code":"invalid_session"`) {
		t.Fatalf("revoked request: status=%d body=%s", secondSessionResponse.Code, secondSessionResponse.Body.String())
	}
}
