package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/pomkita/pomkita-be/internal/domain"
)

func TestHealthAndReady(t *testing.T) {
	router := NewRouterWithDependencies("test", nil, readyStub{current: true}, nil)

	for _, path := range []string{"/health", "/ready"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest("GET", path, nil)
		router.ServeHTTP(recorder, request)

		if recorder.Code != 200 {
			t.Fatalf("expected 200, got %d", recorder.Code)
		}
		if !strings.Contains(recorder.Body.String(), `"status"`) {
			t.Fatal("response does not contain status")
		}
		if recorder.Header().Get("X-Request-ID") == "" {
			t.Fatal("response does not contain X-Request-ID")
		}
	}
}

func TestRouterWithDependencySetUsesExplicitReadinessConfiguration(t *testing.T) {
	latest := 0
	router := NewRouterWithDependencySet("test", nil, RouterDependencies{
		Readiness:       readyStub{current: true, latest: &latest},
		LatestMigration: 6,
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/ready", nil))

	if recorder.Code != 200 || latest != 6 {
		t.Fatalf("ready response: status=%d latest=%d, want 200 and 6", recorder.Code, latest)
	}
}

func TestRouterWithDependencySetDoesNotRegisterLegacyRoutesByDefault(t *testing.T) {
	router := NewRouterWithDependencySet("test", nil, RouterDependencies{})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/api/v1/report/11111111-1111-4111-8111-111111111111", nil))
	if recorder.Code != 404 {
		t.Fatalf("legacy route status: got %d, want 404", recorder.Code)
	}
}

func TestErrorMappingMiddleware_MapsDomainConflictToStableResponse(t *testing.T) {
	router := NewRouter("test", nil)
	router.GET("/domain-error", func(c *gin.Context) {
		_ = c.Error(domain.NewError(domain.CategoryConflict, "ack_already_decided"))
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/domain-error", nil))
	if recorder.Code != 409 || !strings.Contains(recorder.Body.String(), `"code":"ack_already_decided"`) {
		t.Fatalf("domain error response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
