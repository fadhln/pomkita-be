package config

import "os"

type Config struct {
	Port        string
	Environment string
}

func Load() Config {
	return Config{
		Port:        valueOrDefault("PORT", "8080"),
		Environment: valueOrDefault("ENVIRONMENT", "development"),
	}
}

func valueOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
