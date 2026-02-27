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
	EnableSubSync       bool
	SubSyncURL          string
	Debug               bool
}

func Load() *Config {
	cfg := &Config{
		DatabaseURL:         config.GetEnv("DATABASE_URL", ""),
		SessionSecret:       config.GetEnv("SESSION_SECRET", ""),
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
		QBittorrentUser:     config.GetEnv("QBITTORRENT_USER", ""),
		QBittorrentPass:     config.GetEnv("QBITTORRENT_PASS", ""),
		EnableSubSync:       config.GetEnv("ENABLE_SUBSYNC", "false") == "true",
		SubSyncURL:          config.GetEnv("FFSUBSYNC_URL", "http://ffsubsync-api:8080"),
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
	if c.SessionSecret == "" {
		return fmt.Errorf("SESSION_SECRET is required")
	}
	if c.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	if c.QBittorrentUser == "" {
		return fmt.Errorf("QBITTORRENT_USER is required")
	}
	if c.QBittorrentPass == "" {
		return fmt.Errorf("QBITTORRENT_PASS is required")
	}
	return nil
}
