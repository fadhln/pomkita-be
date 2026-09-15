package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	appjwt "github.com/pomkita/pomkita-be/internal/jwt"
	appaudit "github.com/pomkita/pomkita-be/internal/service/audit"
)

type verifierStub struct {
	claims appjwt.Claims
	err    error
}

func (v verifierStub) Verify(context.Context, string) (appjwt.Claims, error) {
	return v.claims, v.err
}

type deniedAuditSpy struct {
	requests []appaudit.DeniedRequest
}

func (s *deniedAuditSpy) RecordDenied(_ context.Context, request appaudit.DeniedRequest) error {
	s.requests = append(s.requests, request)
	return nil
}

func TestAuthMiddlewareVerifiesTokenAndAttachesClaims(t *testing.T) {
	claims := appjwt.Claims{Subject: uuid.MustParse("33333333-3333-4333-8333-333333333333")}
	router := newAuthTestRouter(nil)
	router.GET("/protected", AuthMiddleware(verifierStub{claims: claims}), func(c *gin.Context) {
		value, exists := c.Get("jwt_claims")
		if !exists || value.(appjwt.Claims).Subject != claims.Subject {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusNoContent)
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	request.Header.Set("Authorization", "Bearer token")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("got %d, want %d", recorder.Code, http.StatusNoContent)
	}
}

func TestAuthMiddlewareRecordsDeniedRequestMetadata(t *testing.T) {
	denied := &deniedAuditSpy{}
	router := newAuthTestRouter(denied)
	router.GET("/api/v1/session", AuthMiddleware(verifierStub{err: appjwt.ErrInvalidSignature}), func(c *gin.Context) {})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/session", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if len(denied.requests) != 1 || denied.requests[0].Reason != "invalid_session" || denied.requests[0].Target != "/api/v1/session" {
		t.Fatalf("denied audit requests: %+v", denied.requests)
	}
}

func TestAuthMiddlewareRejectsInvalidToken(t *testing.T) {
	router := newAuthTestRouter(nil)
	router.GET("/protected", AuthMiddleware(verifierStub{err: appjwt.ErrExpired}), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/protected", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(recorder.Body.String(), `"code":"invalid_session"`) {
		t.Fatalf("response has no stable auth error: %s", recorder.Body.String())
	}
}

func TestAuthMiddlewareRejectsIdleSessionWithDistinctCode(t *testing.T) {
	router := newAuthTestRouter(nil)
	router.GET("/protected", AuthMiddleware(verifierStub{err: appjwt.ErrSessionIdle}), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	request.Header.Set("Authorization", "Bearer token")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(recorder.Body.String(), `"code":"session_idle"`) {
		t.Fatalf("response has no idle-session error: %s", recorder.Body.String())
	}
}

func newAuthTestRouter(denied DeniedAuditService) *gin.Engine {
	router := gin.New()
	router.Use(DeniedAuditContext(denied), RequestID(), ErrorMappingMiddleware())
	return router
}
