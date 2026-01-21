package services

import (
	"Arrgo/config"
	"Arrgo/database"
	"Arrgo/models"
	"context"
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	scanMoviesMutex sync.Mutex
)

func ScanMovies(ctx context.Context, cfg *config.Config, onlyIncoming bool) error {
	scanType := ScanMovieLibrary
	if onlyIncoming {
		scanType = ScanIncomingMovies
	}

	if !scanMoviesMutex.TryLock() {
		slog.Info("Movie scan already in progress, skipping")
		return nil
	}
	defer func() {
		scanMoviesMutex.Unlock()
		FinishScan(scanType)
	}()

	slog.Info("Starting movie scan", "scan_type", scanType, "workers", DefaultWorkerCount)

	// Clean up missing files first
	PurgeMissingMovies()

	taskChan := make(chan string, TaskChannelBufferSize)
	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < DefaultWorkerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case path, ok := <-taskChan:
					if !ok {
						return
					}
					processMovieFile(cfg, path)
				}
			}
		}()
	}

	// Scan paths based on preference
	var paths []string
	if onlyIncoming {
		paths = []string{cfg.IncomingMoviesPath}
	} else {
		paths = []string{cfg.MoviesPath}
	}

	for _, p := range paths {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); os.IsNotExist(err) {
			slog.Debug("Path does not exist, skipping", "path", p)
			continue
		}
		slog.Debug("Walking path", "path", p)
		filepath.WalkDir(p, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				slog.Error("Error walking path", "path", path, "error", err)
				return nil
			}

			select {
			case <-ctx.Done():
				return context.Canceled
			default:
			}

			if d.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			if MovieExtensions[ext] {
				taskChan <- path
			}
			return nil
		})
	}

	close(taskChan)
	wg.Wait()

	if ctx.Err() == context.Canceled {
		slog.Info("Movie scan cancelled", "scan_type", scanType)
	} else {
		slog.Info("Movie scan complete", "scan_type", scanType)
	}

	return nil
}

func processMovieFile(cfg *config.Config, path string) {
	filename := filepath.Base(path)
	nameOnly := strings.TrimSuffix(filename, filepath.Ext(filename))
	title, year, tmdbID, tvdbID, imdbID := ParseMediaName(nameOnly)

	// If filename doesn't have year, try parent directory
	if year == 0 && tmdbID == "" && tvdbID == "" && imdbID == "" {
		parentDir := filepath.Base(filepath.Dir(path))
		// Check if parentDir is not just one of the root scan paths
		if parentDir != "." && parentDir != filepath.Base(cfg.MoviesPath) &&
			parentDir != filepath.Base(cfg.IncomingMoviesPath) {
			title, year, tmdbID, tvdbID, imdbID = ParseMediaName(parentDir)
		}
	}

	info, err := os.Stat(path)
	if err != nil {
		return
	}
	size := info.Size()
	quality := DetectQuality(path)

	// Look for local poster
	posterPath := findLocalPoster(filepath.Dir(path))

	movie := models.Movie{
		Title:      title,
		Year:       year,
		TMDBID:     tmdbID,
		IMDBID:     imdbID,
		Path:       path,
		Quality:    quality,
		Size:       size,
		PosterPath: posterPath,
		Status:     "discovered",
	}

	if id, err := upsertMovie(movie); err != nil {
		slog.Error("Error upserting movie", "title", title, "error", err)
	} else {
		// Fetch metadata immediately
		MatchMovie(cfg, id)
	}
}

func upsertMovie(movie models.Movie) (int, error) {
	var id int
	query := `
		INSERT INTO movies (title, year, tmdb_id, imdb_id, path, quality, size, poster_path, status, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, CURRENT_TIMESTAMP)
		ON CONFLICT (path) DO UPDATE SET
			title = EXCLUDED.title,
			year = EXCLUDED.year,
			tmdb_id = COALESCE(NULLIF(EXCLUDED.tmdb_id, ''), movies.tmdb_id),
			imdb_id = COALESCE(NULLIF(EXCLUDED.imdb_id, ''), movies.imdb_id),
			quality = EXCLUDED.quality,
			size = EXCLUDED.size,
			poster_path = COALESCE(NULLIF(EXCLUDED.poster_path, ''), movies.poster_path),
			status = movies.status, -- Keep existing status if it was already matched
			updated_at = CURRENT_TIMESTAMP
		RETURNING id
	`
	err := database.DB.QueryRow(query, movie.Title, movie.Year, movie.TMDBID, movie.IMDBID, movie.Path, movie.Quality, movie.Size, movie.PosterPath, movie.Status).Scan(&id)
	return id, err
}

