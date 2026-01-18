package services

import (
	"Arrgo/config"
	"Arrgo/database"
	"Arrgo/models"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

type TMDBMovieSearchResponse struct {
	Results []struct {
		ID           int     `json:"id"`
		Title        string  `json:"title"`
		ReleaseDate  string  `json:"release_date"`
		Overview     string  `json:"overview"`
		PosterPath   string  `json:"poster_path"`
		VoteAverage  float64 `json:"vote_average"`
	} `json:"results"`
}

func MatchMovie(cfg *config.Config, movieID int) error {
	// 1. Fetch movie from DB
	var m models.Movie
	query := `SELECT id, title, year FROM movies WHERE id = $1`
	err := database.DB.QueryRow(query, movieID).Scan(&m.ID, &m.Title, &m.Year)
	if err != nil {
		return err
	}

	if cfg.TMDBAPIKey == "" {
		return fmt.Errorf("TMDB_API_KEY is not set")
	}

	// 2. Search TMDB
	searchURL := fmt.Sprintf("https://api.themoviedb.org/3/search/movie?api_key=%s&query=%s", 
		cfg.TMDBAPIKey, url.QueryEscape(m.Title))
	if m.Year > 0 {
		searchURL += fmt.Sprintf("&year=%d", m.Year)
	}

	resp, err := http.Get(searchURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var searchResults TMDBMovieSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResults); err != nil {
		return err
	}

	if len(searchResults.Results) == 0 {
		return fmt.Errorf("no matches found on TMDB for %s", m.Title)
	}

	// 3. Take the first result
	result := searchResults.Results[0]
	
	// Fetch full movie details or just use search result
	// Let's store the raw JSON from TMDB too
	rawMetadata, _ := json.Marshal(result)

	// 4. Update DB
	updateQuery := `
		UPDATE movies 
		SET tmdb_id = $1, overview = $2, poster_path = $3, status = 'matched', raw_metadata = $4, updated_at = CURRENT_TIMESTAMP
		WHERE id = $5
	`
	_, err = database.DB.Exec(updateQuery, fmt.Sprintf("%d", result.ID), result.Overview, result.PosterPath, rawMetadata, m.ID)
	if err == nil {
		// Trigger renaming in background or synchronously here
		RenameAndMoveMovie(cfg, m.ID)
	}
	return err
}

type TMDBShowSearchResponse struct {
	Results []struct {
		ID           int     `json:"id"`
		Name         string  `json:"name"`
		FirstAirDate string  `json:"first_air_date"`
		Overview     string  `json:"overview"`
		PosterPath   string  `json:"poster_path"`
		VoteAverage  float64 `json:"vote_average"`
	} `json:"results"`
}

func MatchShow(cfg *config.Config, showID int) error {
	var s models.Show
	query := `SELECT id, title, year FROM shows WHERE id = $1`
	err := database.DB.QueryRow(query, showID).Scan(&s.ID, &s.Title, &s.Year)
	if err != nil {
		return err
	}

	if cfg.TMDBAPIKey == "" {
		return fmt.Errorf("TMDB_API_KEY is not set")
	}

	searchURL := fmt.Sprintf("https://api.themoviedb.org/3/search/tv?api_key=%s&query=%s", 
		cfg.TMDBAPIKey, url.QueryEscape(s.Title))
	if s.Year > 0 {
		searchURL += fmt.Sprintf("&first_air_date_year=%d", s.Year)
	}

	resp, err := http.Get(searchURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var searchResults TMDBShowSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResults); err != nil {
		return err
	}

	if len(searchResults.Results) == 0 {
		return fmt.Errorf("no matches found on TMDB for %s", s.Title)
	}

	result := searchResults.Results[0]
	rawMetadata, _ := json.Marshal(result)

	updateQuery := `
		UPDATE shows 
		SET tvdb_id = $1, overview = $2, poster_path = $3, status = 'matched', raw_metadata = $4, updated_at = CURRENT_TIMESTAMP
		WHERE id = $5
	`
	_, err = database.DB.Exec(updateQuery, fmt.Sprintf("%d", result.ID), result.Overview, result.PosterPath, rawMetadata, s.ID)
	if err == nil {
		// Fetch all episodes for this show and rename them
		epQuery := `SELECT e.id FROM episodes e JOIN seasons s ON e.season_id = s.id WHERE s.show_id = $1`
		rows, err := database.DB.Query(epQuery, s.ID)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var epID int
				if err := rows.Scan(&epID); err == nil {
					RenameAndMoveEpisode(cfg, epID)
				}
			}
		}
	}
	return err
}

func FetchMetadataForAllDiscovered(cfg *config.Config) {
	// Movies
	movieQuery := `SELECT id FROM movies WHERE status = 'discovered'`
	movieRows, err := database.DB.Query(movieQuery)
	if err == nil {
		defer movieRows.Close()
		for movieRows.Next() {
			var id int
			if err := movieRows.Scan(&id); err == nil {
				MatchMovie(cfg, id)
			}
		}
	}

	// Shows
	showQuery := `SELECT id FROM shows WHERE status = 'discovered'`
	showRows, err := database.DB.Query(showQuery)
	if err == nil {
		defer showRows.Close()
		for showRows.Next() {
			var id int
			if err := showRows.Scan(&id); err == nil {
				MatchShow(cfg, id)
			}
		}
	}
}
