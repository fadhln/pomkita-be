package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
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
