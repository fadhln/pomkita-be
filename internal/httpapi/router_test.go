package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthAndReady(t *testing.T) {
	router := NewRouter("test")

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
