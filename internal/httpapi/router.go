package httpapi

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	auditapi "github.com/pomkita/pomkita-be/internal/httpapi/audit"
	draftapi "github.com/pomkita/pomkita-be/internal/httpapi/draft"
	governanceapi "github.com/pomkita/pomkita-be/internal/httpapi/governance"
	policyapi "github.com/pomkita/pomkita-be/internal/httpapi/policy"
	reportingapi "github.com/pomkita/pomkita-be/internal/httpapi/reporting"
	sessionapi "github.com/pomkita/pomkita-be/internal/httpapi/session"
	shiftapi "github.com/pomkita/pomkita-be/internal/httpapi/shift"
	submissionapi "github.com/pomkita/pomkita-be/internal/httpapi/submission"
	"github.com/pomkita/pomkita-be/internal/httpapi/transport"
)

// Readiness checks database reachability and migration state.
type Readiness interface {
	Ping(context.Context) error
	MigrationsCurrent(context.Context, int) (bool, error)
}

// RouterDependencies contains all services required by the HTTP adapter.
type RouterDependencies struct {
	Readiness       Readiness
	Verifier        transport.TokenVerifier
	Sessions        sessionapi.Service
	Shift           shiftapi.ShiftService
	ShiftRead       shiftapi.ShiftReadService
	Draft           draftapi.DraftService
	DraftWrites     draftapi.DraftWriteService
	Submission      submissionapi.SubmissionService
	Governance      governanceapi.GovernanceService
	Amendment       governanceapi.AmendmentService
	Policy          policyapi.PolicyService
	PolicyRead      policyapi.PolicyReadService
	Audit           auditapi.AuditService
	AuditVerify     auditapi.AuditVerificationService
	Anomalies       reportingapi.AnomalyService
	Reports         reportingapi.ReportingService
	DeniedAudit     transport.DeniedAuditService
	LatestMigration int
}

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
	router.Use(
		transport.DeniedAuditContext(dependencies.DeniedAudit),
		gin.Recovery(),
		transport.CORSMiddleware(allowedOrigins),
		transport.RequestID(),
		transport.ErrorMappingMiddleware(),
	)
	router.GET("/health", health)
	router.GET("/ready", readyHandler(dependencies.Readiness, dependencies.LatestMigration))
	sessionapi.RegisterRoutes(router, dependencies.Verifier, dependencies.Sessions, environment == "production")

	versioned := router.Group("/api/v1")
	sessionapi.RegisterRoutes(versioned, dependencies.Verifier, dependencies.Sessions, environment == "production")
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
