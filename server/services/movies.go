package services

import (
	"Arrgo/config"
	"Arrgo/database"
	"Arrgo/models"
	"database/sql"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
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

func ScanMovies(cfg *config.Config, onlyIncoming bool) error {
	log.Printf("[SCANNER] Starting movie scan with 4 workers...")

	// Clean up missing files first
	PurgeMissingMovies()

	taskChan := make(chan string, 100)
	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range taskChan {
				processMovieFile(cfg, path)
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
			log.Printf("[SCANNER] Path does not exist, skipping: %s", p)
			continue
		}
		log.Printf("[SCANNER] Walking path: %s", p)
		filepath.WalkDir(p, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				log.Printf("[SCANNER] Error walking path %s: %v", path, err)
				return nil
			}
			if d.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			if movieExtensions[ext] {
				taskChan <- path
			}
			return nil
		})
	}

	close(taskChan)
	wg.Wait()

	log.Printf("[SCANNER] Movie scan complete.")

	return nil
}

func processMovieFile(cfg *config.Config, path string) {
	filename := filepath.Base(path)
	nameOnly := strings.TrimSuffix(filename, filepath.Ext(filename))
	title, year := parseMovieName(nameOnly)

	// If filename doesn't have year, try parent directory
	if year == 0 {
		parentDir := filepath.Base(filepath.Dir(path))
		// Check if parentDir is not just one of the root scan paths
		if parentDir != "." && parentDir != filepath.Base(cfg.MoviesPath) &&
			parentDir != filepath.Base(cfg.IncomingMoviesPath) {
			title, year = parseMovieName(parentDir)
		}
	}

	info, err := os.Stat(path)
	if err != nil {
		return
	}
	size := info.Size()
	quality := DetectQuality(path)

	// Look for local poster
	posterPath := ""
	parentDir := filepath.Dir(path)
	posterExtensions := []string{".jpg", ".jpeg", ".png", ".webp"}
	posterNames := []string{"poster", "folder", "cover", "movie"}

	for _, name := range posterNames {
		for _, ext := range posterExtensions {
			p := filepath.Join(parentDir, name+ext)
			if _, err := os.Stat(p); err == nil {
				posterPath = p
				break
			}
		}
		if posterPath != "" {
			break
		}
	}

	movie := models.Movie{
		Title:      title,
		Year:       year,
		Path:       path,
		Quality:    quality,
		Size:       size,
		PosterPath: posterPath,
		Status:     "discovered",
	}

	if id, err := upsertMovie(movie); err != nil {
		log.Printf("[SCANNER] Error upserting %s: %v", title, err)
	} else {
		// Fetch metadata immediately
		MatchMovie(cfg, id)
	}
}

func parseMovieName(name string) (string, int) {
	// 1. Clean up ID tags like [tmdbid-343423], {tmdb-343423}, [tvdb-12345], etc.
	// Make it case-insensitive and handle different variations
	idRegex := regexp.MustCompile(`(?i)[\[\{](tmdb|tvdb|tmdbid|imdb)[- ]?([a-z0-9]+)[\]\}]`)
	name = idRegex.ReplaceAllString(name, "")
	name = strings.TrimSpace(name)

	// 2. Match "Title (Year)"
	re := regexp.MustCompile(`^(.*?)\s*\((\d{4})\)$`)
	matches := re.FindStringSubmatch(name)
	if len(matches) == 3 {
		title := strings.TrimSpace(matches[1])
		year, _ := strconv.Atoi(matches[2])
		return title, year
	}
	return name, 0
}

func upsertMovie(movie models.Movie) (int, error) {
	var id int
	query := `
		INSERT INTO movies (title, year, path, quality, size, poster_path, status, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, CURRENT_TIMESTAMP)
		ON CONFLICT (path) DO UPDATE SET
			title = EXCLUDED.title,
			year = EXCLUDED.year,
			quality = EXCLUDED.quality,
			size = EXCLUDED.size,
			poster_path = COALESCE(NULLIF(EXCLUDED.poster_path, ''), movies.poster_path),
			status = movies.status, -- Keep existing status if it was already matched
			updated_at = CURRENT_TIMESTAMP
		RETURNING id
	`
	err := database.DB.QueryRow(query, movie.Title, movie.Year, movie.Path, movie.Quality, movie.Size, movie.PosterPath, movie.Status).Scan(&id)
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
	log.Printf("[SCANNER] Checking for missing movies...")
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
			log.Printf("[SCANNER] Removing missing movie from DB: %s", path)
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
