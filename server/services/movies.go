package services

import (
	"Arrgo/config"
	"Arrgo/database"
	"Arrgo/models"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var movieExtensions = map[string]bool{
	".mp4":  true,
	".mkv":  true,
	".avi":  true,
	".mov":  true,
	".wmv":  true,
	".m4v":  true,
	".flv":  true,
	".webm": true,
}

func ScanMovies(cfg *config.Config) error {
	// Scan both media path and incoming path
	paths := []string{cfg.MoviesPath, cfg.IncomingPath}
	for _, p := range paths {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); os.IsNotExist(err) {
			continue
		}
		if err := scanPath(cfg, p); err != nil {
			return err
		}
	}

	// Trigger metadata fetching in background
	go FetchMetadataForAllDiscovered(cfg)

	return nil
}

func scanPath(cfg *config.Config, root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if !movieExtensions[ext] {
			return nil
		}

		// Parse title and year from filename or parent directory name
		filename := strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))
		title, year := parseMovieName(filename)

		// If filename doesn't have year, try parent directory
		if year == 0 {
			parentDir := filepath.Base(filepath.Dir(path))
			if parentDir != "." && parentDir != filepath.Base(root) {
				title, year = parseMovieName(parentDir)
			}
		}

		movie := models.Movie{
			Title:  title,
			Year:   year,
			Path:   path,
			Status: "discovered",
		}

		return upsertMovie(movie)
	})
}

func parseMovieName(name string) (string, int) {
	// Match "Title (Year)"
	re := regexp.MustCompile(`^(.*?)\s*\((\d{4})\)$`)
	matches := re.FindStringSubmatch(name)
	if len(matches) == 3 {
		title := strings.TrimSpace(matches[1])
		year, _ := strconv.Atoi(matches[2])
		return title, year
	}
	return name, 0
}

func upsertMovie(movie models.Movie) error {
	query := `
		INSERT INTO movies (title, year, path, status, updated_at)
		VALUES ($1, $2, $3, $4, CURRENT_TIMESTAMP)
		ON CONFLICT (path) DO UPDATE SET
			title = EXCLUDED.title,
			year = EXCLUDED.year,
			status = movies.status, -- Keep existing status if it was already matched
			updated_at = CURRENT_TIMESTAMP
	`
	_, err := database.DB.Exec(query, movie.Title, movie.Year, movie.Path, movie.Status)
	return err
}

func GetMovies() ([]models.Movie, error) {
	query := `SELECT id, title, year, tmdb_id, path, overview, poster_path, status, created_at, updated_at FROM movies ORDER BY title ASC`
	rows, err := database.DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	movies := []models.Movie{}
	for rows.Next() {
		var m models.Movie
		err := rows.Scan(&m.ID, &m.Title, &m.Year, &m.TMDBID, &m.Path, &m.Overview, &m.PosterPath, &m.Status, &m.CreatedAt, &m.UpdatedAt)
		if err != nil {
			return nil, err
		}
		movies = append(movies, m)
	}
	return movies, nil
}
