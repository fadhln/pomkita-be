package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	appdb "github.com/pomkita/pomkita-be/internal/db"
	appjwt "github.com/pomkita/pomkita-be/internal/jwt"
)

// Readiness checks database reachability and migration state.
type Readiness interface {
	Ping(context.Context) error
	MigrationsCurrent(context.Context, int) (bool, error)
}

// TokenVerifier verifies a raw session token.
type TokenVerifier interface {
	Verify(context.Context, string) (appjwt.Claims, error)
}

// NewRouter creates a router without a database readiness dependency.
func NewRouter(environment string) *gin.Engine {
	return NewRouterWithDependencies(environment, nil, nil)
}

// NewRouterWithDependencies creates a router with its readiness and token services.
func NewRouterWithDependencies(environment string, readiness Readiness, verifier TokenVerifier) *gin.Engine {
	if environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	router.Use(gin.Recovery(), requestID(), ErrorMappingMiddleware())
	router.GET("/health", health)
	router.GET("/ready", readyHandler(readiness))
	return router
}

func health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func readyHandler(readiness Readiness) gin.HandlerFunc {
	return func(c *gin.Context) {
		if readiness == nil || readiness.Ping(c.Request.Context()) != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unready"})
			return
		}
		current, err := readiness.MigrationsCurrent(c.Request.Context(), 2)
		if err != nil || !current {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unready"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	}
}

// AuthMiddleware verifies the bearer token or session cookie and stores claims.
func AuthMiddleware(verifier TokenVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		if verifier == nil {
			writeError(c, http.StatusUnauthorized, "invalid_session")
			return
		}
		rawToken := bearerToken(c.GetHeader("Authorization"))
		if rawToken == "" {
			rawToken, _ = c.Cookie("session")
		}
		if rawToken == "" {
			writeError(c, http.StatusUnauthorized, "invalid_session")
			return
		}
		claims, err := verifier.Verify(c.Request.Context(), rawToken)
		if err != nil {
			writeError(c, http.StatusUnauthorized, "invalid_session")
			return
		}
		c.Set("jwt_claims", claims)
		c.Next()
	}
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
		return ""
	}
	return header[len(prefix):]
}

// ErrorMappingMiddleware maps known PostgreSQL errors to safe HTTP errors.
func ErrorMappingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if c.Writer.Written() || len(c.Errors) == 0 {
			return
		}
		err := c.Errors.Last().Err
		writeError(c, appdb.HTTPStatusForError(err), stableDatabaseCode(appdb.HTTPStatusForError(err)))
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

func writeError(c *gin.Context, status int, code string) {
	c.AbortWithStatusJSON(status, gin.H{
		"code":       code,
		"message":    safeMessage(status),
		"request_id": c.GetString("request_id"),
	})
}

func safeMessage(status int) string {
	if status == http.StatusUnauthorized {
		return "Invalid session"
	}
	return "Request failed"
}

func requestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = newRequestID()
		}
		c.Header("X-Request-ID", requestID)
		c.Set("request_id", requestID)
		c.Next()
	}
}

func newRequestID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err == nil {
		return hex.EncodeToString(bytes)
	}
	return time.Now().UTC().Format("20060102T150405.000000000Z07:00")
}
