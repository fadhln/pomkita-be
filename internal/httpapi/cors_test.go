package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

const corsOrigin = "https://web.example"

func TestCORSMiddleware_PreflightAllowedOrigin(t *testing.T) {
	router := NewRouterWithDependencies("test", []string{corsOrigin}, readyStub{}, nil)
	executed := false
	router.OPTIONS("/cors", func(c *gin.Context) {
		executed = true
		c.Status(http.StatusTeapot)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodOptions, "/cors", nil)
	request.Header.Set("Origin", corsOrigin)
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	request.Header.Set("Access-Control-Request-Headers", "Content-Type, X-Request-ID")
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent || executed {
		t.Fatalf("preflight: status=%d executed=%t, want 204 and no handler", recorder.Code, executed)
	}
	assertHeader(t, recorder, "Access-Control-Allow-Origin", corsOrigin)
	assertHeader(t, recorder, "Access-Control-Allow-Credentials", "true")
	assertHeader(t, recorder, "Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	assertHeader(t, recorder, "Access-Control-Allow-Headers", "Content-Type, X-Requested-With, X-Request-ID")
	assertHeader(t, recorder, "Access-Control-Max-Age", "600")
	assertHeader(t, recorder, "Vary", "Origin, Access-Control-Request-Method, Access-Control-Request-Headers")
}

func TestCORSMiddleware_DeniesDisallowedPreflight(t *testing.T) {
	router := NewRouterWithDependencies("test", []string{corsOrigin}, readyStub{}, nil)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodOptions, "/cors", nil)
	request.Header.Set("Origin", "https://other.example")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status: got %d, want %d", recorder.Code, http.StatusForbidden)
	}
	if !strings.Contains(recorder.Body.String(), `"code":"forbidden"`) || !strings.Contains(recorder.Body.String(), `"message":"Origin not allowed"`) {
		t.Fatalf("error body: %s", recorder.Body.String())
	}
	if recorder.Header().Get("X-Request-ID") == "" || recorder.Header().Get("Access-Control-Allow-Origin") != "" || recorder.Header().Get("Vary") != "" {
		t.Fatalf("denied preflight headers: %#v", recorder.Header())
	}
}

func TestCORSMiddleware_AllowedActualAndNoOriginRequests(t *testing.T) {
	cases := []struct {
		name       string
		origins    []string
		origin     string
		method     string
		wantStatus int
		wantAllow  string
	}{
		{name: "allowed actual", origins: []string{corsOrigin}, origin: corsOrigin, method: http.MethodPost, wantStatus: http.StatusNoContent, wantAllow: corsOrigin},
		{name: "disallowed actual", origins: []string{corsOrigin}, origin: "https://other.example", method: http.MethodPost, wantStatus: http.StatusNoContent},
		{name: "bare preflight", origins: []string{corsOrigin}, method: http.MethodOptions, wantStatus: http.StatusNoContent},
		{name: "empty config", origin: corsOrigin, method: http.MethodOptions, wantStatus: http.StatusNoContent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := NewRouterWithDependencies("test", tc.origins, readyStub{}, nil)
			router.Handle(tc.method, "/cors", func(c *gin.Context) { c.Status(http.StatusNoContent) })
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(tc.method, "/cors", nil)
			if tc.origin != "" {
				request.Header.Set("Origin", tc.origin)
			}
			router.ServeHTTP(recorder, request)

			if recorder.Code != tc.wantStatus {
				t.Fatalf("status: got %d, want %d", recorder.Code, tc.wantStatus)
			}
			if recorder.Header().Get("Access-Control-Allow-Origin") != tc.wantAllow {
				t.Fatalf("allow origin: got %q, want %q", recorder.Header().Get("Access-Control-Allow-Origin"), tc.wantAllow)
			}
			if tc.wantAllow != "" {
				assertHeader(t, recorder, "Access-Control-Allow-Credentials", "true")
				assertHeader(t, recorder, "Vary", "Origin")
			} else if recorder.Header().Get("Access-Control-Allow-Credentials") != "" || recorder.Header().Get("Vary") != "" {
				t.Fatalf("unexpected CORS headers: %#v", recorder.Header())
			}
		})
	}
}

func assertHeader(t *testing.T, recorder *httptest.ResponseRecorder, name, want string) {
	t.Helper()
	if got := recorder.Header().Get(name); got != want {
		t.Fatalf("%s: got %q, want %q", name, got, want)
	}
}
