package main

import (
	"context"
	"log"
	"os"

	"github.com/pomkita/pomkita-be/internal/config"
	appdb "github.com/pomkita/pomkita-be/internal/db"
	"github.com/pomkita/pomkita-be/internal/httpapi"
	appjwt "github.com/pomkita/pomkita-be/internal/jwt"
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
	jwtService := appjwt.NewService(database.JWTStore(cfg.JWTSecrets), appjwt.Config{
		Issuer: cfg.JWTIssuer, Audience: cfg.JWTAudience,
	})
	sessionManager := appdb.NewSessionManager(database, jwtService)
	router := httpapi.NewRouterWithAllDependencies(cfg.Environment, cfg.CorsAllowedOrigins, database, jwtService, sessionManager, appdb.NewShiftManager(database), appdb.NewGovernanceManager(database), appdb.NewReportingManager(database), appdb.NewPolicyManager(database))

	if err := router.Run(":" + cfg.Port); err != nil {
		log.Printf("server stopped: %v", err)
		os.Exit(1)
	}
}
