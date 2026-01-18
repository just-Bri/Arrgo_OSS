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
	TVShowsPath   string
	IncomingPath  string
	TMDBAPIKey    string
	TVDBAPIKey    string
	Debug         bool
}

func Load() *Config {
	return &Config{
		DatabaseURL:   getEnv("DATABASE_URL", "postgres://arrgo:arrgo@localhost:5432/arrgo?sslmode=disable"),
		SessionSecret: getEnv("SESSION_SECRET", "change-me-in-production"),
		ServerPort:    getEnv("PORT", "5003"),
		Environment:   getEnv("ENV", "development"),
		MoviesPath:    getEnv("MOVIES_PATH", "/mnt/movies"),
		TVShowsPath:   getEnv("TV_SHOWS_PATH", "/mnt/tv"),
		IncomingPath:  getEnv("INCOMING_PATH", "/mnt/incoming"),
		TMDBAPIKey:    getEnv("TMDB_API_KEY", ""),
		TVDBAPIKey:    getEnv("TVDB_API_KEY", ""),
		Debug:         getEnv("DEBUG", "false") == "true",
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

