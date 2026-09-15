package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/pomkita/pomkita-be/internal/httpapi/session"
	"github.com/pomkita/pomkita-be/internal/httpapi/transport"
	appjwt "github.com/pomkita/pomkita-be/internal/jwt"
)

type readyStub struct {
	pingErr  error
	current  bool
	stateErr error
	latest   *int
}

func testRouter(environment string, origins []string, readiness Readiness, verifier transport.TokenVerifier, sessions session.Service) *gin.Engine {
	return NewRouterWithDependencySet(environment, origins, RouterDependencies{
		Readiness: readiness,
		Verifier:  verifier,
		Sessions:  sessions,
	})
}

func (r readyStub) Ping(context.Context) error { return r.pingErr }

func (r readyStub) MigrationsCurrent(_ context.Context, latest int) (bool, error) {
	if r.latest != nil {
		*r.latest = latest
	}
	return r.current, r.stateErr
}

type verifierStub struct {
	claims appjwt.Claims
	err    error
}

func (v verifierStub) Verify(context.Context, string) (appjwt.Claims, error) {
	return v.claims, v.err
}

func TestReadyChecksLatestMigration(t *testing.T) {
	latest := 0
	router := testRouter("test", nil, readyStub{current: true, latest: &latest}, nil, nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if recorder.Code != http.StatusOK || latest != 13 {
		t.Fatalf("ready migration check: status=%d latest=%d, want 200 and 13", recorder.Code, latest)
	}
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
			router := testRouter("test", nil, tc.ready, nil, nil)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ready", nil))
			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("got %d, want %d", recorder.Code, http.StatusServiceUnavailable)
			}
		})
	}
}
