package main

import (
	"context"
	"log"
	"os"

	gormstore "github.com/pomkita/pomkita-be/internal/adapter/persistence/gorm"
	"github.com/pomkita/pomkita-be/internal/config"
	appdb "github.com/pomkita/pomkita-be/internal/db"
	"github.com/pomkita/pomkita-be/internal/httpapi"
	appjwt "github.com/pomkita/pomkita-be/internal/jwt"
	authservice "github.com/pomkita/pomkita-be/internal/service/auth"
)

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
	router := httpapi.NewRouterWithDependencySet(cfg.Environment, cfg.CorsAllowedOrigins, httpapi.RouterDependencies{
		Readiness:       database,
		Verifier:        jwtService,
		Sessions:        sessionService,
		Shifts:          appdb.NewShiftManager(database),
		Governance:      appdb.NewGovernanceManager(database),
		Reporting:       appdb.NewReportingManager(database),
		Policy:          appdb.NewPolicyManager(database),
		LatestMigration: 23,
	})

	if err := router.Run(":" + cfg.Port); err != nil {
		log.Printf("server stopped: %v", err)
		os.Exit(1)
	}
}
