package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"

	gormstore "github.com/pomkita/pomkita-be/internal/adapter/persistence/gorm"
	"github.com/pomkita/pomkita-be/internal/config"
	"github.com/pomkita/pomkita-be/internal/httpapi"
	appjwt "github.com/pomkita/pomkita-be/internal/jwt"
	cleanmigrations "github.com/pomkita/pomkita-be/internal/platform/migrations"
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

func cleanMigrationsDirectory(value string) string {
	if filepath.Base(filepath.Clean(value)) == "clean" {
		return value
	}
	return filepath.Join(value, "clean")
}

func composeRouterDependencies(readiness httpapi.Readiness, verifier httpapi.TokenVerifier, sessions httpapi.SessionService, shifts httpapi.ShiftService, modernShift httpapi.ModernShiftService, modernShiftRead httpapi.ModernShiftReadService, modernDraft httpapi.ModernDraftService, modernDraftWrites httpapi.ModernDraftWriteService, modernSubmission httpapi.ModernSubmissionService, modernGovernance httpapi.ModernGovernanceService, modernAmendment httpapi.ModernAmendmentService, modernPolicy httpapi.ModernPolicyService, modernPolicyRead httpapi.ModernPolicyReadService, modernAudit httpapi.ModernAuditService, modernAuditVerify httpapi.ModernAuditVerificationService, modernAnomalies httpapi.ModernAnomalyService, governance httpapi.GovernanceService, reporting httpapi.ReportingService, policy httpapi.PolicyRevisionService, reports httpapi.ModernReportingService) httpapi.RouterDependencies {
	return httpapi.RouterDependencies{
		Readiness: readiness, Verifier: verifier, Sessions: sessions,
		Shifts: shifts, ModernShift: modernShift, ModernShiftRead: modernShiftRead, ModernDraft: modernDraft, ModernDraftWrites: modernDraftWrites, ModernSubmission: modernSubmission, ModernGovernance: modernGovernance, ModernAmendment: modernAmendment, ModernPolicy: modernPolicy, ModernPolicyRead: modernPolicyRead, ModernAudit: modernAudit, ModernAuditVerify: modernAuditVerify, ModernAnomalies: modernAnomalies, Governance: governance, Reporting: reporting,
		Reports: reports, Policy: policy, LatestMigration: 11,
	}
}

func main() {
	cfg := config.Load()
	ctx := context.Background()
	migrator, err := cleanmigrations.New(cfg.DatabaseURL, cleanMigrationsDirectory(cfg.MigrationsDir))
	if err != nil {
		log.Printf("create migration runner: %v", err)
		os.Exit(1)
	}
	if err := migrator.Up(ctx); err != nil {
		log.Printf("apply migrations: %v", err)
		os.Exit(1)
	}
	database, err := gormstore.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Printf("open GORM database: %v", err)
		os.Exit(1)
	}
	defer database.Close()
	authRepository := gormstore.NewAuthRepository(database, cfg.JWTSecrets)
	jwtService := appjwt.NewService(authRepository, appjwt.Config{
		Issuer: cfg.JWTIssuer, Audience: cfg.JWTAudience,
	})
	sessionService := authservice.NewService(authRepository, jwtService)
	modernReporting := appreporting.NewService(gormstore.NewReportingRepository(database))
	modernShift := shiftservice.NewService(gormstore.NewShiftRepository(database), systemClock{})
	modernDraft := draftservice.NewService(gormstore.NewDraftRepository(database), systemClock{})
	modernSubmission := submissionservice.NewService(gormstore.NewSubmissionRepository(database), systemClock{})
	modernGovernance := governanceservice.NewService(gormstore.NewGovernanceRepository(database), systemClock{})
	modernAmendment := governanceservice.NewAmendmentService(gormstore.NewGovernanceRepository(database), systemClock{})
	modernPolicy := policysservice.NewService(gormstore.NewPolicyRepository(database), systemClock{})
	modernAudit := auditservice.NewService(gormstore.NewAuditRepository(database), systemClock{})
	router := httpapi.NewRouterWithDependencySet(cfg.Environment, cfg.CorsAllowedOrigins, composeRouterDependencies(
		database, jwtService, sessionService, nil, modernShift, modernShift, modernDraft, modernDraft, modernSubmission, modernGovernance, modernAmendment, modernPolicy, modernPolicy, modernReporting, modernAudit, modernReporting,
		nil, nil, nil, modernReporting,
	))

	if err := router.Run(":" + cfg.Port); err != nil {
		log.Printf("server stopped: %v", err)
		os.Exit(1)
	}
}
