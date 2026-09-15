package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/pomkita/pomkita-be/internal/config"
	"github.com/pomkita/pomkita-be/internal/httpapi"
	appjwt "github.com/pomkita/pomkita-be/internal/jwt"
	cleanmigrations "github.com/pomkita/pomkita-be/internal/platform/migrations"
	auditrepository "github.com/pomkita/pomkita-be/internal/repository/audit"
	authrepository "github.com/pomkita/pomkita-be/internal/repository/auth"
	draftrepository "github.com/pomkita/pomkita-be/internal/repository/draft"
	governancerepository "github.com/pomkita/pomkita-be/internal/repository/governance"
	policyrepository "github.com/pomkita/pomkita-be/internal/repository/policy"
	reportingrepository "github.com/pomkita/pomkita-be/internal/repository/reporting"
	shiftrepository "github.com/pomkita/pomkita-be/internal/repository/shift"
	store "github.com/pomkita/pomkita-be/internal/repository/store"
	submissionrepository "github.com/pomkita/pomkita-be/internal/repository/submission"
	auditservice "github.com/pomkita/pomkita-be/internal/service/audit"
	authservice "github.com/pomkita/pomkita-be/internal/service/auth"
	draftservice "github.com/pomkita/pomkita-be/internal/service/draft"
	governanceservice "github.com/pomkita/pomkita-be/internal/service/governance"
	policysservice "github.com/pomkita/pomkita-be/internal/service/policy"
	appreporting "github.com/pomkita/pomkita-be/internal/service/reporting"
	shiftservice "github.com/pomkita/pomkita-be/internal/service/shift"
	submissionservice "github.com/pomkita/pomkita-be/internal/service/submission"
)

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

func composeRouterDependencies(readiness httpapi.Readiness, verifier httpapi.TokenVerifier, sessions httpapi.SessionService, Shift httpapi.ShiftService, ShiftRead httpapi.ShiftReadService, Draft httpapi.DraftService, DraftWrites httpapi.DraftWriteService, Submission httpapi.SubmissionService, Governance httpapi.GovernanceService, Amendment httpapi.AmendmentService, Policy httpapi.PolicyService, PolicyRead httpapi.PolicyReadService, Audit httpapi.AuditService, AuditVerify httpapi.AuditVerificationService, Anomalies httpapi.AnomalyService, reports httpapi.ReportingService) httpapi.RouterDependencies {
	return httpapi.RouterDependencies{
		Readiness: readiness, Verifier: verifier, Sessions: sessions,
		Shift: Shift, ShiftRead: ShiftRead, Draft: Draft, DraftWrites: DraftWrites, Submission: Submission, Governance: Governance, Amendment: Amendment, Policy: Policy, PolicyRead: PolicyRead, Audit: Audit, AuditVerify: AuditVerify, Anomalies: Anomalies,
		Reports: reports, LatestMigration: 11,
	}
}

func main() {
	cfg := config.Load()
	ctx := context.Background()
	migrator, err := cleanmigrations.New(cfg.DatabaseURL, cfg.MigrationsDir)
	if err != nil {
		log.Printf("create migration runner: %v", err)
		os.Exit(1)
	}
	if err := migrator.Up(ctx); err != nil {
		log.Printf("apply migrations: %v", err)
		os.Exit(1)
	}
	database, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Printf("open GORM database: %v", err)
		os.Exit(1)
	}
	defer database.Close()
	authRepository := authrepository.NewAuthRepository(database, cfg.JWTSecrets)
	jwtService := appjwt.NewService(authRepository, appjwt.Config{
		Issuer: cfg.JWTIssuer, Audience: cfg.JWTAudience,
	})
	sessionService := authservice.NewService(authRepository, jwtService)
	Reporting := appreporting.NewService(reportingrepository.NewReportingRepository(database))
	Shift := shiftservice.NewService(shiftrepository.NewShiftRepository(database), systemClock{})
	Draft := draftservice.NewService(draftrepository.NewDraftRepository(database), systemClock{})
	Submission := submissionservice.NewService(submissionrepository.NewSubmissionRepository(database), systemClock{})
	Governance := governanceservice.NewService(governancerepository.NewGovernanceRepository(database), systemClock{})
	Amendment := governanceservice.NewAmendmentService(governancerepository.NewGovernanceRepository(database), systemClock{})
	Policy := policysservice.NewService(policyrepository.NewPolicyRepository(database), systemClock{})
	auditRepository := auditrepository.NewAuditRepository(database)
	Audit := auditservice.NewService(auditRepository, systemClock{})
	deniedAudit := auditservice.NewDeniedService(auditRepository, systemClock{})
	dependencies := composeRouterDependencies(
		database, jwtService, sessionService, Shift, Shift, Draft, Draft, Submission, Governance, Amendment, Policy, Policy, Reporting, Audit, Reporting, Reporting,
	)
	dependencies.DeniedAudit = deniedAudit
	router := httpapi.NewRouterWithDependencySet(cfg.Environment, cfg.CorsAllowedOrigins, dependencies)

	if err := router.Run(":" + cfg.Port); err != nil {
		log.Printf("server stopped: %v", err)
		os.Exit(1)
	}
}
