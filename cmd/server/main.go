package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/fadhln/pomkita-be/internal/config"
	"github.com/fadhln/pomkita-be/internal/httpapi"
	accountapi "github.com/fadhln/pomkita-be/internal/httpapi/account"
	appjwt "github.com/fadhln/pomkita-be/internal/jwt"
	"github.com/fadhln/pomkita-be/internal/platform/mailer"
	migrations "github.com/fadhln/pomkita-be/internal/platform/migrations"
	accountrepository "github.com/fadhln/pomkita-be/internal/repository/account"
	auditrepository "github.com/fadhln/pomkita-be/internal/repository/audit"
	authrepository "github.com/fadhln/pomkita-be/internal/repository/auth"
	draftrepository "github.com/fadhln/pomkita-be/internal/repository/draft"
	governancerepository "github.com/fadhln/pomkita-be/internal/repository/governance"
	identityrepository "github.com/fadhln/pomkita-be/internal/repository/identity"
	organizationrepository "github.com/fadhln/pomkita-be/internal/repository/organization"
	policyrepository "github.com/fadhln/pomkita-be/internal/repository/policy"
	reportingrepository "github.com/fadhln/pomkita-be/internal/repository/reporting"
	shiftrepository "github.com/fadhln/pomkita-be/internal/repository/shift"
	stationrepository "github.com/fadhln/pomkita-be/internal/repository/station"
	store "github.com/fadhln/pomkita-be/internal/repository/store"
	submissionrepository "github.com/fadhln/pomkita-be/internal/repository/submission"
	accountservice "github.com/fadhln/pomkita-be/internal/service/account"
	auditservice "github.com/fadhln/pomkita-be/internal/service/audit"
	authservice "github.com/fadhln/pomkita-be/internal/service/auth"
	draftservice "github.com/fadhln/pomkita-be/internal/service/draft"
	governanceservice "github.com/fadhln/pomkita-be/internal/service/governance"
	identityservice "github.com/fadhln/pomkita-be/internal/service/identity"
	organizationservice "github.com/fadhln/pomkita-be/internal/service/organization"
	policysservice "github.com/fadhln/pomkita-be/internal/service/policy"
	appreporting "github.com/fadhln/pomkita-be/internal/service/reporting"
	shiftservice "github.com/fadhln/pomkita-be/internal/service/shift"
	stationservice "github.com/fadhln/pomkita-be/internal/service/station"
	submissionservice "github.com/fadhln/pomkita-be/internal/service/submission"
)

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

func main() {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Printf("invalid configuration: %v", err)
		os.Exit(1)
	}
	ctx := context.Background()
	migrator, err := migrations.New(cfg.DatabaseURL, cfg.MigrationsDir)
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
	var messageSender mailer.Mailer
	if cfg.Mailer == "resend" {
		messageSender = mailer.NewResend(cfg.ResendAPIKey, cfg.MailFrom)
	} else {
		messageSender = mailer.NewSpool(cfg.MailSpoolDirectory)
	}
	identityRepository := identityrepository.NewIdentityRepository(database)
	identityService := identityservice.NewService(identityRepository, identityRepository, messageSender, systemClock{}, cfg.PublicBaseURL)
	accountService := accountservice.NewService(accountrepository.NewRepository(database), systemClock{}, messageSender, cfg.PublicBaseURL)
	organizationService := organizationservice.NewService(organizationrepository.NewRepository(database), systemClock{})
	stationService := stationservice.NewService(stationrepository.NewRepository(database), systemClock{})
	reportingService := appreporting.NewService(reportingrepository.NewReportingRepository(database))
	shiftService := shiftservice.NewService(shiftrepository.NewShiftRepository(database), systemClock{})
	draftService := draftservice.NewService(draftrepository.NewDraftRepository(database), systemClock{})
	submissionService := submissionservice.NewService(submissionrepository.NewSubmissionRepository(database), systemClock{})
	governanceService := governanceservice.NewService(governancerepository.NewGovernanceRepository(database), systemClock{})
	amendmentService := governanceservice.NewAmendmentService(governancerepository.NewGovernanceRepository(database), systemClock{})
	policyService := policysservice.NewService(policyrepository.NewPolicyRepository(database), systemClock{})
	auditRepository := auditrepository.NewAuditRepository(database)
	auditService := auditservice.NewService(auditRepository, systemClock{})
	deniedAudit := auditservice.NewDeniedService(auditRepository, systemClock{})
	dependencies := httpapi.RouterDependencies{
		Readiness: database, Verifier: jwtService, Sessions: sessionService,
		Shift: shiftService, ShiftRead: shiftService, Draft: draftService, DraftWrites: draftService,
		Submission: submissionService, Governance: governanceService, Amendment: amendmentService,
		Policy: policyService, PolicyRead: policyService, Audit: reportingService, AuditVerify: auditService,
		Anomalies: reportingService, Reports: reportingService, DeniedAudit: deniedAudit,
		Users: identityService, Account: accountService, Organization: organizationService, Station: stationService,
		AccountLimiter: accountapi.NewLimiter(5, time.Minute, time.Now), LatestMigration: 18,
	}
	router := httpapi.NewRouterWithDependencySet(cfg.Environment, cfg.CorsAllowedOrigins, dependencies)

	if err := router.Run(":" + cfg.Port); err != nil {
		log.Printf("server stopped: %v", err)
		os.Exit(1)
	}
}
