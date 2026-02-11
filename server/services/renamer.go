package services

import (
	"Arrgo/config"
	"Arrgo/database"
	"Arrgo/models"
	"context"
	"database/sql"
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
	tags := []string{
		"- IMPORTED", "RARBG", "YTS", "YIFY", "Eztv", "1337x", "GalaxyRG", "TGX", "PSA", "VXT", "EVO", "MeGusta", "AVS", "SNEAKY",
		"BRRip", "WEB-DL", "WEB-DLRip", "WEBRip", "BluRay", "BDRip", "DVDRip", "HDTV", "PDTV", "SDTV",
		"1080p", "720p", "480p", "2160p", "4K", "UHD",
		"x264", "x265", "HEVC", "H264", "H265", "AVC", "XviD", "DivX",
		"AC3", "DTS", "AAC", "MP3", "DDP", "DD5.1", "DTS-HD", "TrueHD",
		"Subs", "Sub", "Dub", "Dubbed", "Multi", "Multi-Audio",
		"REPACK", "PROPER", "READNFO", "NFO",
		"EXTENDED", "EXTENDED CUT", "DIRECTOR'S CUT", "DIRECTORS CUT", "UNRATED", "UNRATED CUT",
		"THEATRICAL", "THEATRICAL CUT", "FINAL CUT", "SPECIAL EDITION", "COLLECTOR'S EDITION",
		"MOVIE", "THE MOVIE", "COMPLETE SERIES", "FULL SERIES", "OAV", "OVA", "ONASHIFT", "OAD", "ONA",
	}

	cleaned := title
	for _, tag := range tags {
		re := regexp.MustCompile(`(?i)\s*[-_.]?\s*` + regexp.QuoteMeta(tag) + `\b`)
		cleaned = re.ReplaceAllString(cleaned, "")
	}

	// Remove generic [brackets] or {braces} if they didn't match ID patterns
	bracketRegex := regexp.MustCompile(`\s*[\[\{].*?[\]\}]`)
	cleaned = bracketRegex.ReplaceAllString(cleaned, "")

	// Remove parentheses content that isn't a year (4 digits)
	// This handles cases like "(Карусель)" or "(Dub)" etc.
	parenRegex := regexp.MustCompile(`\s*\([^)]*\)`)
	cleaned = parenRegex.ReplaceAllStringFunc(cleaned, func(match string) string {
		// Check if it's a year pattern (4 digits)
		yearMatch := regexp.MustCompile(`^\((\d{4})\)$`)
		if yearMatch.MatchString(match) {
			return match // Keep year parentheses
		}
		return "" // Remove other parentheses content
	})

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

	// 2b. Remove "Season X" or "Season XX" patterns (case-insensitive)
	// This handles folder names like "Fargo Season 1" -> "Fargo"
	seasonTextRegex := regexp.MustCompile(`(?i)\s*[-_.]?\s*Season\s+\d+\b`)
	name = seasonTextRegex.ReplaceAllString(name, "")

	// 3. Extract year from anywhere in the string (not just at the end)
	// First try to match "Title (Year)" at the end for clean names
	re := regexp.MustCompile(`^(.*?)\s*\((\d{4})\)$`)
	yearMatches := re.FindStringSubmatch(strings.TrimSpace(name))

	title := name
	year := 0
	if len(yearMatches) == 3 {
		// Year is at the end
		title = strings.TrimSpace(yearMatches[1])
		year, _ = strconv.Atoi(yearMatches[2])
	} else {
		// Try to find year pattern anywhere in the string (e.g., "Title (Year) 1080p")
		yearRegex := regexp.MustCompile(`\((\d{4})\)`)
		yearMatch := yearRegex.FindStringSubmatch(name)
		if len(yearMatch) == 2 {
			year, _ = strconv.Atoi(yearMatch[1])
			// Extract title part before the year
			parts := yearRegex.Split(name, 2)
			if len(parts) > 0 {
				title = strings.TrimSpace(parts[0])
			}
		}
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
	var torrentHash sql.NullString
	query := `SELECT id, title, year, tmdb_id, imdb_id, path, quality, size, poster_path, torrent_hash FROM movies WHERE id = $1`
	err := database.DB.QueryRow(query, movieID).Scan(&m.ID, &m.Title, &m.Year, &m.TMDBID, &m.IMDBID, &m.Path, &m.Quality, &m.Size, &m.PosterPath, &torrentHash)
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

	// Check seeding criteria BEFORE moving - if still seeding, copy instead of move
	oldPath := m.Path
	shouldCopyInsteadOfMove := false
	if torrentHash.Valid && torrentHash.String != "" && strings.HasPrefix(oldPath, cfg.IncomingMoviesPath) {
		if qb, err := NewQBittorrentClient(cfg); err == nil {
			ctx := context.Background()
			meetsCriteria, err := CheckSeedingCriteriaOnImport(ctx, cfg, qb, torrentHash.String)
			if err == nil && !meetsCriteria {
				// Still seeding, copy instead of move to keep original for seeding
				shouldCopyInsteadOfMove = true
				slog.Info("Movie still seeding, copying instead of moving", "movie_id", m.ID, "torrent_hash", torrentHash.String)
			}
		}
	}

	// Move or copy the file based on seeding status
	if shouldCopyInsteadOfMove {
		if err := copyFile(oldPath, destPath); err != nil {
			return err
		}
		slog.Info("Successfully copied movie (still seeding)", "old_path", oldPath, "new_path", destPath)
	} else {
		if err := safeRename(oldPath, destPath); err != nil {
			return err
		}
		slog.Info("Successfully moved movie", "old_path", oldPath, "new_path", destPath)
	}

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

	// Update DB with new path and status, mark as imported
	updateQuery := `UPDATE movies SET path = $1, poster_path = $2, status = 'ready', imported_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE id = $3`
	_, err = database.DB.Exec(updateQuery, destPath, newPosterPath, m.ID)
	if err != nil {
		return err
	}

	// Check seeding criteria and clean up torrent if needed (only if we moved, not copied)
	if !shouldCopyInsteadOfMove && torrentHash.Valid && torrentHash.String != "" && strings.HasPrefix(oldPath, cfg.IncomingMoviesPath) {
		// Try to get qBittorrent client
		if qb, err := NewQBittorrentClient(cfg); err == nil {
			go func() {
				ctx := context.Background()
				if err := CleanupTorrentOnImport(ctx, cfg, qb, torrentHash.String, oldPath); err != nil {
					slog.Error("Failed to cleanup torrent on movie import", "movie_id", m.ID, "error", err)
				}
			}()
		}
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
	var torrentHash sql.NullString

	query := `
		SELECT e.id, e.episode_number, e.title, e.file_path, e.quality, e.size, e.torrent_hash, s.season_number, sh.id, sh.title, sh.year, sh.tvdb_id, sh.imdb_id, sh.poster_path
		FROM episodes e
		JOIN seasons s ON e.season_id = s.id
		JOIN shows sh ON s.show_id = sh.id
		WHERE e.id = $1
	`
	err := database.DB.QueryRow(query, episodeID).Scan(&e.ID, &e.EpisodeNumber, &e.Title, &e.FilePath, &e.Quality, &e.Size, &torrentHash, &s.SeasonNumber, &sh.ID, &sh.Title, &sh.Year, &sh.TVDBID, &sh.IMDBID, &sh.PosterPath)
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

	// Check seeding criteria BEFORE moving - if still seeding, copy instead of move
	oldPath := e.FilePath
	shouldCopyInsteadOfMove := false
	if torrentHash.Valid && torrentHash.String != "" && strings.HasPrefix(oldPath, cfg.IncomingShowsPath) {
		if qb, err := NewQBittorrentClient(cfg); err == nil {
			ctx := context.Background()
			meetsCriteria, err := CheckSeedingCriteriaOnImport(ctx, cfg, qb, torrentHash.String)
			if err == nil && !meetsCriteria {
				// Still seeding, copy instead of move to keep original for seeding
				shouldCopyInsteadOfMove = true
				slog.Info("Episode still seeding, copying instead of moving", "episode_id", e.ID, "torrent_hash", torrentHash.String)
			}
		}
	}

	// Move or copy the file based on seeding status
	if shouldCopyInsteadOfMove {
		if err := copyFile(oldPath, destPath); err != nil {
			return err
		}
		slog.Info("Successfully copied episode (still seeding)", "old_path", oldPath, "new_path", destPath)
	} else {
		if err := safeRename(oldPath, destPath); err != nil {
			return err
		}
		slog.Info("Successfully moved episode", "old_path", oldPath, "new_path", destPath)
	}

	// Update DB with new path, mark as imported
	updateQuery := `UPDATE episodes SET file_path = $1, imported_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE id = $2`
	_, err = database.DB.Exec(updateQuery, destPath, e.ID)
	if err != nil {
		return err
	}

	// Rescan the show directory to ensure all episodes are detected and added to the database
	// This is important after importing episodes so the library is up-to-date
	go func() {
		showDirPath := filepath.Join(cfg.ShowsPath, showDirName)
		scanSeasons(sh.ID, showDirPath)
		slog.Info("Rescanned show directory after episode import",
			"show_id", sh.ID,
			"show_title", sh.Title,
			"season", s.SeasonNumber,
			"episode", e.EpisodeNumber)
	}()

	// Check seeding criteria and clean up torrent if needed (only if we moved, not copied)
	if !shouldCopyInsteadOfMove && torrentHash.Valid && torrentHash.String != "" && strings.HasPrefix(oldPath, cfg.IncomingShowsPath) {
		// Try to get qBittorrent client
		if qb, err := NewQBittorrentClient(cfg); err == nil {
			go func() {
				ctx := context.Background()
				if err := CleanupTorrentOnImport(ctx, cfg, qb, torrentHash.String, oldPath); err != nil {
					slog.Error("Failed to cleanup torrent on episode import",
						"episode_id", e.ID,
						"error", err)
				}
			}()
		}
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
		// Pass doCleanup flag to episodes so they clean up empty directories as they're moved
		if err := RenameAndMoveEpisodeWithCleanup(cfg, epID, doCleanup); err != nil {
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

	// Always cleanup empty directories in incoming if the show was in incoming
	// This ensures empty directories are removed after episodes are moved/copied
	if strings.HasPrefix(sh.Path, cfg.IncomingShowsPath) {
		CleanupEmptyDirs(cfg.IncomingShowsPath)
	}

	return nil
}
