// Package transport provides shared HTTP handler support.
package transport

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	appjwt "github.com/pomkita/pomkita-be/internal/jwt"
	appaudit "github.com/pomkita/pomkita-be/internal/service/audit"
)

type TokenVerifier interface {
	Verify(context.Context, string) (appjwt.Claims, error)
}
type SessionView = appjwt.SessionView
type SessionService interface {
	ReadSession(context.Context, string) (SessionView, error)
}
type DeniedAuditService interface {
	RecordDenied(context.Context, appaudit.DeniedRequest) error
}

// DeniedAuditContext makes the denied-audit service available to middleware.
func DeniedAuditContext(service DeniedAuditService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if service != nil {
			c.Set("denied_audit", service)
		}
		c.Next()
	}
}

var decimalPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)

func AuthMiddleware(verifier TokenVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		if verifier == nil {
			denyAuthentication(c, "invalid_session")
			return
		}
		rawToken := bearerToken(c.GetHeader("Authorization"))
		if rawToken == "" {
			rawToken, _ = c.Cookie("pomkita_session")
		}
		if rawToken == "" {
			denyAuthentication(c, "invalid_session")
			return
		}
		claims, err := verifier.Verify(c.Request.Context(), rawToken)
		if err != nil {
			code := "invalid_session"
			if errors.Is(err, appjwt.ErrSessionIdle) {
				code = "session_idle"
			}
			denyAuthentication(c, code)
			return
		}
		c.Set("jwt_claims", claims)
		c.Set("raw_token", rawToken)
		c.Next()
	}
}

func RequireCSRF(c *gin.Context) {
	if c.GetHeader("X-Requested-With") == "" {
		WriteError(c, http.StatusForbidden, "csrf_required")
		return
	}
	c.Next()
}

func DecodeRequest(c *gin.Context, target any) bool {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil || ensureEnd(decoder) != nil {
		ValidationError(c)
		return false
	}
	return true
}

func ReadSession(c *gin.Context, sessions SessionService, stationID uuid.UUID) (SessionView, bool) {
	if sessions == nil {
		WriteError(c, http.StatusInternalServerError, "internal_error")
		return SessionView{}, false
	}
	session, err := sessions.ReadSession(c.Request.Context(), c.GetString("raw_token"))
	if err != nil {
		_ = c.Error(err)
		return SessionView{}, false
	}
	if !ContainsUUID(session.StationIDs, stationID) {
		WriteError(c, http.StatusForbidden, "station_scope_forbidden")
		return SessionView{}, false
	}
	return session, true
}

func ContainsUUID(values []uuid.UUID, target uuid.UUID) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
func ContainsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
func PathUUID(c *gin.Context, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		ValidationError(c)
		return uuid.Nil, false
	}
	return id, true
}
func ValidDecimal(value string) bool { return decimalPattern.MatchString(value) }
func ValidationError(c *gin.Context) { WriteError(c, http.StatusBadRequest, "validation_error") }
func WriteError(c *gin.Context, status int, code string) {
	c.AbortWithStatusJSON(status, gin.H{"code": code, "message": safeMessage(code, status), "request_id": c.GetString("request_id"), "field_errors": nil})
}
func WriteDraftRevisionResult(c *gin.Context, revision int, err error) {
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"revision": revision})
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
		return ""
	}
	return header[len(prefix):]
}
func denyAuthentication(c *gin.Context, reason string) {
	if value, exists := c.Get("denied_audit"); exists {
		if service, ok := value.(DeniedAuditService); ok {
			requestID := uuid.New()
			if parsed, err := uuid.Parse(c.GetString("request_id")); err == nil {
				requestID = parsed
			}
			target := c.FullPath()
			if target == "" {
				target = c.Request.URL.Path
			}
			if err := service.RecordDenied(c.Request.Context(), appaudit.DeniedRequest{RequestID: requestID, Action: strings.ToLower(c.Request.Method), Target: target, Reason: reason, Outcome: "denied"}); err != nil {
				c.AbortWithStatus(http.StatusInternalServerError)
				return
			}
		}
	}
	WriteError(c, http.StatusUnauthorized, reason)
}
func ensureEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}
func safeMessage(code string, status int) string {
	if code == "forbidden" {
		return "Origin not allowed"
	}
	if code == "invalid_credentials" {
		return "Invalid credentials"
	}
	if code == "csrf_required" {
		return "CSRF header required"
	}
	if status == http.StatusUnauthorized {
		return "Invalid session"
	}
	return "Request failed"
}
