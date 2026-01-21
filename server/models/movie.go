package models

import "time"

type Movie struct {
	ID          int       `json:"id"`
	Title       string    `json:"title"`
	Year        int       `json:"year"`
	TMDBID      string    `json:"tmdb_id"`
	IMDBID      string    `json:"imdb_id"`
	Path        string    `json:"path"`
	Quality     string    `json:"quality"`
	Size        int64     `json:"size"`
	Overview    string    `json:"overview"`
	PosterPath  string    `json:"poster_path"`
	Genres      string    `json:"genres"`
	Status      string    `json:"status"` // e.g., "discovered", "matching", "ready"
	RawMetadata []byte    `json:"raw_metadata"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
