package models

import "time"

type Show struct {
	ID          int       `json:"id"`
	Title       string    `json:"title"`
	Year        int       `json:"year"`
	TVDBID      string    `json:"tvdb_id"`
	Path        string    `json:"path"`
	Overview    string    `json:"overview"`
	PosterPath  string    `json:"poster_path"`
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
	ID            int       `json:"id"`
	SeasonID      int       `json:"season_id"`
	EpisodeNumber int       `json:"episode_number"`
	Title         string    `json:"title"`
	FilePath      string    `json:"file_path"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}
