package transport

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestErrorMappingMiddlewareMapsSQLState(t *testing.T) {
	router := newErrorTestRouter()
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
	router := newErrorTestRouter()
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

func newErrorTestRouter() *gin.Engine {
	router := gin.New()
	router.Use(RequestID(), ErrorMappingMiddleware())
	return router
}
