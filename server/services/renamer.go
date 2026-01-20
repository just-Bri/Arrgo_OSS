package services

import (
	"Arrgo/config"
	"Arrgo/database"
	"Arrgo/models"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// pathMutex provides per-path locking to prevent concurrent operations on the same file/directory
var (
	pathMutexes = make(map[string]*sync.Mutex)
	pathMu      sync.Mutex
)

// getPathMutex returns a mutex for the given path, creating one if it doesn't exist
func getPathMutex(path string) *sync.Mutex {
	pathMu.Lock()
	defer pathMu.Unlock()
	
	if mu, exists := pathMutexes[path]; exists {
		return mu
	}
	
	mu := &sync.Mutex{}
	pathMutexes[path] = mu
	return mu
}

// lockPath locks the mutex for the given path and returns an unlock function
func lockPath(path string) func() {
	mu := getPathMutex(path)
	mu.Lock()
	return mu.Unlock
}

func safeRename(src, dst string) error {
	// Try renaming first (efficient if on same device)
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}

	// Fallback for "invalid cross-device link" (EXDEV)
	slog.Info("Cross-device move detected, falling back to copy+delete", "src", src, "dst", dst)

	// Create destination directory if it doesn't exist (should already be created by caller, but safe check)
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return err
	}

	// Ensure everything is written to disk
	if err := destFile.Sync(); err != nil {
		return err
	}

	// Important to close before removing
	sourceFile.Close()
	destFile.Close()

	return os.Remove(src)
}

func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return err
	}

	return destFile.Sync()
}

// handleExistingFile checks if a file exists at the destination and handles quality comparison.
// Returns true if the candidate should be deleted (lower quality/size), false if it should proceed.
// If the existing file should be replaced, it will be deleted by deleteExisting callback.
func handleExistingFile(destPath, candidatePath, candidateQuality string, candidateSize int64, query string, deleteCandidate func() error, deleteExisting func() error) bool {
	if _, err := os.Stat(destPath); err != nil {
		return false // File doesn't exist, proceed
	}

	var existingQuality string
	var existingSize int64
	err := database.DB.QueryRow(query, destPath).Scan(&existingQuality, &existingSize)
	if err != nil {
		return false // Can't compare, proceed
	}

	comp := CompareQuality(candidateQuality, existingQuality)
	if comp < 0 {
		// Candidate is LOWER quality than existing
		slog.Info("Candidate is lower quality, deleting candidate", "candidate_path", candidatePath, "candidate_quality", candidateQuality, "existing_quality", existingQuality)
		deleteCandidate()
		return true
	} else if comp == 0 {
		// Qualities are equal, compare size
		if candidateSize <= existingSize {
			slog.Info("Candidate has same quality but smaller/equal size, deleting candidate", "candidate_path", candidatePath, "quality", candidateQuality)
			deleteCandidate()
			return true
		}
		// Candidate is larger, proceed to replace
		slog.Info("Candidate has same quality but larger size, replacing existing", "candidate_path", candidatePath, "quality", candidateQuality)
	} else {
		// Candidate is HIGHER quality
		slog.Info("Candidate is higher quality, replacing existing", "candidate_path", candidatePath, "candidate_quality", candidateQuality, "existing_quality", existingQuality)
	}

	// Remove existing file and its DB entry
	deleteExisting()
	return false
}

func sanitizePath(name string) string {
	// Remove or replace characters that are problematic for filesystems
	// Specifically :, /, \, *, ?, ", <, >, |
	replacer := strings.NewReplacer(
		":", " -",
		"/", "-",
		"\\", "-",
		"*", "",
		"?", "",
		"\"", "",
		"<", "",
		">", "",
		"|", "-",
	)
	sanitized := replacer.Replace(name)
	// Remove trailing dots and spaces
	sanitized = strings.TrimRight(sanitized, ". ")
	return sanitized
}

