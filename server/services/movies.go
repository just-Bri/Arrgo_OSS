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
	"regexp"
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

		slog.Debug("Found entries in directory", "path", p, "count", len(entries))
		dirCount := 0
		stopped := false
		for _, entry := range entries {
			select {
			case <-ctx.Done():
				stopped = true
			default:
				if entry.IsDir() {
					dirCount++
					slog.Debug("Found directory to process", "path", p, "folder", entry.Name())
					taskChan <- movieTask{root: p, name: entry.Name()}
				}
			}
			if stopped {
				break
			}
		}
		slog.Debug("Queued directories for processing", "path", p, "directory_count", dirCount)
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
		"plex versions": true, "plex optimized": true,
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

	slog.Debug("Processing movie folder", "folder", folderName, "folder_path", folderPath, "parsed_title", title, "year", year)

	// Check if a movie with this title/year already exists in the library (not in incoming)
	// This prevents re-scanning folders for movies that were already imported
	var existingMovieID int
	var existingMoviePath string
	checkQuery := `
		SELECT id, path FROM movies 
		WHERE title = $1 AND year = $2 
		AND imported_at IS NOT NULL
		AND path NOT LIKE $3 || '%'
		LIMIT 1
	`
	err := database.DB.QueryRow(checkQuery, title, year, cfg.IncomingMoviesPath).Scan(&existingMovieID, &existingMoviePath)
	if err == nil {
		slog.Debug("Movie already exists in library, skipping incoming folder",
			"folder", folderName,
			"title", title,
			"year", year,
			"existing_movie_id", existingMovieID,
			"existing_path", existingMoviePath)
		return
	}

	// Find the main movie file in this folder
	mainMovieFile := findMainMovieFile(folderPath, folderName)
	if mainMovieFile == "" {
		slog.Debug("No movie file found in folder", "folder", folderName, "folder_path", folderPath)
		return
	}

	slog.Debug("Found main movie file", "folder", folderName, "file", mainMovieFile)

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

	id, err := upsertMovie(movie)
	if err != nil {
		slog.Error("Error upserting movie", "title", title, "folder", folderName, "error", err)
		return
	}

	slog.Info("Upserted movie", "movie_id", id, "title", title, "year", year, "folder", folderName, "path", mainMovieFile)

	// Try to link torrent hash if file is in incoming folder
	if strings.HasPrefix(mainMovieFile, cfg.IncomingMoviesPath) {
		if qb, err := NewQBittorrentClient(cfg); err == nil {
			LinkTorrentHashToFile(cfg, qb, mainMovieFile, "movie")
		} else {
			slog.Debug("Could not create qBittorrent client for hash linking", "error", err)
		}
	}
	// Fetch metadata immediately
	if err := MatchMovie(cfg, id); err != nil {
		slog.Debug("Error matching movie metadata", "movie_id", id, "title", title, "error", err)
	}
}

