package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestShiftAPI_RequiresAuthenticationOnEveryEndpoint(t *testing.T) {
	router := NewRouterWithDependencies("test", nil, readyStub{}, nil)
	requests := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/shift/open"},
		{http.MethodPost, "/draft/claim"},
		{http.MethodPost, "/draft/heartbeat"},
		{http.MethodPost, "/draft/reading"},
		{http.MethodPost, "/draft/sales"},
		{http.MethodPost, "/draft/loss"},
		{http.MethodPost, "/draft/evidence"},
		{http.MethodGet, "/draft?shift_id=11111111-1111-4111-8111-111111111111"},
		{http.MethodPost, "/shift/submit"},
		{http.MethodGet, "/shifts"},
		{http.MethodGet, "/shifts/11111111-1111-4111-8111-111111111111"},
		{http.MethodGet, "/report/11111111-1111-4111-8111-111111111111"},
	}
	for _, item := range requests {
		t.Run(item.method+" "+item.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(item.method, item.path, nil))
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status: got %d, want %d; body=%s", recorder.Code, http.StatusUnauthorized, recorder.Body.String())
			}
		})
	}
}

func TestShiftAPI_RejectsMalformedMutationJSON(t *testing.T) {
	router := NewRouterWithAllDependencies("test", nil, readyStub{}, verifierStub{}, nil, nil)
	request := httptest.NewRequest(http.MethodPost, "/draft/reading", strings.NewReader("{"))
	request.Header.Set("Authorization", "Bearer token")
	request.Header.Set("X-Requested-With", "XMLHttpRequest")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}
