package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/pomkita/pomkita-be/internal/config"
	"github.com/pomkita/pomkita-be/internal/httpapi"
	accountapi "github.com/pomkita/pomkita-be/internal/httpapi/account"
	auditapi "github.com/pomkita/pomkita-be/internal/httpapi/audit"
	draftapi "github.com/pomkita/pomkita-be/internal/httpapi/draft"
	governanceapi "github.com/pomkita/pomkita-be/internal/httpapi/governance"
	policyapi "github.com/pomkita/pomkita-be/internal/httpapi/policy"
	reportingapi "github.com/pomkita/pomkita-be/internal/httpapi/reporting"
	sessionapi "github.com/pomkita/pomkita-be/internal/httpapi/session"
	shiftapi "github.com/pomkita/pomkita-be/internal/httpapi/shift"
	submissionapi "github.com/pomkita/pomkita-be/internal/httpapi/submission"
	"github.com/pomkita/pomkita-be/internal/httpapi/transport"
	appjwt "github.com/pomkita/pomkita-be/internal/jwt"
	"github.com/pomkita/pomkita-be/internal/platform/mailer"
	migrations "github.com/pomkita/pomkita-be/internal/platform/migrations"
	accountrepository "github.com/pomkita/pomkita-be/internal/repository/account"
	auditrepository "github.com/pomkita/pomkita-be/internal/repository/audit"
	authrepository "github.com/pomkita/pomkita-be/internal/repository/auth"
	draftrepository "github.com/pomkita/pomkita-be/internal/repository/draft"
	governancerepository "github.com/pomkita/pomkita-be/internal/repository/governance"
	identityrepository "github.com/pomkita/pomkita-be/internal/repository/identity"
	organizationrepository "github.com/pomkita/pomkita-be/internal/repository/organization"
	policyrepository "github.com/pomkita/pomkita-be/internal/repository/policy"
	reportingrepository "github.com/pomkita/pomkita-be/internal/repository/reporting"
	shiftrepository "github.com/pomkita/pomkita-be/internal/repository/shift"
	stationrepository "github.com/pomkita/pomkita-be/internal/repository/station"
	store "github.com/pomkita/pomkita-be/internal/repository/store"
	submissionrepository "github.com/pomkita/pomkita-be/internal/repository/submission"
	accountservice "github.com/pomkita/pomkita-be/internal/service/account"
	auditservice "github.com/pomkita/pomkita-be/internal/service/audit"
	authservice "github.com/pomkita/pomkita-be/internal/service/auth"
	draftservice "github.com/pomkita/pomkita-be/internal/service/draft"
	governanceservice "github.com/pomkita/pomkita-be/internal/service/governance"
	identityservice "github.com/pomkita/pomkita-be/internal/service/identity"
	organizationservice "github.com/pomkita/pomkita-be/internal/service/organization"
	policysservice "github.com/pomkita/pomkita-be/internal/service/policy"
	appreporting "github.com/pomkita/pomkita-be/internal/service/reporting"
	shiftservice "github.com/pomkita/pomkita-be/internal/service/shift"
	stationservice "github.com/pomkita/pomkita-be/internal/service/station"
	submissionservice "github.com/pomkita/pomkita-be/internal/service/submission"
)

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

func composeRouterDependencies(readiness httpapi.Readiness, verifier transport.TokenVerifier, sessions sessionapi.Service, shiftService shiftapi.ShiftService, shiftRead shiftapi.ShiftReadService, draftService draftapi.DraftService, draftWrites draftapi.DraftWriteService, submissionService submissionapi.SubmissionService, governanceService governanceapi.GovernanceService, amendment governanceapi.AmendmentService, policyService policyapi.PolicyService, policyRead policyapi.PolicyReadService, auditService auditapi.AuditService, auditVerify auditapi.AuditVerificationService, anomalies reportingapi.AnomalyService, reports reportingapi.ReportingService) httpapi.RouterDependencies {
	return httpapi.RouterDependencies{
		Readiness: readiness, Verifier: verifier, Sessions: sessions,
		Shift: shiftService, ShiftRead: shiftRead, Draft: draftService, DraftWrites: draftWrites, Submission: submissionService, Governance: governanceService, Amendment: amendment, Policy: policyService, PolicyRead: policyRead, Audit: auditService, AuditVerify: auditVerify, Anomalies: anomalies,
		Reports: reports, LatestMigration: 15,
	}
}

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
	identityService := identityservice.NewService(identityrepository.NewIdentityRepository(database), messageSender, systemClock{}, cfg.PublicBaseURL)
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
	dependencies := composeRouterDependencies(
		database, jwtService, sessionService, shiftService, shiftService, draftService, draftService, submissionService, governanceService, amendmentService, policyService, policyService, reportingService, auditService, reportingService, reportingService,
	)
	dependencies.DeniedAudit = deniedAudit
	dependencies.Users = identityService
	dependencies.Account = accountService
	dependencies.Organization = organizationService
	dependencies.Station = stationService
	dependencies.AccountLimiter = accountapi.NewLimiter(5, time.Minute, time.Now)
	router := httpapi.NewRouterWithDependencySet(cfg.Environment, cfg.CorsAllowedOrigins, dependencies)

	if err := router.Run(":" + cfg.Port); err != nil {
		log.Printf("server stopped: %v", err)
		os.Exit(1)
	}
}