func cleanTitleTags(title string) string {
	// Remove common uploader/junk patterns and tags without nuking everything after
	// We use a list of specific tags to remove
	tags := []string{"- IMPORTED", "RARBG", "YTS", "YIFY", "Eztv", "1337x", "GalaxyRG", "TGX", "PSA", "VXT", "EVO", "MeGusta", "AVS", "SNEAKY", "BRRip", "WEB-DL", "BluRay", "1080p", "720p", "2160p", "x264", "x265", "HEVC", "H264", "H265"}

	cleaned := title
	for _, tag := range tags {
		re := regexp.MustCompile(`(?i)\s*[-_.]?\s*` + regexp.QuoteMeta(tag) + `\b`)
		cleaned = re.ReplaceAllString(cleaned, "")
	}

	// Also remove generic [brackets] or {braces} if they didn't match ID patterns
	bracketRegex := regexp.MustCompile(`\s*[\[\{].*?[\]\}]`)
	cleaned = bracketRegex.ReplaceAllString(cleaned, "")

	return strings.Trim(cleaned, " -._")
}

func ParseMediaName(name string) (string, int, string, string, string) {
	var tmdbID, tvdbID, imdbID string

	// 1. Extract and clean ID tags like [tmdbid-343423], {tmdb-343423}, [tvdb-12345], etc.
	idRegex := regexp.MustCompile(`(?i)[\[\{](tmdb|tvdb|tmdbid|imdb)[- ]?([a-z0-9]+)[\]\}]`)
	matches := idRegex.FindAllStringSubmatch(name, -1)
	for _, match := range matches {
		tag := strings.ToLower(match[1])
		id := match[2]
		if tag == "tmdb" || tag == "tmdbid" {
			tmdbID = id
		} else if tag == "tvdb" {
			tvdbID = id
		} else if tag == "imdb" {
			imdbID = id
		}
	}
	name = idRegex.ReplaceAllString(name, "")

	// 2. Remove SXXEXX or SXX patterns
	seasonEpRegex := regexp.MustCompile(`(?i)\s*[-_.]?\s*(S\d+E\d+|S\d+)\b`)
	name = seasonEpRegex.ReplaceAllString(name, "")

	// 3. Match "Title (Year)" first if possible
	re := regexp.MustCompile(`^(.*?)\s*\((\d{4})\)$`)
	yearMatches := re.FindStringSubmatch(strings.TrimSpace(name))

	title := name
	year := 0
	if len(yearMatches) == 3 {
		title = strings.TrimSpace(yearMatches[1])
		year, _ = strconv.Atoi(yearMatches[2])
	}

	// 4. Clean tags from the title
	title = cleanTitleTags(title)

	return title, year, tmdbID, tvdbID, imdbID
}

func RenameAndMoveMovie(cfg *config.Config, movieID int) error {
	return RenameAndMoveMovieWithCleanup(cfg, movieID, false)
}

