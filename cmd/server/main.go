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
	authservice "github.com/pomkita/pomkita-be/internal/service/auth"
	draftservice "github.com/pomkita/pomkita-be/internal/service/draft"
	governanceservice "github.com/pomkita/pomkita-be/internal/service/governance"
	appreporting "github.com/pomkita/pomkita-be/internal/service/reporting"
	shiftservice "github.com/pomkita/pomkita-be/internal/service/shift"
	submissionservice "github.com/pomkita/pomkita-be/internal/service/submission"
)

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

func composeRouterDependencies(readiness httpapi.Readiness, verifier httpapi.TokenVerifier, sessions httpapi.SessionService, shifts httpapi.ShiftService, modernShift httpapi.ModernShiftService, modernDraft httpapi.ModernDraftService, modernSubmission httpapi.ModernSubmissionService, modernGovernance httpapi.ModernGovernanceService, governance httpapi.GovernanceService, reporting httpapi.ReportingService, policy httpapi.PolicyRevisionService, reports httpapi.ModernReportingService) httpapi.RouterDependencies {
	return httpapi.RouterDependencies{
		Readiness: readiness, Verifier: verifier, Sessions: sessions,
		Shifts: shifts, ModernShift: modernShift, ModernDraft: modernDraft, ModernSubmission: modernSubmission, ModernGovernance: modernGovernance, Governance: governance, Reporting: reporting,
		Reports: reports, Policy: policy, LatestMigration: 11,
	}
}

func main() {
	cfg := config.Load()
	ctx := context.Background()
	migrator, err := cleanmigrations.New(cfg.DatabaseURL, filepath.Join(cfg.MigrationsDir, "clean"))
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
	router := httpapi.NewRouterWithDependencySet(cfg.Environment, cfg.CorsAllowedOrigins, composeRouterDependencies(
		database, jwtService, sessionService, nil, modernShift, modernDraft, modernSubmission, modernGovernance,
		nil, nil, nil, modernReporting,
	))

	if err := router.Run(":" + cfg.Port); err != nil {
		log.Printf("server stopped: %v", err)
		os.Exit(1)
	}
}
