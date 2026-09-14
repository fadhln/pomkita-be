package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
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

// SessionView is the verified identity and scope returned to the frontend.
type SessionView = appjwt.SessionView

// SessionService provides the database-backed session contract.
type SessionService interface {
	Login(context.Context, string, string) (string, appjwt.Claims, error)
	Logout(context.Context, uuid.UUID) error
	ReadSession(context.Context, string) (SessionView, error)
}

// ErrInvalidCredentials indicates that login credentials do not match an enabled user.
var ErrInvalidCredentials = appdb.ErrInvalidCredentials

// NewRouter creates a router without a database readiness dependency.
func NewRouter(environment string, allowedOrigins []string) *gin.Engine {
	return NewRouterWithDependencies(environment, allowedOrigins, nil, nil)
}

// NewRouterWithDependencies creates a router with its readiness and token services.
func NewRouterWithDependencies(environment string, allowedOrigins []string, readiness Readiness, verifier TokenVerifier, services ...SessionService) *gin.Engine {
	var sessions SessionService
	if len(services) > 0 {
		sessions = services[0]
	}
	return NewRouterWithAllDependencies(environment, allowedOrigins, readiness, verifier, sessions, nil)
}

// NewRouterWithAllDependencies creates a router with session and shift services.
func NewRouterWithAllDependencies(environment string, allowedOrigins []string, readiness Readiness, verifier TokenVerifier, sessions SessionService, shifts ShiftService, dependencies ...any) *gin.Engine {
	if environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	var governance GovernanceService
	var reporting ReportingService
	var policy PolicyRevisionService
	for _, dependency := range dependencies {
		if dependency == nil {
			continue
		}
		if candidate, ok := dependency.(GovernanceService); ok {
			governance = candidate
		}
		if candidate, ok := dependency.(ReportingService); ok {
			reporting = candidate
		}
		if candidate, ok := dependency.(PolicyRevisionService); ok {
			policy = candidate
		}
	}

	router := gin.New()
	router.Use(gin.Recovery(), corsMiddleware(allowedOrigins), requestID(), ErrorMappingMiddleware())
	router.GET("/health", health)
	router.GET("/ready", readyHandler(readiness))
	registerSessionRoutes(router, verifier, sessions, environment == "production")
	registerShiftRoutes(router, verifier, shifts)
	registerGovernanceRoutes(router, verifier, governance)
	registerReportingRoutes(router, verifier, reporting, policy)

	versioned := router.Group("/api/v1")
	registerSessionRoutes(versioned, verifier, sessions, environment == "production")
	registerShiftRoutes(versioned, verifier, shifts)
	registerGovernanceRoutes(versioned, verifier, governance)
	registerReportingRoutes(versioned, verifier, reporting, policy)
	return router
}

func registerSessionRoutes(router gin.IRoutes, verifier TokenVerifier, service SessionService, secure bool) {
	router.POST("/login", requireCSRF, loginHandler(service, secure))
	router.DELETE("/logout", AuthMiddleware(verifier), requireCSRF, logoutHandler(service, secure))
	router.GET("/session", AuthMiddleware(verifier), sessionHandler(service))
}

func registerShiftRoutes(router gin.IRoutes, verifier TokenVerifier, service ShiftService) {
	protectedWrite := []gin.HandlerFunc{AuthMiddleware(verifier), requireCSRF}
	protectedRead := []gin.HandlerFunc{AuthMiddleware(verifier)}
	router.POST("/shift/open", append(protectedWrite, openShiftHandler(service))...)
	router.POST("/draft/claim", append(protectedWrite, claimDraftHandler(service))...)
	router.POST("/draft/heartbeat", append(protectedWrite, heartbeatDraftHandler(service))...)
	router.POST("/draft/reading", append(protectedWrite, writeDraftReadingHandler(service))...)
	router.POST("/draft/sales", append(protectedWrite, writeDraftSalesHandler(service))...)
	router.POST("/draft/loss", append(protectedWrite, writeDraftLossHandler(service))...)
	router.POST("/draft/evidence", append(protectedWrite, stageDraftEvidenceHandler(service))...)
	router.GET("/draft", append(protectedRead, readDraftHandler(service))...)
	router.POST("/shift/submit", append(protectedWrite, submitShiftHandler(service))...)
	router.GET("/shifts", append(protectedRead, readShiftListHandler(service))...)
	router.GET("/shifts/:id", append(protectedRead, readShiftDetailHandler(service))...)
	router.GET("/report/:id", append(protectedRead, readReportHandler(service))...)
}

