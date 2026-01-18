package models

import "time"

type Movie struct {
	ID         int       `json:"id"`
	Title      string    `json:"title"`
	Year       int       `json:"year"`
	TMDBID     string    `json:"tmdb_id"`
	Path       string    `json:"path"`
	Overview    string    `json:"overview"`
	PosterPath  string    `json:"poster_path"`
	Status      string    `json:"status"` // e.g., "discovered", "matching", "ready"
	RawMetadata []byte    `json:"raw_metadata"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

