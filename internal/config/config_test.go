package config

import "testing"

func TestLoadUsesB0DatabaseAndJWTDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("MIGRATIONS_DIR", "")
	t.Setenv("JWT_ISSUER", "")
	t.Setenv("JWT_AUDIENCE", "")
	cfg := Load()
	if cfg.DatabaseURL != "postgres://pomkita:pomkita_dev@127.0.0.1:5432/pomkita?sslmode=disable" {
		t.Fatalf("database URL: got %q", cfg.DatabaseURL)
	}
	if cfg.MigrationsDir != "migrations" {
		t.Fatalf("migrations directory: got %q", cfg.MigrationsDir)
	}
	if cfg.JWTIssuer != "pomkita" || cfg.JWTAudience != "pomkita" {
		t.Fatalf("JWT defaults: issuer=%q audience=%q", cfg.JWTIssuer, cfg.JWTAudience)
	}
}

func TestLoadParsesAllowedCORSOrigins(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", " http://localhost:3100, http://localhost:3101,,")

	cfg := Load()

	want := []string{"http://localhost:3100", "http://localhost:3101"}
	if len(cfg.CorsAllowedOrigins) != len(want) {
		t.Fatalf("allowed origins: got %#v, want %#v", cfg.CorsAllowedOrigins, want)
	}
	for i := range want {
		if cfg.CorsAllowedOrigins[i] != want[i] {
			t.Fatalf("allowed origins: got %#v, want %#v", cfg.CorsAllowedOrigins, want)
		}
	}
}

func TestLoadLeavesCORSDisabledWhenOriginsAreEmpty(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "")

	if origins := Load().CorsAllowedOrigins; len(origins) != 0 {
		t.Fatalf("allowed origins: got %#v, want empty", origins)
	}
}

func TestConfig_RejectsIncompleteResendProductionConfiguration(t *testing.T) {
	cfg := Config{Environment: "production", Mailer: "resend", PublicBaseURL: "https://app.example.test"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate accepted missing Resend configuration")
	}
}

func TestConfig_AcceptsSpoolDevelopmentConfiguration(t *testing.T) {
	cfg := Config{Environment: "development", Mailer: "spool", PublicBaseURL: "http://localhost:3000", MailSpoolDirectory: "var/spool/mail"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate rejected spool configuration: %v", err)
	}
}
