package transport

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pomkita/pomkita-be/internal/domain"
	appjwt "github.com/pomkita/pomkita-be/internal/jwt"
)

// ErrorMappingMiddleware maps known service and PostgreSQL errors to safe HTTP errors.
func ErrorMappingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if c.Writer.Written() || len(c.Errors) == 0 {
			return
		}
		err := c.Errors.Last().Err
		status := httpStatusForDatabaseError(err)
		code := stableCodeForDatabaseError(err)
		if domainStatus, domainCode := domainHTTPError(err); domainCode != "" {
			status = domainStatus
			code = domainCode
		}
		if code == "" {
			code = stableDatabaseCode(status)
		}
		if fields := fieldErrorsForDatabaseError(err); fields != nil {
			writeErrorWithFields(c, status, code, fields)
			return
		}
		WriteError(c, status, code)
	}
}

// RequestID adds a request identifier to the request context and response.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		setRequestID(c)
		c.Next()
	}
}

// CORSMiddleware applies the configured cross-origin request policy.
func CORSMiddleware(allowedOrigins []string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		allowed[origin] = struct{}{}
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" || len(allowed) == 0 {
			c.Next()
			return
		}
		if _, ok := allowed[origin]; !ok {
			if c.Request.Method == http.MethodOptions {
				setRequestID(c)
				WriteError(c, http.StatusForbidden, "forbidden")
				return
			}
			c.Next()
			return
		}

		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Access-Control-Allow-Credentials", "true")
		if c.Request.Method == http.MethodOptions {
			setRequestID(c)
			c.Header("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Content-Type, X-Requested-With, X-Request-ID")
			c.Header("Access-Control-Max-Age", "600")
			c.Header("Vary", "Origin, Access-Control-Request-Method, Access-Control-Request-Headers")
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Header("Vary", "Origin")
		c.Next()
	}
}

func domainHTTPError(err error) (int, string) {
	if errors.Is(err, appjwt.ErrSessionNotFound) || errors.Is(err, appjwt.ErrSessionExpired) || errors.Is(err, appjwt.ErrRevokedJTI) {
		return http.StatusUnauthorized, "invalid_session"
	}
	var domainErr *domain.Error
	if !errors.As(err, &domainErr) {
		return 0, ""
	}
	switch domainErr.Category {
	case domain.CategoryAuthentication:
		return http.StatusUnauthorized, domainErr.Code
	case domain.CategoryAuthorization:
		return http.StatusForbidden, domainErr.Code
	case domain.CategoryValidation:
		return http.StatusUnprocessableEntity, domainErr.Code
	case domain.CategoryConflict:
		return http.StatusConflict, domainErr.Code
	case domain.CategoryNotFound:
		return http.StatusNotFound, domainErr.Code
	case domain.CategoryDependency:
		return http.StatusServiceUnavailable, domainErr.Code
	default:
		return http.StatusInternalServerError, domainErr.Code
	}
}

func stableDatabaseCode(status int) string {
	switch status {
	case http.StatusConflict:
		return "conflict"
	case http.StatusUnprocessableEntity:
		return "validation_error"
	default:
		return "internal_error"
	}
}

func writeErrorWithFields(c *gin.Context, status int, code string, fields map[string]string) {
	c.Header("Content-Type", "application/problem+json")
	c.AbortWithStatusJSON(status, gin.H{
		"type": "about:blank", "title": http.StatusText(status), "status": status, "detail": safeMessage(code, status), "instance": c.Request.URL.Path,
		"code": code, "message": safeMessage(code, status),
		"request_id": c.GetString("request_id"), "field_errors": fields,
	})
}

func setRequestID(c *gin.Context) {
	requestID := c.GetHeader("X-Request-ID")
	if requestID == "" {
		requestID = newRequestID()
	}
	c.Header("X-Request-ID", requestID)
	c.Set("request_id", requestID)
}

func newRequestID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err == nil {
		return hex.EncodeToString(bytes)
	}
	return time.Now().UTC().Format("20060102T150405.000000000Z07:00")
}
