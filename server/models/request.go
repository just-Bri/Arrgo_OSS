package models

import "time"

type Request struct {
	ID          int       `json:"id"`
	UserID      int       `json:"user_id"`
	Username    string    `json:"username,omitempty"` // For display
	Title       string    `json:"title"`
	MediaType   string    `json:"media_type"` // "movie" or "show"
	TMDBID      string    `json:"tmdb_id,omitempty"`
	TVDBID      string    `json:"tvdb_id,omitempty"`
	Year        int       `json:"year"`
	PosterPath  string    `json:"poster_path"`
	Overview    string    `json:"overview"`
	Seasons     string    `json:"seasons,omitempty"` // Comma-separated list of season numbers
	Status      string    `json:"status"` // "pending", "approved", "downloading", "completed", "cancelled"
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
