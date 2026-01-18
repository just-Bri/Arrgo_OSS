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
	IncomingMoviesPath string
	IncomingTVPath     string
	TMDBAPIKey    string
	TVDBAPIKey    string
	OpenSubtitlesAPIKey string
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
		IncomingMoviesPath: getEnv("INCOMING_MOVIES_PATH", "/mnt/incoming/movies"),
		IncomingTVPath:     getEnv("INCOMING_TV_PATH", "/mnt/incoming/tv"),
		TMDBAPIKey:    getEnv("TMDB_API_KEY", ""),
		TVDBAPIKey:    getEnv("TVDB_API_KEY", ""),
		OpenSubtitlesAPIKey: getEnv("OPENSUBTITLES_API_KEY", ""),
		Debug:         getEnv("DEBUG", "false") == "true",
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

