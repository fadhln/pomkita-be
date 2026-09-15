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
	"github.com/pomkita/pomkita-be/internal/domain"
	auditapi "github.com/pomkita/pomkita-be/internal/httpapi/audit"
	draftapi "github.com/pomkita/pomkita-be/internal/httpapi/draft"
	governanceapi "github.com/pomkita/pomkita-be/internal/httpapi/governance"
	policyapi "github.com/pomkita/pomkita-be/internal/httpapi/policy"
	reportingapi "github.com/pomkita/pomkita-be/internal/httpapi/reporting"
	shiftapi "github.com/pomkita/pomkita-be/internal/httpapi/shift"
	submissionapi "github.com/pomkita/pomkita-be/internal/httpapi/submission"
	appjwt "github.com/pomkita/pomkita-be/internal/jwt"
	appaudit "github.com/pomkita/pomkita-be/internal/service/audit"
	appauth "github.com/pomkita/pomkita-be/internal/service/auth"
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

// DeniedAuditService records safe metadata for denied requests.
type DeniedAuditService interface {
	RecordDenied(context.Context, appaudit.DeniedRequest) error
}

// RouterDependencies contains all services required by the HTTP adapter.
type RouterDependencies struct {
	Readiness       Readiness
	Verifier        TokenVerifier
	Sessions        SessionService
	Shift           ShiftService
	ShiftRead       ShiftReadService
	Draft           DraftService
	DraftWrites     DraftWriteService
	Submission      SubmissionService
	Governance      GovernanceService
	Amendment       AmendmentService
	Policy          PolicyService
	PolicyRead      PolicyReadService
	Audit           AuditService
	AuditVerify     AuditVerificationService
	Anomalies       AnomalyService
	Reports         ReportingService
	DeniedAudit     DeniedAuditService
	LatestMigration int
}

// ErrInvalidCredentials indicates that login credentials do not match an enabled user.
var ErrInvalidCredentials = appauth.ErrInvalidCredentials

// NewRouter creates a router without a database readiness dependency.
func NewRouter(environment string, allowedOrigins []string) *gin.Engine {
	return buildRouter(environment, allowedOrigins, RouterDependencies{})
}

// NewRouterWithDependencySet creates a router with explicit typed dependencies.
func NewRouterWithDependencySet(environment string, allowedOrigins []string, dependencies RouterDependencies) *gin.Engine {
	return buildRouter(environment, allowedOrigins, dependencies)
}

func buildRouter(environment string, allowedOrigins []string, dependencies RouterDependencies) *gin.Engine {
	if environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	if dependencies.LatestMigration == 0 {
		dependencies.LatestMigration = 11
	}
	router := gin.New()
	router.Use(deniedAuditContext(dependencies.DeniedAudit), gin.Recovery(), corsMiddleware(allowedOrigins), requestID(), ErrorMappingMiddleware())
	router.GET("/health", health)
	router.GET("/ready", readyHandler(dependencies.Readiness, dependencies.LatestMigration))
	registerSessionRoutes(router, dependencies.Verifier, dependencies.Sessions, environment == "production")

	versioned := router.Group("/api/v1")
	registerSessionRoutes(versioned, dependencies.Verifier, dependencies.Sessions, environment == "production")
	reportingapi.RegisterRoutes(versioned, dependencies.Verifier, dependencies.Sessions, dependencies.Reports)
	shiftapi.RegisterRoutes(versioned, dependencies.Verifier, dependencies.Sessions, dependencies.Shift)
	shiftapi.RegisterReadRoutes(versioned, dependencies.Verifier, dependencies.Sessions, dependencies.ShiftRead)
	draftapi.RegisterRoutes(versioned, dependencies.Verifier, dependencies.Sessions, dependencies.Draft)
	draftapi.RegisterWriteRoutes(versioned, dependencies.Verifier, dependencies.Sessions, dependencies.DraftWrites)
	submissionapi.RegisterRoutes(versioned, dependencies.Verifier, dependencies.Sessions, dependencies.Submission)
	governanceapi.RegisterAcknowledgementRoutes(versioned, dependencies.Verifier, dependencies.Sessions, dependencies.Governance)
	governanceapi.RegisterAmendmentRoutes(versioned, dependencies.Verifier, dependencies.Sessions, dependencies.Amendment)
	policyapi.RegisterRoutes(versioned, dependencies.Verifier, dependencies.Sessions, dependencies.Policy)
	policyapi.RegisterHistoryRoutes(versioned, dependencies.Verifier, dependencies.Sessions, dependencies.PolicyRead)
	if dependencies.Audit != nil {
		auditapi.RegisterRoutes(versioned, dependencies.Verifier, dependencies.Sessions, dependencies.Audit)
	}
	if dependencies.AuditVerify != nil {
		auditapi.RegisterVerifyRoutes(versioned, dependencies.Verifier, dependencies.Sessions, dependencies.AuditVerify)
	}
	reportingapi.RegisterAnomalyRoutes(versioned, dependencies.Verifier, dependencies.Sessions, dependencies.Anomalies)
	return router
}

func registerSessionRoutes(router gin.IRoutes, verifier TokenVerifier, service SessionService, secure bool) {
	router.POST("/login", requireCSRF, loginHandler(service, secure))
	router.DELETE("/logout", AuthMiddleware(verifier), requireCSRF, logoutHandler(service, secure))
	router.GET("/session", AuthMiddleware(verifier), sessionHandler(service))
}

func health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func readyHandler(readiness Readiness, latestMigration int) gin.HandlerFunc {
	return func(c *gin.Context) {
		if readiness == nil || readiness.Ping(c.Request.Context()) != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unready"})
			return
		}
		current, err := readiness.MigrationsCurrent(c.Request.Context(), latestMigration)
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

func deniedAuditContext(service DeniedAuditService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if service != nil {
			c.Set("denied_audit", service)
		}
		c.Next()
	}
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
	writeError(c, http.StatusUnauthorized, reason)
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
		writeError(c, status, code)
	}
}

func domainHTTPError(err error) (int, string) {
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
		if service == nil {
			writeError(c, http.StatusInternalServerError, "internal_error")
			return
		}
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
		if service == nil {
			writeError(c, http.StatusInternalServerError, "internal_error")
			return
		}
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
