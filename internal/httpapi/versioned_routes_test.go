package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVersionedSessionRoute_UsesTheV1ProductPrefix(t *testing.T) {
	view := SessionView{DisplayName: "Test User"}
	router := testRouter("test", nil, readyStub{}, verifierStub{}, &sessionServiceStub{view: view})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	request.Header.Set("Authorization", "Bearer token")
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"display_name":"Test User"`) {
		t.Fatalf("response does not contain the session view: %s", recorder.Body.String())
	}
}

func TestVersionedHealthAndReadyRoutes_AreNotRegistered(t *testing.T) {
	router := testRouter("test", nil, readyStub{}, nil, nil)

	for _, path := range []string{"/api/v1/health", "/api/v1/ready"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))

		if recorder.Code != http.StatusNotFound {
			t.Fatalf("path %s: status got %d, want %d", path, recorder.Code, http.StatusNotFound)
		}
	}
}
