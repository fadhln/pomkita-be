package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	appjwt "github.com/pomkita/pomkita-be/internal/jwt"
)

type readyStub struct {
	pingErr  error
	current  bool
	stateErr error
}

func (r readyStub) Ping(context.Context) error { return r.pingErr }

func (r readyStub) MigrationsCurrent(context.Context, int) (bool, error) {
	return r.current, r.stateErr
}

type verifierStub struct {
	claims appjwt.Claims
	err    error
}

func (v verifierStub) Verify(context.Context, string) (appjwt.Claims, error) {
	return v.claims, v.err
}

func TestReadyReturnsUnavailableWhenDatabaseIsNotReady(t *testing.T) {
	cases := []struct {
		name  string
		ready readyStub
	}{
		{name: "database unreachable", ready: readyStub{pingErr: errors.New("down"), current: true}},
		{name: "migrations not current", ready: readyStub{current: false}},
		{name: "migration check failed", ready: readyStub{current: true, stateErr: errors.New("failed")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := NewRouterWithDependencies("test", tc.ready, nil)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ready", nil))
			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("got %d, want %d", recorder.Code, http.StatusServiceUnavailable)
			}
		})
	}
}

func TestAuthMiddlewareVerifiesTokenAndAttachesClaims(t *testing.T) {
	claims := appjwt.Claims{Subject: uuid.MustParse("33333333-3333-4333-8333-333333333333")}
	router := NewRouterWithDependencies("test", readyStub{}, verifierStub{claims: claims})
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

func TestAuthMiddlewareRejectsInvalidToken(t *testing.T) {
	router := NewRouterWithDependencies("test", readyStub{}, nil)
	router.GET("/protected", AuthMiddleware(verifierStub{err: appjwt.ErrExpired}), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	request.Header.Set("Authorization", "Bearer token")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(recorder.Body.String(), `"code":"invalid_session"`) {
		t.Fatalf("response has no stable auth error: %s", recorder.Body.String())
	}
}

func TestAuthMiddlewareRejectsIdleSessionWithDistinctCode(t *testing.T) {
	router := NewRouterWithDependencies("test", readyStub{}, nil)
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

func TestErrorMappingMiddlewareMapsSQLState(t *testing.T) {
	router := NewRouterWithDependencies("test", readyStub{}, nil)
	router.GET("/conflict", func(c *gin.Context) {
		_ = c.Error(&pgconn.PgError{Code: "23505"})
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/conflict", nil))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("got %d, want %d", recorder.Code, http.StatusConflict)
	}
	if !strings.Contains(recorder.Body.String(), `"code":"conflict"`) {
		t.Fatalf("response has no stable conflict error: %s", recorder.Body.String())
	}
}

func TestErrorMappingMiddlewareMapsIdleSQLState(t *testing.T) {
	router := NewRouterWithDependencies("test", readyStub{}, nil)
	router.GET("/idle", func(c *gin.Context) {
		_ = c.Error(&pgconn.PgError{Code: "28000", Message: "session_idle"})
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/idle", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(recorder.Body.String(), `"code":"session_idle"`) {
		t.Fatalf("response has no idle-session error: %s", recorder.Body.String())
	}
}
