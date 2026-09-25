package config

import (
	"errors"
	"os"
	"strings"
)

// Config contains process configuration.
type Config struct {
	Port               string
	Environment        string
	DatabaseURL        string
	MigrationsDir      string
	JWTIssuer          string
	JWTAudience        string
	JWTSecrets         map[string]string
	CorsAllowedOrigins []string
	Mailer             string
	MailFrom           string
	ResendAPIKey       string
	PublicBaseURL      string
	MailSpoolDirectory string
}

func Load() Config {
	environment := valueOrDefault("ENVIRONMENT", "development")
	mailerName := valueOrDefault("POMKITA_MAILER", "spool")
	mailFrom := os.Getenv("POMKITA_MAIL_FROM")

	if mailFrom == "" && !(environment == "production" && mailerName == "resend") {
		mailFrom = "noreply@localhost"
	}

	publicBaseURL := os.Getenv("POMKITA_PUBLIC_BASE_URL")

	if publicBaseURL == "" && environment != "production" {
		publicBaseURL = "http://localhost:3000"
	}

	return Config{
		Port:               valueOrDefault("PORT", "8080"),
		Environment:        environment,
		DatabaseURL:        valueOrDefault("DATABASE_URL", "postgres://pomkita:pomkita_dev@127.0.0.1:5432/pomkita?sslmode=disable"),
		MigrationsDir:      valueOrDefault("MIGRATIONS_DIR", "migrations"),
		JWTIssuer:          valueOrDefault("JWT_ISSUER", "pomkita"),
		JWTAudience:        valueOrDefault("JWT_AUDIENCE", "pomkita"),
		JWTSecrets:         jwtSecrets(),
		CorsAllowedOrigins: corsAllowedOrigins(),
		Mailer:             mailerName,
		MailFrom:           mailFrom,
		ResendAPIKey:       os.Getenv("POMKITA_RESEND_API_KEY"),
		PublicBaseURL:      publicBaseURL,
		MailSpoolDirectory: valueOrDefault("POMKITA_MAIL_SPOOL_DIR", "var/spool/mail"),
	}
}

// Validate checks configuration that must be valid before process start.
func (c Config) Validate() error {
	if c.Mailer != "spool" && c.Mailer != "resend" {
		return errors.New("POMKITA_MAILER must be spool or resend")
	}
	if c.Environment == "production" && strings.TrimSpace(c.PublicBaseURL) == "" {
		return errors.New("POMKITA_PUBLIC_BASE_URL is required")
	}
	if c.Mailer == "resend" {
		if strings.TrimSpace(c.MailFrom) == "" {
			return errors.New("POMKITA_MAIL_FROM is required for resend")
		}
		if strings.TrimSpace(c.ResendAPIKey) == "" {
			return errors.New("POMKITA_RESEND_API_KEY is required for resend")
		}
	}
	if c.Mailer == "spool" && strings.TrimSpace(c.MailSpoolDirectory) == "" {
		return errors.New("mail spool directory is required for spool")
	}
	return nil
}

func corsAllowedOrigins() []string {
	values := strings.Split(os.Getenv("CORS_ALLOWED_ORIGINS"), ",")
	origins := make([]string, 0, len(values))
	for _, value := range values {
		if origin := strings.TrimSpace(value); origin != "" {
			origins = append(origins, origin)
		}
	}
	return origins
}

func jwtSecrets() map[string]string {
	secrets := make(map[string]string)
	if value := os.Getenv("JWT_SECRET_KEY_1"); value != "" {
		secrets["app.jwt_secret.key_1"] = value
	}
	return secrets
}

func valueOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