func registerGovernanceRoutes(router gin.IRoutes, verifier TokenVerifier, service GovernanceService) {
	protectedWrite := []gin.HandlerFunc{AuthMiddleware(verifier), requireCSRF}
	protectedRead := []gin.HandlerFunc{AuthMiddleware(verifier)}
	router.POST("/shift/ack", append(protectedWrite, ackShiftHandler(service))...)
	router.POST("/amendment/approve", append(protectedWrite, approveAmendmentHandler(service))...)
	router.POST("/amendment/request", append(protectedWrite, requestAmendmentHandler(service))...)
	router.POST("/amendment/reject", append(protectedWrite, rejectAmendmentHandler(service))...)
	router.GET("/amendments", append(protectedRead, readAmendmentQueueHandler(service))...)
	router.GET("/anomalies", append(protectedRead, readGovernanceAnomaliesHandler(service))...)
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
		current, err := readiness.MigrationsCurrent(c.Request.Context(), 23)
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
			rawToken, _ = c.Cookie("pomkita_session")
		}
		if rawToken == "" {
			writeError(c, http.StatusUnauthorized, "invalid_session")
			return
		}
		claims, err := verifier.Verify(c.Request.Context(), rawToken)
		if err != nil {
			code := "invalid_session"
			if errors.Is(err, appjwt.ErrSessionIdle) {
				code = "session_idle"
			}
			writeError(c, http.StatusUnauthorized, code)
			return
		}
		c.Set("jwt_claims", claims)
		c.Set("raw_token", rawToken)
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
		status := appdb.HTTPStatusForError(err)
		code := appdb.StableCodeForError(err)
		if code == "" {
			code = stableDatabaseCode(status)
		}
		if fields := fieldErrorsForDatabaseError(err); fields != nil {
			writeErrorWithFields(c, status, code, fields)
			return
		}
		writeError(c, status, code)
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
	writeErrorWithFields(c, status, code, nil)
}

func writeErrorWithFields(c *gin.Context, status int, code string, fields map[string]string) {
	c.AbortWithStatusJSON(status, gin.H{
		"code":         code,
		"message":      safeMessage(code, status),
		"request_id":   c.GetString("request_id"),
		"field_errors": fields,
	})
}

func fieldErrorsForDatabaseError(err error) map[string]string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23514" && pgErr.Message == "rollover_over_threshold" {
		return map[string]string{"readings": "Meter rollover is above the allowed threshold"}
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

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func loginHandler(service SessionService, secure bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if service == nil {
			writeError(c, http.StatusInternalServerError, "internal_error")
			return
		}
		var input loginRequest
		decoder := json.NewDecoder(c.Request.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeError(c, http.StatusBadRequest, "validation_error")
			return
		}
		if err := ensureEndOfJSON(decoder); err != nil || strings.TrimSpace(input.Email) == "" || input.Password == "" {
			writeError(c, http.StatusBadRequest, "validation_error")
			return
		}
		token, _, err := service.Login(c.Request.Context(), input.Email, input.Password)
		if err != nil {
			if errors.Is(err, ErrInvalidCredentials) {
				writeError(c, http.StatusUnauthorized, "invalid_credentials")
				return
			}
			_ = c.Error(err)
			return
		}
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie("pomkita_session", token, 900, "/", "", secure, true)
		c.Status(http.StatusOK)
	}
}

func ensureEndOfJSON(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func requireCSRF(c *gin.Context) {
	if c.GetHeader("X-Requested-With") == "" {
		writeError(c, http.StatusForbidden, "csrf_required")
		return
	}
	c.Next()
}

func logoutHandler(service SessionService, secure bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims := c.MustGet("jwt_claims").(appjwt.Claims)
		if err := service.Logout(c.Request.Context(), claims.JTI); err != nil {
			_ = c.Error(err)
			return
		}
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie("pomkita_session", "", -1, "/", "", secure, true)
		c.Status(http.StatusNoContent)
	}
}

func sessionHandler(service SessionService) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, err := service.ReadSession(c.Request.Context(), c.GetString("raw_token"))
		if err != nil {
			_ = c.Error(err)
			return
		}
		c.JSON(http.StatusOK, view)
	}
}

func requestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		setRequestID(c)
		c.Next()
	}
}

func setRequestID(c *gin.Context) {
	requestID := c.GetHeader("X-Request-ID")
	if requestID == "" {
		requestID = newRequestID()
	}
	c.Header("X-Request-ID", requestID)
	c.Set("request_id", requestID)
}

func corsMiddleware(allowedOrigins []string) gin.HandlerFunc {
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
				writeError(c, http.StatusForbidden, "forbidden")
				return
			}
			c.Next()
			return
		}

		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Access-Control-Allow-Credentials", "true")
		if c.Request.Method == http.MethodOptions {
			setRequestID(c)
			c.Header("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
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

func newRequestID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err == nil {
		return hex.EncodeToString(bytes)
	}
	return time.Now().UTC().Format("20060102T150405.000000000Z07:00")
}
