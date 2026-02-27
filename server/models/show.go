package models

import "time"

type Show struct {
	ID          int       `json:"id"`
	Title       string    `json:"title"`
	Year        int       `json:"year"`
	TVDBID      string    `json:"tvdb_id"`
	TMDBID      string    `json:"tmdb_id"`
	IMDBID      string    `json:"imdb_id"`
	Path        string    `json:"path"`
	Overview    string    `json:"overview"`
	PosterPath  string    `json:"poster_path"`
	Genres      string    `json:"genres"`
	Status      string    `json:"status"`
	RawMetadata []byte    `json:"raw_metadata"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Season struct {
	ID           int       `json:"id"`
	ShowID       int       `json:"show_id"`
	SeasonNumber int       `json:"season_number"`
	Overview     string    `json:"overview"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Episode struct {
	ID              int        `json:"id"`
	SeasonID        int        `json:"season_id"`
	EpisodeNumber   int        `json:"episode_number"`
	Title           string     `json:"title"`
	FilePath        string     `json:"file_path"`
	Quality         string     `json:"quality"`
	Size            int64      `json:"size"`
	TorrentHash     string     `json:"torrent_hash,omitempty"` // Torrent hash for seeding status
	ImportedAt      *time.Time `json:"imported_at,omitempty"`  // Timestamp when imported to library
	SubtitlesSynced bool       `json:"subtitles_synced"`       // Whether subtitles have been synced
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}