func GetMovies() ([]models.Movie, error) {
	query := `SELECT id, title, year, tmdb_id, imdb_id, path, quality, size, overview, poster_path, genres, status, created_at, updated_at FROM movies ORDER BY title ASC`
	rows, err := database.DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	movies := []models.Movie{}
	for rows.Next() {
		var m models.Movie
		var tmdbID, imdbID, overview, posterPath, quality, genres sql.NullString
		err := rows.Scan(&m.ID, &m.Title, &m.Year, &tmdbID, &imdbID, &m.Path, &quality, &m.Size, &overview, &posterPath, &genres, &m.Status, &m.CreatedAt, &m.UpdatedAt)
		if err != nil {
			return nil, err
		}
		m.TMDBID = tmdbID.String
		m.IMDBID = imdbID.String
		m.Overview = overview.String
		m.PosterPath = posterPath.String
		m.Quality = quality.String
		m.Genres = genres.String
		movies = append(movies, m)
	}
	return movies, nil
}

func GetMovieByID(id int) (*models.Movie, error) {
	query := `SELECT id, title, year, tmdb_id, imdb_id, path, quality, size, overview, poster_path, genres, status, created_at, updated_at FROM movies WHERE id = $1`
	var m models.Movie
	var tmdbID, imdbID, overview, posterPath, quality, genres sql.NullString
	err := database.DB.QueryRow(query, id).Scan(&m.ID, &m.Title, &m.Year, &tmdbID, &imdbID, &m.Path, &quality, &m.Size, &overview, &posterPath, &genres, &m.Status, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return nil, err
	}
	m.TMDBID = tmdbID.String
	m.IMDBID = imdbID.String
	m.Overview = overview.String
	m.PosterPath = posterPath.String
	m.Quality = quality.String
	m.Genres = genres.String
	return &m, nil
}

func GetMovieCount(excludeIncomingPath string) (int, error) {
	var count int
	var err error
	if excludeIncomingPath != "" {
		err = database.DB.QueryRow("SELECT COUNT(*) FROM movies WHERE path NOT LIKE $1 || '%'", excludeIncomingPath).Scan(&count)
	} else {
		err = database.DB.QueryRow("SELECT COUNT(*) FROM movies").Scan(&count)
	}
	return count, err
}

func PurgeMissingMovies() {
	slog.Debug("Checking for missing movies")
	rows, err := database.DB.Query("SELECT id, path FROM movies")
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id int
		var path string
		if err := rows.Scan(&id, &path); err != nil {
			continue
		}

		if _, err := os.Stat(path); os.IsNotExist(err) {
			slog.Info("Removing missing movie from DB", "movie_id", id, "path", path)
			database.DB.Exec("DELETE FROM movies WHERE id = $1", id)
		}
	}
}

func SearchMoviesLocal(query string) ([]models.Movie, error) {
	dbQuery := `
		SELECT id, title, year, tmdb_id, imdb_id, path, quality, size, overview, poster_path, genres, status, created_at, updated_at
		FROM movies
		WHERE title ILIKE $1 OR overview ILIKE $1 OR genres ILIKE $1
		ORDER BY title ASC
	`
	rows, err := database.DB.Query(dbQuery, "%"+query+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var movies []models.Movie
	for rows.Next() {
		var m models.Movie
		var tmdbID, imdbID, overview, posterPath, quality, genres sql.NullString
		err := rows.Scan(&m.ID, &m.Title, &m.Year, &tmdbID, &imdbID, &m.Path, &quality, &m.Size, &overview, &posterPath, &genres, &m.Status, &m.CreatedAt, &m.UpdatedAt)
		if err != nil {
			return nil, err
		}
		m.TMDBID = tmdbID.String
		m.IMDBID = imdbID.String
		m.Overview = overview.String
		m.PosterPath = posterPath.String
		m.Quality = quality.String
		m.Genres = genres.String
		movies = append(movies, m)
	}
	return movies, nil
}
