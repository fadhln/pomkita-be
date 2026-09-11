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
