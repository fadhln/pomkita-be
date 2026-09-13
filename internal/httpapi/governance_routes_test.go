package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGovernanceAPI_RequiresAuthenticationOnEveryEndpoint(t *testing.T) {
	router := NewRouterWithDependencies("test", nil, readyStub{}, nil)
	for _, item := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/shift/ack"},
		{http.MethodPost, "/amendment/approve"},
		{http.MethodGet, "/anomalies"},
	} {
		t.Run(item.method+" "+item.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(item.method, item.path, nil))
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status: got %d, want %d; body=%s", recorder.Code, http.StatusUnauthorized, recorder.Body.String())
			}
		})
	}
}
