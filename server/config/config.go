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
	ShowsPath     string
	IncomingMoviesPath string
	IncomingShowsPath  string
	TMDBAPIKey    string
	TVDBAPIKey    string
	OpenSubtitlesAPIKey string
	OpenSubtitlesUser string
	OpenSubtitlesPass string
	QBittorrentURL string
	QBittorrentUser string
	QBittorrentPass string
	IndexerURL    string
	Debug         bool
}

func Load() *Config {
	return &Config{
		DatabaseURL:   getEnv("DATABASE_URL", "postgres://arrgo:arrgo@localhost:5432/arrgo?sslmode=disable"),
		SessionSecret: getEnv("SESSION_SECRET", "change-me-in-production"),
		ServerPort:    getEnv("PORT", "5003"),
		Environment:   getEnv("ENV", "development"),
		MoviesPath:    getEnv("MOVIES_PATH", "/mnt/movies"),
		ShowsPath:     getEnv("SHOWS_PATH", "/mnt/shows"),
		IncomingMoviesPath: getEnv("INCOMING_MOVIES_PATH", "/mnt/incoming/movies"),
		IncomingShowsPath:  getEnv("INCOMING_SHOWS_PATH", "/mnt/incoming/shows"),
		TMDBAPIKey:    getEnv("TMDB_API_KEY", ""),
		TVDBAPIKey:    getEnv("TVDB_API_KEY", ""),
		OpenSubtitlesAPIKey: getEnv("OPENSUBTITLES_API_KEY", ""),
		OpenSubtitlesUser:   getEnv("OPENSUBTITLES_USER", ""),
		OpenSubtitlesPass:   getEnv("OPENSUBTITLES_PASS", ""),
		QBittorrentURL:  getEnv("QBITTORRENT_URL", "http://localhost:8080"),
		QBittorrentUser: getEnv("QBITTORRENT_USER", "admin"),
		QBittorrentPass: getEnv("QBITTORRENT_PASS", "adminadmin"),
		IndexerURL:     getEnv("INDEXER_URL", "http://localhost:5004"),
		Debug:         getEnv("DEBUG", "false") == "true",
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

