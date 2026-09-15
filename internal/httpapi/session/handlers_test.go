package session

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/pomkita/pomkita-be/internal/httpapi/transport"
	appjwt "github.com/pomkita/pomkita-be/internal/jwt"
	appauth "github.com/pomkita/pomkita-be/internal/service/auth"
)

type sessionServiceStub struct {
	loginToken string
	loginErr   error
	logoutErr  error
	view       appjwt.SessionView
	readErr    error
	logoutJTI  uuid.UUID
}

func (s *sessionServiceStub) Login(context.Context, string, string) (string, appjwt.Claims, error) {
	return s.loginToken, appjwt.Claims{Subject: uuid.MustParse("33333333-3333-4333-8333-333333333333")}, s.loginErr
}

func (s *sessionServiceStub) Logout(_ context.Context, jti uuid.UUID) error {
	s.logoutJTI = jti
	return s.logoutErr
}

func (s *sessionServiceStub) ReadSession(context.Context, string) (appjwt.SessionView, error) {
	return s.view, s.readErr
}

func TestLoginSetsSessionCookieWithContractFlags(t *testing.T) {
	service := &sessionServiceStub{loginToken: "jwt-token"}
	router := testSessionRouter("production", nil, service)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":"demo-user","password":"secret"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Requested-With", "XMLHttpRequest")
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", recorder.Code, http.StatusOK)
	}
	cookie := recorder.Result().Cookies()
	if len(cookie) != 1 {
		t.Fatalf("cookies: got %d, want 1", len(cookie))
	}
	if cookie[0].Name != "pomkita_session" || cookie[0].Value != "jwt-token" {
		t.Fatalf("session cookie: %#v", cookie[0])
	}
	if !cookie[0].HttpOnly || !cookie[0].Secure || cookie[0].SameSite != http.SameSiteLaxMode || cookie[0].Path != "/" || cookie[0].MaxAge != 900 {
		t.Fatalf("cookie flags: %#v", cookie[0])
	}
}

func TestLoginRequiresCSRFHeader(t *testing.T) {
	router := testSessionRouter("test", nil, &sessionServiceStub{loginToken: "jwt-token"})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":"demo-user","password":"secret"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"csrf_required"`) {
		t.Fatalf("response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestLoginReturnsSameInvalidCredentialsErrorForBadPasswordAndUnknownEmail(t *testing.T) {
	for _, name := range []string{"bad password", "unknown email"} {
		t.Run(name, func(t *testing.T) {
			service := &sessionServiceStub{loginErr: appauth.ErrInvalidCredentials}
			router := testSessionRouter("test", nil, service)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":"demo-user","password":"wrong"}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Requested-With", "XMLHttpRequest")
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusUnauthorized || !strings.Contains(recorder.Body.String(), `"code":"invalid_credentials"`) {
				t.Fatalf("response: status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestLogoutRevokesAuthenticatedSessionAndClearsCookie(t *testing.T) {
	jti := uuid.MustParse("44444444-4444-4444-8444-444444444444")
	service := &sessionServiceStub{}
	router := testSessionRouter("test", sessionVerifierStub{claims: appjwt.Claims{JTI: jti}}, service)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/logout", nil)
	request.Header.Set("Authorization", "Bearer token")
	request.Header.Set("X-Requested-With", "XMLHttpRequest")
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status: got %d, want %d", recorder.Code, http.StatusNoContent)
	}
	if service.logoutJTI != jti {
		t.Fatalf("logout JTI: got %s, want %s", service.logoutJTI, jti)
	}
	cookie := recorder.Result().Cookies()
	if len(cookie) != 1 || cookie[0].Name != "pomkita_session" || cookie[0].MaxAge >= 0 || cookie[0].Value != "" {
		t.Fatalf("cleared cookie: %#v", cookie)
	}
}

func TestLogoutRequiresCSRFHeader(t *testing.T) {
	router := testSessionRouter("test", sessionVerifierStub{}, &sessionServiceStub{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/logout", nil)
	request.Header.Set("Authorization", "Bearer token")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"csrf_required"`) {
		t.Fatalf("response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestLogoutReturnsInternalErrorWhenSessionServiceIsMissing(t *testing.T) {
	router := testSessionRouter("test", sessionVerifierStub{}, nil)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/logout", nil)
	request.Header.Set("Authorization", "Bearer token")
	request.Header.Set("X-Requested-With", "XMLHttpRequest")
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError || !strings.Contains(recorder.Body.String(), `"code":"internal_error"`) {
		t.Fatalf("response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestSessionReturnsSessionPayload(t *testing.T) {
	view := appjwt.SessionView{
		UserID:      uuid.MustParse("33333333-3333-4333-8333-333333333333"),
		DisplayName: "Test User",
		Username:    "test-user",
		Roles:       []string{"Owner"},
		OrgID:       uuid.MustParse("11111111-1111-4111-8111-111111111111"),
		StationIDs:  []uuid.UUID{uuid.MustParse("22222222-2222-4222-8222-222222222222")},
	}
	router := testSessionRouter("test", sessionVerifierStub{}, &sessionServiceStub{view: view})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/session", nil)
	request.Header.Set("Authorization", "Bearer token")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", recorder.Code, http.StatusOK)
	}
	body := recorder.Body.String()
	for _, expected := range []string{`"display_name":"Test User"`, `"roles":["Owner"]`, `"org_id":"11111111-1111-4111-8111-111111111111"`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("response does not contain %s: %s", expected, body)
		}
	}
}

type sessionVerifierStub struct {
	claims appjwt.Claims
	err    error
}

func (v sessionVerifierStub) Verify(context.Context, string) (appjwt.Claims, error) {
	return v.claims, v.err
}

func testSessionRouter(environment string, verifier transport.TokenVerifier, service Service) *gin.Engine {
	router := gin.New()
	router.Use(transport.RequestID(), transport.ErrorMappingMiddleware())
	RegisterRoutes(router, verifier, service, environment == "production")
	return router
}