func RenameAndMoveMovieWithCleanup(cfg *config.Config, movieID int, doCleanup bool) error {
	var m models.Movie
	query := `SELECT id, title, year, tmdb_id, imdb_id, path, quality, size, poster_path FROM movies WHERE id = $1`
	err := database.DB.QueryRow(query, movieID).Scan(&m.ID, &m.Title, &m.Year, &m.TMDBID, &m.IMDBID, &m.Path, &m.Quality, &m.Size, &m.PosterPath)
	if err != nil {
		return err
	}

	if m.TMDBID == "" {
		return fmt.Errorf("movie must be matched before renaming")
	}

	ext := filepath.Ext(m.Path)
	cleanedTitle := cleanTitleTags(m.Title)
	sanitizedTitle := sanitizePath(cleanedTitle)
	newName := fmt.Sprintf("%s (%d) {tmdb-%s}%s", sanitizedTitle, m.Year, m.TMDBID, ext)

	// Create destination directory: Movies/Title (Year) {tmdb-id}/Title (Year) {tmdb-id}.ext
	destDirName := fmt.Sprintf("%s (%d) {tmdb-%s}", sanitizedTitle, m.Year, m.TMDBID)
	destDirPath := filepath.Join(cfg.MoviesPath, destDirName)
	destPath := filepath.Join(destDirPath, newName)

	// Lock both source and destination paths to prevent concurrent operations
	unlockSrc := lockPath(m.Path)
	defer unlockSrc()
	unlockDst := lockPath(destPath)
	defer unlockDst()
	unlockDir := lockPath(destDirPath)
	defer unlockDir()

	if err := os.MkdirAll(destDirPath, 0755); err != nil {
		return err
	}

	if m.Path == destPath {
		return nil // Already in correct place
	}

	// SMART RENAMING LOGIC: Quality Check
	shouldDelete := handleExistingFile(
		destPath, m.Path, m.Quality, m.Size,
		"SELECT quality, size FROM movies WHERE path = $1",
		func() error {
			os.Remove(m.Path)
			database.DB.Exec("DELETE FROM movies WHERE id = $1", m.ID)
			if doCleanup {
				CleanupEmptyDirs(cfg.IncomingMoviesPath)
			}
			return nil
		},
		func() error {
			os.Remove(destPath)
			database.DB.Exec("DELETE FROM movies WHERE path = $1", destPath)
			return nil
		},
	)
	if shouldDelete {
		return nil
	}

	// Move the file
	oldPath := m.Path
	if err := safeRename(oldPath, destPath); err != nil {
		return err
	}
	slog.Info("Successfully moved movie", "old_path", oldPath, "new_path", destPath)

	// Copy the poster if it exists and is in the same directory as the movie
	newPosterPath := ""
	if m.PosterPath != "" {
		if strings.HasPrefix(m.PosterPath, filepath.Dir(oldPath)) {
			posterExt := filepath.Ext(m.PosterPath)
			newPosterPath = filepath.Join(destDirPath, "poster"+posterExt)
			if err := copyFile(m.PosterPath, newPosterPath); err != nil {
				slog.Error("Failed to copy poster", "error", err, "old_path", m.PosterPath, "new_path", newPosterPath)
				newPosterPath = "" // Reset if failed
			} else {
				slog.Info("Copied movie poster", "old_path", m.PosterPath, "new_path", newPosterPath)
			}
		} else {
			// If it's a TMDB URL or already in library, keep it as is
			newPosterPath = m.PosterPath
		}
	}

	// Cleanup old directory if it was in incoming (only if requested)
	if doCleanup {
		CleanupEmptyDirs(cfg.IncomingMoviesPath)
	}

	// Update DB with new path and status
	updateQuery := `UPDATE movies SET path = $1, poster_path = $2, status = 'ready', updated_at = CURRENT_TIMESTAMP WHERE id = $3`
	_, err = database.DB.Exec(updateQuery, destPath, newPosterPath, m.ID)
	if err != nil {
		return err
	}

	// Trigger subtitle download
	go func() {
		if err := DownloadSubtitlesForMovie(cfg, m.ID); err != nil {
			slog.Error("Subtitle download failed for movie", "movie_id", m.ID, "title", m.Title, "error", err)
		}
	}()

	return nil
}

func RenameAndMoveEpisode(cfg *config.Config, episodeID int) error {
	return RenameAndMoveEpisodeWithCleanup(cfg, episodeID, false)
}

