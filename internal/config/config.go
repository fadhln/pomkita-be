package config

import "os"

// Config contains process configuration.
type Config struct {
	Port          string
	Environment   string
	DatabaseURL   string
	MigrationsDir string
	JWTIssuer     string
	JWTAudience   string
	JWTSecrets    map[string]string
}

func Load() Config {
	return Config{
		Port:          valueOrDefault("PORT", "8080"),
		Environment:   valueOrDefault("ENVIRONMENT", "development"),
		DatabaseURL:   valueOrDefault("DATABASE_URL", "postgres://pomkita:pomkita_dev@127.0.0.1:5432/pomkita?sslmode=disable"),
		MigrationsDir: valueOrDefault("MIGRATIONS_DIR", "migrations"),
		JWTIssuer:     valueOrDefault("JWT_ISSUER", "pomkita"),
		JWTAudience:   valueOrDefault("JWT_AUDIENCE", "pomkita"),
		JWTSecrets:    jwtSecrets(),
	}
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
