package services

import (
	"Arrgo/config"
	"Arrgo/models"
	"fmt"
	"os"
	"path/filepath"
)

func ScanMovies(cfg *config.Config) ([]models.Movie, error) {
	movies := []models.Movie{}

	// Check if movies path exists
	if _, err := os.Stat(cfg.MoviesPath); os.IsNotExist(err) {
		return movies, fmt.Errorf("movies path does not exist: %s", cfg.MoviesPath)
	}

	// Read directory entries
	entries, err := os.ReadDir(cfg.MoviesPath)
	if err != nil {
		return movies, fmt.Errorf("failed to read movies directory: %w", err)
	}

	// Iterate through entries and find directories (each movie is in its own folder)
	for _, entry := range entries {
		if entry.IsDir() {
			moviePath := filepath.Join(cfg.MoviesPath, entry.Name())
			movies = append(movies, models.Movie{
				Title: entry.Name(),
				Path:  moviePath,
			})
		}
	}

	return movies, nil
}