func RenameAndMoveEpisodeWithCleanup(cfg *config.Config, episodeID int, doCleanup bool) error {
	var e models.Episode
	var s models.Season
	var sh models.Show

	query := `
		SELECT e.id, e.episode_number, e.title, e.file_path, e.quality, e.size, s.season_number, sh.title, sh.year, sh.tvdb_id, sh.imdb_id, sh.poster_path
		FROM episodes e
		JOIN seasons s ON e.season_id = s.id
		JOIN shows sh ON s.show_id = sh.id
		WHERE e.id = $1
	`
	err := database.DB.QueryRow(query, episodeID).Scan(&e.ID, &e.EpisodeNumber, &e.Title, &e.FilePath, &e.Quality, &e.Size, &s.SeasonNumber, &sh.Title, &sh.Year, &sh.TVDBID, &sh.IMDBID, &sh.PosterPath)
	if err != nil {
		return err
	}

	// TV Shows: Title (Year) {tvdb-ID}/Season XX/Title - SXXEXX - Episode Title.ext
	ext := filepath.Ext(e.FilePath)

	cleanedShowTitle := cleanTitleTags(sh.Title)
	sanitizedShowTitle := sanitizePath(cleanedShowTitle)

	// Better episode title cleaning
	epTitle := e.Title
	// If title looks like a scene filename or raw file name, clean it
	if strings.Contains(epTitle, ".") || strings.Contains(epTitle, "-") || strings.Contains(strings.ToLower(epTitle), "s0") {
		// Clean the episode title using the shared parser
		epTitle, _, _, _, _ = ParseMediaName(epTitle)
	}

	if epTitle == "" || strings.EqualFold(epTitle, sh.Title) {
		epTitle = fmt.Sprintf("Episode %d", e.EpisodeNumber)
	}

	sanitizedEpTitle := sanitizePath(epTitle)

	showDirName := fmt.Sprintf("%s (%d)", sanitizedShowTitle, sh.Year)
	if sh.TVDBID != "" {
		showDirName = fmt.Sprintf("%s (%d) {tvdb-%s}", sanitizedShowTitle, sh.Year, sh.TVDBID)
	}
	seasonDirName := fmt.Sprintf("Season %02d", s.SeasonNumber)

	newFileName := fmt.Sprintf("%s - S%02dE%02d - %s%s", sanitizedShowTitle, s.SeasonNumber, e.EpisodeNumber, sanitizedEpTitle, ext)

	destDirPath := filepath.Join(cfg.ShowsPath, showDirName, seasonDirName)
	destPath := filepath.Join(destDirPath, newFileName)

	// Lock both source and destination paths to prevent concurrent operations
	unlockSrc := lockPath(e.FilePath)
	defer unlockSrc()
	unlockDst := lockPath(destPath)
	defer unlockDst()
	unlockDir := lockPath(destDirPath)
	defer unlockDir()

	if err := os.MkdirAll(destDirPath, 0755); err != nil {
		return err
	}

	if e.FilePath == destPath {
		return nil
	}

	// SMART RENAMING LOGIC: Quality Check for Episodes
	shouldDelete := handleExistingFile(
		destPath, e.FilePath, e.Quality, e.Size,
		"SELECT quality, size FROM episodes WHERE file_path = $1",
		func() error {
			os.Remove(e.FilePath)
			database.DB.Exec("DELETE FROM episodes WHERE id = $1", e.ID)
			if doCleanup {
				CleanupEmptyDirs(cfg.IncomingShowsPath)
			}
			return nil
		},
		func() error {
			os.Remove(destPath)
			database.DB.Exec("DELETE FROM episodes WHERE file_path = $1", destPath)
			return nil
		},
	)
	if shouldDelete {
		return nil
	}

	oldPath := e.FilePath
	if err := safeRename(oldPath, destPath); err != nil {
		return err
	}
	slog.Info("Successfully moved episode", "old_path", oldPath, "new_path", destPath)

	updateQuery := `UPDATE episodes SET file_path = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`
	_, err = database.DB.Exec(updateQuery, destPath, e.ID)
	if err != nil {
		return err
	}

	// Cleanup old directory if it was in incoming (only if requested)
	if doCleanup {
		CleanupEmptyDirs(cfg.IncomingShowsPath)
	}

	// Trigger subtitle download for episode
	go func() {
		if err := DownloadSubtitlesForEpisode(cfg, e.ID); err != nil {
			slog.Error("Subtitle download failed for episode",
				"episode_id", e.ID,
				"show_title", sh.Title,
				"season", s.SeasonNumber,
				"episode", e.EpisodeNumber,
				"error", err)
		}
	}()

	return nil
}

func RenameAndMoveShow(cfg *config.Config, showID int) error {
	return RenameAndMoveShowWithCleanup(cfg, showID, false)
}

