package main

import (
	"context"
	"log"
	"os"
	"time"

	gormstore "github.com/pomkita/pomkita-be/internal/adapter/persistence/gorm"
	"github.com/pomkita/pomkita-be/internal/config"
	appdb "github.com/pomkita/pomkita-be/internal/db"
	"github.com/pomkita/pomkita-be/internal/httpapi"
	appjwt "github.com/pomkita/pomkita-be/internal/jwt"
	authservice "github.com/pomkita/pomkita-be/internal/service/auth"
	draftservice "github.com/pomkita/pomkita-be/internal/service/draft"
	appreporting "github.com/pomkita/pomkita-be/internal/service/reporting"
	shiftservice "github.com/pomkita/pomkita-be/internal/service/shift"
	submissionservice "github.com/pomkita/pomkita-be/internal/service/submission"
)

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

func composeRouterDependencies(readiness httpapi.Readiness, verifier httpapi.TokenVerifier, sessions httpapi.SessionService, shifts httpapi.ShiftService, modernShift httpapi.ModernShiftService, modernDraft httpapi.ModernDraftService, modernSubmission httpapi.ModernSubmissionService, governance httpapi.GovernanceService, reporting httpapi.ReportingService, policy httpapi.PolicyRevisionService, reports httpapi.ModernReportingService) httpapi.RouterDependencies {
	return httpapi.RouterDependencies{
		Readiness: readiness, Verifier: verifier, Sessions: sessions,
		Shifts: shifts, ModernShift: modernShift, ModernDraft: modernDraft, ModernSubmission: modernSubmission, Governance: governance, Reporting: reporting,
		Reports: reports, Policy: policy, LatestMigration: 23,
	}
}

func main() {
	cfg := config.Load()
	ctx := context.Background()
	database, err := appdb.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Printf("open database: %v", err)
		os.Exit(1)
	}
	defer database.Close()
	database.SetJWTSecrets(cfg.JWTSecrets)
	database.SetJWTAudience(cfg.JWTAudience)
	migrator, err := appdb.NewMigrator(cfg.DatabaseURL, cfg.MigrationsDir)
	if err != nil {
		log.Printf("create migration runner: %v", err)
		os.Exit(1)
	}
	if err := migrator.Up(ctx); err != nil {
		log.Printf("apply migrations: %v", err)
		os.Exit(1)
	}
	gormDatabase, err := gormstore.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Printf("open GORM database: %v", err)
		os.Exit(1)
	}
	defer gormDatabase.Close()
	authRepository := gormstore.NewLegacyAuthRepository(gormDatabase, cfg.JWTSecrets)
	jwtService := appjwt.NewService(authRepository, appjwt.Config{
		Issuer: cfg.JWTIssuer, Audience: cfg.JWTAudience,
	})
	sessionService := authservice.NewService(authRepository, jwtService)
	modernReporting := appreporting.NewService(gormstore.NewReportingRepository(gormDatabase))
	modernShift := shiftservice.NewService(gormstore.NewShiftRepository(gormDatabase), systemClock{})
	modernDraft := draftservice.NewService(gormstore.NewDraftRepository(gormDatabase), systemClock{})
	modernSubmission := submissionservice.NewService(gormstore.NewSubmissionRepository(gormDatabase), systemClock{})
	router := httpapi.NewRouterWithDependencySet(cfg.Environment, cfg.CorsAllowedOrigins, composeRouterDependencies(
		database, jwtService, sessionService, appdb.NewShiftManager(database), modernShift, modernDraft, modernSubmission,
		appdb.NewGovernanceManager(database), appdb.NewReportingManager(database),
		appdb.NewPolicyManager(database), modernReporting,
	))

	if err := router.Run(":" + cfg.Port); err != nil {
		log.Printf("server stopped: %v", err)
		os.Exit(1)
	}
}
