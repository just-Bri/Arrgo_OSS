package services

import (
	"Arrgo/config"
	"Arrgo/database"
	"Arrgo/models"
	"context"
	"database/sql"
	"fmt"
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

	type movieTask struct {
		root string
		name string
	}

	taskChan := make(chan movieTask, TaskChannelBufferSize)
	var wg sync.WaitGroup

	// Start workers
	for range DefaultWorkerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case task, ok := <-taskChan:
					if !ok {
						return
					}
					processMovieDir(cfg, task.root, task.name)
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

		entries, err := os.ReadDir(p)
		if err != nil {
			slog.Error("Error reading directory", "path", p, "error", err)
			continue
		}

		stopped := false
		for _, entry := range entries {
			select {
			case <-ctx.Done():
				stopped = true
			default:
				if entry.IsDir() {
					taskChan <- movieTask{root: p, name: entry.Name()}
				}
			}
			if stopped {
				break
			}
		}
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

// processMovieDir processes a movie folder and finds the main movie file
func processMovieDir(cfg *config.Config, root string, folderName string) {
	folderPath := filepath.Join(root, folderName)

	// Lock this folder to prevent concurrent processing by multiple workers
	unlock := lockPath(folderPath)
	defer unlock()

	// Skip common extra/subdirectory names (case-insensitive, with common variations)
	folderNameLower := strings.ToLower(strings.TrimSpace(folderName))
	skipDirs := map[string]bool{
		"sps": true, "sp": true, "extras": true, "extra": true,
		"bonus": true, "bonuses": true, "menus": true, "menu": true,
		"trailers": true, "trailer": true, "samples": true, "sample": true,
		"scenes": true, "scene": true, "deleted": true, "deleted scenes": true,
		"featurettes": true, "featurette": true, "behind": true, "behind the scenes": true,
		"specials": true, "special": true, "bonus content": true,
	}

	// Check if folder name matches skip patterns (exact match or contains pattern)
	if skipDirs[folderNameLower] {
		slog.Debug("Skipping extra folder", "folder", folderName)
		return
	}

	// Also check if folder name is very short (likely not a movie title)
	// "SPs" is 3 chars, so we need to be careful - check if it's in skip list or very short
	if len(folderNameLower) <= 3 {
		// Double-check it's not a known skip pattern
		if skipDirs[folderNameLower] || folderNameLower == "sps" || folderNameLower == "sp" {
			slog.Debug("Skipping short folder name (likely extra)", "folder", folderName)
			return
		}
	}

	// Parse folder name for movie info
	title, year, tmdbID, _, imdbID := ParseMediaName(folderName)

	// If parsed title is empty or very short after cleaning, skip it
	// Also check if parsed title matches skip patterns (in case ParseMediaName didn't clean it properly)
	parsedTitleLower := strings.ToLower(strings.TrimSpace(title))
	if title == "" || len(parsedTitleLower) <= 2 || skipDirs[parsedTitleLower] {
		slog.Debug("Skipping folder with empty/short/skip title after parsing", "folder", folderName, "parsed_title", title)
		return
	}

	// Find the main movie file in this folder
	mainMovieFile := findMainMovieFile(folderPath, folderName)
	if mainMovieFile == "" {
		// No movie file found, skip this folder
		return
	}

	info, err := os.Stat(mainMovieFile)
	if err != nil {
		return
	}
	size := info.Size()
	quality := DetectQuality(mainMovieFile)

	// Look for local poster
	posterPath := findLocalPoster(folderPath)

	movie := models.Movie{
		Title:      title,
		Year:       year,
		TMDBID:     tmdbID,
		IMDBID:     imdbID,
		Path:       mainMovieFile,
		Quality:    quality,
		Size:       size,
		PosterPath: posterPath,
		Status:     "discovered",
	}

	if id, err := upsertMovie(movie); err != nil {
		slog.Error("Error upserting movie", "title", title, "error", err)
	} else {
		// Try to link torrent hash if file is in incoming folder
		if strings.HasPrefix(mainMovieFile, cfg.IncomingMoviesPath) {
			if qb, err := NewQBittorrentClient(cfg); err == nil {
				LinkTorrentHashToFile(cfg, qb, mainMovieFile, "movie")
			}
		}
		// Fetch metadata immediately
		MatchMovie(cfg, id)
	}
}

// findMainMovieFile finds the main movie file in a folder, skipping extras
func findMainMovieFile(folderPath string, folderName string) string {
	var candidates []struct {
		path string
		size int64
	}

	// Common patterns for extra files to skip
	skipPatterns := []string{
		"cm", "pv", "iv", "tvsp", "menu", "trailer", "sample",
		"deleted", "behind", "featurette", "interview", "promo",
		"promotional", "teaser", "preview", "intro", "outro",
		"credit", "credits", "opening", "ending",
	}

	// Walk the folder (but skip subdirectories that are clearly extras)
	err := filepath.WalkDir(folderPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		// Skip subdirectories (like SPs, Extras, etc.)
		if d.IsDir() && path != folderPath {
			subDirName := strings.ToLower(filepath.Base(path))
			skipDirs := map[string]bool{
				"sps": true, "sp": true, "extras": true, "extra": true,
				"bonus": true, "bonuses": true, "menus": true, "menu": true,
				"trailers": true, "trailer": true, "samples": true, "sample": true,
			}
			if skipDirs[subDirName] {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if !MovieExtensions[ext] {
			return nil
		}

		// Check if this looks like an extra file
		filename := strings.ToLower(filepath.Base(path))
		isExtra := false
		for _, pattern := range skipPatterns {
			if strings.Contains(filename, pattern) {
				isExtra = true
				break
			}
		}

		// Also check if filename contains folder name (more likely to be main movie)
		containsFolderName := strings.Contains(strings.ToLower(filename), strings.ToLower(folderName))

		info, err := os.Stat(path)
		if err != nil {
			return nil
		}

		// Prefer files that contain the folder name, or if none found, use largest
		candidates = append(candidates, struct {
			path string
			size int64
		}{
			path: path,
			size: info.Size(),
		})

		// If we found a file that contains the folder name and isn't an extra, prefer it
		if containsFolderName && !isExtra {
			// This is likely the main movie, but continue to check for better matches
			return nil
		}

		return nil
	})

	if err != nil {
		return ""
	}

	if len(candidates) == 0 {
		return ""
	}

	// Find the best candidate:
	// 1. Files that contain folder name and aren't extras
	// 2. Largest file if no folder name match
	var bestCandidate string
	var bestSize int64
	var foundFolderMatch bool

	for _, cand := range candidates {
		filename := strings.ToLower(filepath.Base(cand.path))
		containsFolderName := strings.Contains(filename, strings.ToLower(folderName))

		// Check if it's an extra
		isExtra := false
		for _, pattern := range skipPatterns {
			if strings.Contains(filename, pattern) {
				isExtra = true
				break
			}
		}

		// Prefer files that match folder name and aren't extras
		if containsFolderName && !isExtra {
			if !foundFolderMatch || cand.size > bestSize {
				bestCandidate = cand.path
				bestSize = cand.size
				foundFolderMatch = true
			}
		} else if !foundFolderMatch && !isExtra {
			// If no folder match yet, prefer largest non-extra file
			if bestCandidate == "" || cand.size > bestSize {
				bestCandidate = cand.path
				bestSize = cand.size
			}
		}
	}

	return bestCandidate
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
	query := `SELECT id, title, year, tmdb_id, imdb_id, path, quality, size, overview, poster_path, genres, status, imported_at, torrent_hash, created_at, updated_at FROM movies ORDER BY title ASC`
	rows, err := database.DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

		movies := []models.Movie{}
	for rows.Next() {
		var m models.Movie
		var tmdbID, imdbID, overview, posterPath, quality, genres, torrentHash sql.NullString
		var importedAt sql.NullTime
		err := rows.Scan(&m.ID, &m.Title, &m.Year, &tmdbID, &imdbID, &m.Path, &quality, &m.Size, &overview, &posterPath, &genres, &m.Status, &importedAt, &torrentHash, &m.CreatedAt, &m.UpdatedAt)
		if err != nil {
			return nil, err
		}
		m.TMDBID = tmdbID.String
		m.IMDBID = imdbID.String
		m.Overview = overview.String
		m.PosterPath = posterPath.String
		if importedAt.Valid {
			m.ImportedAt = &importedAt.Time
		}
		m.Quality = quality.String
		m.Genres = genres.String
		m.TorrentHash = torrentHash.String
		movies = append(movies, m)
	}
	return movies, nil
}

func GetMovieByID(id int) (*models.Movie, error) {
	query := `SELECT id, title, year, tmdb_id, imdb_id, path, quality, size, overview, poster_path, genres, status, imported_at, created_at, updated_at FROM movies WHERE id = $1`
	var m models.Movie
	var tmdbID, imdbID, overview, posterPath, quality, genres sql.NullString
	var importedAt sql.NullTime
	err := database.DB.QueryRow(query, id).Scan(&m.ID, &m.Title, &m.Year, &tmdbID, &imdbID, &m.Path, &quality, &m.Size, &overview, &posterPath, &genres, &m.Status, &importedAt, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return nil, err
	}
	m.TMDBID = tmdbID.String
	m.IMDBID = imdbID.String
	m.Overview = overview.String
	m.PosterPath = posterPath.String
	m.Quality = quality.String
	m.Genres = genres.String
	if importedAt.Valid {
		m.ImportedAt = &importedAt.Time
	}
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
	// Get search variants (e.g., "In & Out" -> ["In & Out", "In and Out"])
	variants := GetSearchVariantsForDB(query)
	
	// Build SQL query with OR conditions for each variant
	var conditions []string
	args := make([]interface{}, len(variants))
	for i, variant := range variants {
		conditions = append(conditions, fmt.Sprintf("(title ILIKE $%d OR overview ILIKE $%d OR genres ILIKE $%d)", i+1, i+1, i+1))
		args[i] = variant
	}
	
	dbQuery := fmt.Sprintf(`
		SELECT id, title, year, tmdb_id, imdb_id, path, quality, size, overview, poster_path, genres, status, created_at, updated_at
		FROM movies
		WHERE %s
		ORDER BY title ASC
	`, strings.Join(conditions, " OR "))
	
	rows, err := database.DB.Query(dbQuery, args...)
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