func RenameAndMoveShowWithCleanup(cfg *config.Config, showID int, doCleanup bool) error {
	var sh models.Show
	err := database.DB.QueryRow("SELECT id, title, year, tvdb_id, path, poster_path FROM shows WHERE id = $1", showID).Scan(&sh.ID, &sh.Title, &sh.Year, &sh.TVDBID, &sh.Path, &sh.PosterPath)
	if err != nil {
		return err
	}

	cleanedShowTitle := cleanTitleTags(sh.Title)
	sanitizedShowTitle := sanitizePath(cleanedShowTitle)
	showDirName := fmt.Sprintf("%s (%d)", sanitizedShowTitle, sh.Year)
	if sh.TVDBID != "" {
		showDirName = fmt.Sprintf("%s (%d) {tvdb-%s}", sanitizedShowTitle, sh.Year, sh.TVDBID)
	}
	destShowPath := filepath.Join(cfg.ShowsPath, showDirName)

	// Lock the destination show path to prevent concurrent operations
	unlockShow := lockPath(destShowPath)
	defer unlockShow()

	if err := os.MkdirAll(destShowPath, 0755); err != nil {
		return err
	}

	// Move show poster if it exists and is in the incoming folder
	newPosterPath := sh.PosterPath
	if sh.PosterPath != "" && strings.HasPrefix(sh.PosterPath, cfg.IncomingShowsPath) {
		ext := filepath.Ext(sh.PosterPath)
		newPosterPath = filepath.Join(destShowPath, "poster"+ext)
		if _, err := os.Stat(newPosterPath); os.IsNotExist(err) {
			// We'll copy the poster instead of moving it, so the original can be nuked by cleanup
			if err := copyFile(sh.PosterPath, newPosterPath); err != nil {
				slog.Error("Failed to copy show poster", "error", err, "old_path", sh.PosterPath, "new_path", newPosterPath)
				newPosterPath = sh.PosterPath
			} else {
				slog.Info("Copied show poster", "old_path", sh.PosterPath, "new_path", newPosterPath)
			}
		}
	}

	// Fetch all episodes for this show
	query := `
		SELECT e.id
		FROM episodes e
		JOIN seasons s ON e.season_id = s.id
		WHERE s.show_id = $1
	`
	rows, err := database.DB.Query(query, showID)
	if err != nil {
		return err
	}
	defer rows.Close()

		for rows.Next() {
			var epID int
			if err := rows.Scan(&epID); err != nil {
				continue
			}
			if err := RenameAndMoveEpisodeWithCleanup(cfg, epID, false); err != nil {
				slog.Error("Error renaming episode", "episode_id", epID, "error", err)
			}
		}

	// Update show status and path in DB, handling potential duplicates
	var existingID int
	err = database.DB.QueryRow("SELECT id FROM shows WHERE path = $1", destShowPath).Scan(&existingID)
	if err == nil && existingID != showID {
		// Merge: another show already exists at this destination path
		slog.Info("Destination path already exists in DB, merging show", "dest_path", destShowPath, "existing_id", existingID, "show_id", showID)

		// Move seasons to the existing show
		sRows, err := database.DB.Query("SELECT id, season_number FROM seasons WHERE show_id = $1", showID)
		if err == nil {
			defer sRows.Close()
			for sRows.Next() {
				var oldSeasonID, seasonNum int
				if err := sRows.Scan(&oldSeasonID, &seasonNum); err == nil {
					// Check if existing show already has this season
					var newSeasonID int
					errS := database.DB.QueryRow("SELECT id FROM seasons WHERE show_id = $1 AND season_number = $2", existingID, seasonNum).Scan(&newSeasonID)
					if errS == nil {
						// Merge episodes from old season to new season
						database.DB.Exec("UPDATE episodes SET season_id = $1 WHERE season_id = $2", newSeasonID, oldSeasonID)
						database.DB.Exec("DELETE FROM seasons WHERE id = $1", oldSeasonID)
					} else {
						// Just point the old season to the new show
						database.DB.Exec("UPDATE seasons SET show_id = $1 WHERE id = $2", existingID, oldSeasonID)
					}
				}
			}
		}

		// Update metadata on the existing show and delete the duplicate
		_, err = database.DB.Exec(`
			UPDATE shows 
			SET poster_path = CASE WHEN poster_path = '' OR poster_path IS NULL THEN $1 ELSE poster_path END, 
				status = 'ready', 
				updated_at = CURRENT_TIMESTAMP 
			WHERE id = $2`, newPosterPath, existingID)
		database.DB.Exec("DELETE FROM shows WHERE id = $1", showID)
	} else {
		// Normal case: update the current show
		updateQuery := `UPDATE shows SET path = $1, poster_path = $2, status = 'ready', updated_at = CURRENT_TIMESTAMP WHERE id = $3`
		_, err = database.DB.Exec(updateQuery, destShowPath, newPosterPath, showID)
		if err != nil {
			slog.Error("Error updating show in DB", "show_id", showID, "error", err)
		}
	}

	// Cleanup old directory if it was in incoming (only if requested)
	if doCleanup {
		CleanupEmptyDirs(cfg.IncomingShowsPath)
	}

	return nil
}
