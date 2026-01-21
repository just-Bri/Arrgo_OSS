package config

import (
	"fmt"
	"log/slog"

	"github.com/justbri/arrgo/shared/config"
)

type Config struct {
	DatabaseURL         string
	SessionSecret       string
	ServerPort          string
	Environment         string
	MoviesPath          string
	ShowsPath           string
	IncomingMoviesPath  string
	IncomingShowsPath   string
	TMDBAPIKey          string
	TVDBAPIKey          string
	OpenSubtitlesAPIKey string
	OpenSubtitlesUser   string
	OpenSubtitlesPass   string
	QBittorrentURL      string
	QBittorrentUser     string
	QBittorrentPass     string
	IndexerURL          string
	Debug               bool
}

func Load() *Config {
	cfg := &Config{
		DatabaseURL:         config.GetEnv("DATABASE_URL", "postgres://arrgo:arrgo@localhost:5432/arrgo?sslmode=disable"),
		SessionSecret:       config.GetEnv("SESSION_SECRET", "change-me-in-production"),
		ServerPort:          config.GetEnv("PORT", "5003"),
		Environment:         config.GetEnv("ENV", "development"),
		MoviesPath:          config.GetEnv("MOVIES_PATH", "/mnt/movies"),
		ShowsPath:           config.GetEnv("SHOWS_PATH", "/mnt/shows"),
		IncomingMoviesPath:  config.GetEnv("INCOMING_MOVIES_PATH", "/mnt/incoming/movies"),
		IncomingShowsPath:   config.GetEnv("INCOMING_SHOWS_PATH", "/mnt/incoming/shows"),
		TMDBAPIKey:          config.GetEnv("TMDB_API_KEY", ""),
		TVDBAPIKey:          config.GetEnv("TVDB_API_KEY", ""),
		OpenSubtitlesAPIKey: config.GetEnv("OPENSUBTITLES_API_KEY", ""),
		OpenSubtitlesUser:   config.GetEnv("OPENSUBTITLES_USER", ""),
		OpenSubtitlesPass:   config.GetEnv("OPENSUBTITLES_PASS", ""),
		QBittorrentURL:      config.GetEnv("QBITTORRENT_URL", "http://localhost:8080"),
		QBittorrentUser:     config.GetEnv("QBITTORRENT_USER", "admin"),
		QBittorrentPass:     config.GetEnv("QBITTORRENT_PASS", "adminadmin"),
		IndexerURL:          config.GetEnv("INDEXER_URL", "http://localhost:5004"),
		Debug:               config.GetEnv("DEBUG", "false") == "true",
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		slog.Warn("Configuration validation failed", "error", err)
	}

	return cfg
}

// Validate checks critical configuration values
func (c *Config) Validate() error {
	if c.SessionSecret == "change-me-in-production" && c.Environment == "production" {
		return fmt.Errorf("SESSION_SECRET must be changed in production")
	}
	if c.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	return nil
}