// findMainMovieFile finds the main movie file in a folder, skipping extras
func findMainMovieFile(folderPath string, folderName string) string {
	var candidates []struct {
		path string
		size int64
	}

	// Common patterns for extra files to skip
	// Use word boundaries to avoid false matches (e.g., "ending" shouldn't match "Bending")
	skipPatterns := []string{
		"cm", "pv", "iv", "tvsp", "menu", "trailer", "sample",
		"deleted", "behind", "featurette", "interview", "promo",
		"promotional", "teaser", "preview", "intro", "outro",
		"credit", "credits", "opening", "plex versions", "plex optimized",
	}

	slog.Debug("Searching for movie files", "folder_path", folderPath, "folder_name", folderName)

	// Walk the folder (but skip subdirectories that are clearly extras)
	err := filepath.WalkDir(folderPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			slog.Debug("Error walking directory", "path", path, "error", err)
			return nil
		}

		// Skip subdirectories (like SPs, Extras, etc.)
		if d.IsDir() && path != folderPath {
			subDirName := strings.ToLower(d.Name())

			// Detect show-like subdirectories
			seasonRegex := regexp.MustCompile(`(?i)^(Season\s+\d+|S\d+)$`)
			if seasonRegex.MatchString(subDirName) {
				slog.Debug("Skipping show folder during movie scan", "subdir", subDirName, "path", path)
				return filepath.SkipDir
			}

			skipDirs := map[string]bool{
				"sps": true, "sp": true, "extras": true, "extra": true,
				"bonus": true, "bonuses": true, "menus": true, "menu": true,
				"trailers": true, "trailer": true, "samples": true, "sample": true,
				"plex versions": true, "plex optimized": true,
			}
			if skipDirs[subDirName] {
				slog.Debug("Skipping subdirectory", "subdir", subDirName, "path", path)
				return filepath.SkipDir
			}
			slog.Debug("Found subdirectory (not skipped)", "subdir", subDirName, "path", path)
			return nil
		}

		if d.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		filename := filepath.Base(path)
		if !MovieExtensions[ext] {
			slog.Debug("Skipping file - extension not recognized", "file", filename, "ext", ext)
			return nil
		}

		slog.Debug("Found potential movie file", "file", filename, "ext", ext, "path", path)

		// Check if this looks like an extra file
		// Use word boundaries to avoid false matches (e.g., "ending" shouldn't match "Bending")
		filenameLower := strings.ToLower(filename)
		isExtra := false
		for _, pattern := range skipPatterns {
			// Use regexp for word boundary matching - pattern must be a whole word
			// Word boundary: start of string, end of string, or non-word character
			patternLower := strings.ToLower(pattern)
			// Escape special regex characters in pattern
			escapedPattern := regexp.QuoteMeta(patternLower)
			// Match whole word: word boundary before and after
			wordBoundaryRegex := regexp.MustCompile(`(^|[^a-z0-9])` + escapedPattern + `([^a-z0-9]|$)`)
			if wordBoundaryRegex.MatchString(filenameLower) {
				isExtra = true
				slog.Debug("File marked as extra", "file", filename, "pattern", pattern)
				break
			}
		}

		// Also check if filename contains folder name (more likely to be main movie)
		containsFolderName := strings.Contains(filenameLower, strings.ToLower(folderName))

		info, err := os.Stat(path)
		if err != nil {
			slog.Debug("Error getting file info", "file", filename, "error", err)
			return nil
		}

		slog.Debug("Adding file as candidate", "file", filename, "size", info.Size(), "is_extra", isExtra, "contains_folder_name", containsFolderName)

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
		slog.Debug("Error walking directory", "folder_path", folderPath, "error", err)
		return ""
	}

	if len(candidates) == 0 {
		slog.Debug("No movie file candidates found", "folder_path", folderPath, "folder_name", folderName)
		return ""
	}

	slog.Debug("Found movie file candidates", "folder_path", folderPath, "count", len(candidates))

	// Find the best candidate:
	// 1. Files that contain folder name and aren't extras
	// 2. Largest file if no folder name match
	var bestCandidate string
	var bestSize int64
	var foundFolderMatch bool

	var foundEpisode bool
	episodeRegex := regexp.MustCompile(`(?i)S\d+E\d+`)

	for _, cand := range candidates {
		filename := strings.ToLower(filepath.Base(cand.path))

		// If ANY file in this folder looks like an episode, skip the folder for movie scan
		if episodeRegex.MatchString(filename) {
			foundEpisode = true
			break
		}

		containsFolderName := strings.Contains(filename, strings.ToLower(folderName))

		// Check if it's an extra (using word boundaries)
		isExtra := false
		filenameLower := strings.ToLower(filename)
		for _, pattern := range skipPatterns {
			patternLower := strings.ToLower(pattern)
			escapedPattern := regexp.QuoteMeta(patternLower)
			wordBoundaryRegex := regexp.MustCompile(`(^|[^a-z0-9])` + escapedPattern + `([^a-z0-9]|$)`)
			if wordBoundaryRegex.MatchString(filenameLower) {
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

	if foundEpisode {
		slog.Debug("Skipping show folder (has episodes) during movie scan", "folder", folderName)
		return ""
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
	query := `SELECT id, title, year, tmdb_id, imdb_id, path, quality, size, overview, poster_path, genres, status, imported_at, torrent_hash, subtitles_synced, created_at, updated_at FROM movies ORDER BY title ASC`
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
		err := rows.Scan(&m.ID, &m.Title, &m.Year, &tmdbID, &imdbID, &m.Path, &quality, &m.Size, &overview, &posterPath, &genres, &m.Status, &importedAt, &torrentHash, &m.SubtitlesSynced, &m.CreatedAt, &m.UpdatedAt)
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

func GetIncomingMovies(incomingPath string) ([]models.Movie, error) {
	query := `
		SELECT id, title, year, tmdb_id, imdb_id, path, quality, size, overview, poster_path, genres, status, imported_at, torrent_hash, subtitles_synced, created_at, updated_at 
		FROM movies 
		WHERE path LIKE $1 || '%' AND imported_at IS NULL
		ORDER BY created_at DESC`
	rows, err := database.DB.Query(query, incomingPath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	movies := []models.Movie{}
	for rows.Next() {
		var m models.Movie
		var tmdbID, imdbID, overview, posterPath, quality, genres, torrentHash sql.NullString
		var importedAt sql.NullTime
		err := rows.Scan(&m.ID, &m.Title, &m.Year, &tmdbID, &imdbID, &m.Path, &quality, &m.Size, &overview, &posterPath, &genres, &m.Status, &importedAt, &torrentHash, &m.SubtitlesSynced, &m.CreatedAt, &m.UpdatedAt)
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
	query := `SELECT id, title, year, tmdb_id, imdb_id, path, quality, size, overview, poster_path, genres, status, imported_at, subtitles_synced, created_at, updated_at FROM movies WHERE id = $1`
	var m models.Movie
	var tmdbID, imdbID, overview, posterPath, quality, genres sql.NullString
	var importedAt sql.NullTime
	err := database.DB.QueryRow(query, id).Scan(&m.ID, &m.Title, &m.Year, &tmdbID, &imdbID, &m.Path, &quality, &m.Size, &overview, &posterPath, &genres, &m.Status, &importedAt, &m.SubtitlesSynced, &m.CreatedAt, &m.UpdatedAt)
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
		SELECT id, title, year, tmdb_id, imdb_id, path, quality, size, overview, poster_path, genres, status, subtitles_synced, created_at, updated_at
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
		err := rows.Scan(&m.ID, &m.Title, &m.Year, &tmdbID, &imdbID, &m.Path, &quality, &m.Size, &overview, &posterPath, &genres, &m.Status, &m.SubtitlesSynced, &m.CreatedAt, &m.UpdatedAt)
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
