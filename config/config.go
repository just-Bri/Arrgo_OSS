package config

import (
	"os"
)

type Config struct {
	DatabaseURL   string
	SessionSecret string
	ServerPort    string
	Environment   string
	MoviesPath    string
}

func Load() *Config {
	return &Config{
		DatabaseURL:   getEnv("DATABASE_URL", "postgres://arrgo:arrgo@localhost:5432/arrgo?sslmode=disable"),
		SessionSecret: getEnv("SESSION_SECRET", "change-me-in-production"),
		ServerPort:    getEnv("PORT", "8080"),
		Environment:   getEnv("ENV", "development"),
		MoviesPath:    getEnv("MOVIES_PATH", "/mnt/user/media/movies"),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

