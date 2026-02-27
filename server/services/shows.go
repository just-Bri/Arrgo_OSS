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
	"strconv"
	"strings"
	"sync"
)

var scanShowsMutex sync.Mutex

// findRequestTVDBIDFromPath attempts to find a request TVDB ID by checking if any files
// in the show directory have a torrent hash linked to a request.
// This allows exact matching when scanning incoming shows that were requested.
func findRequestTVDBIDFromPath(cfg *config.Config, showPath string) string {
	// Try to find a torrent hash for files in this directory
	qb, err := NewQBittorrentClient(cfg)
	if err != nil {
		return ""
	}

	ctx := context.Background()
	torrents, err := qb.GetTorrentsDetailed(ctx, "")
	if err != nil {
		return ""
	}

	// Check if any torrent's file is inside the show directory
	for _, torrent := range torrents {
		normalizedHash := strings.ToLower(torrent.Hash)
		showPathClean := filepath.Clean(showPath)

		// Fetch files for this torrent
		tFiles, err := qb.GetTorrentFiles(ctx, normalizedHash)
		if err != nil {
			continue
		}

		matched := false
		for _, tf := range tFiles {
			fullPath := filepath.Join(torrent.SavePath, tf.Name)
			if strings.HasPrefix(fullPath, showPathClean) {
				matched = true
				break
			}
		}

		if matched {
			// Look up the request via downloads table
			var requestTVDBID sql.NullString
			err := database.DB.QueryRow(`
				SELECT r.tvdb_id 
				FROM requests r 
				JOIN downloads d ON r.id = d.request_id 
				WHERE LOWER(d.torrent_hash) = $1 
				AND r.media_type = 'show'
				AND r.tvdb_id IS NOT NULL 
				AND r.tvdb_id != ''
				LIMIT 1`, normalizedHash).Scan(&requestTVDBID)

			if err == nil && requestTVDBID.Valid {
				slog.Info("Found request TVDB ID from torrent hash",
					"show_path", showPath,
					"torrent_hash", normalizedHash,
					"tvdb_id", requestTVDBID.String)
				return requestTVDBID.String
			}
		}
	}

	return ""
}

// findRequestTVDBIDFromEpisodes attempts to find a request TVDB ID by checking if any episodes
// in the show have a torrent hash linked to a request.
func findRequestTVDBIDFromEpisodes(showID int) string {
	// Find any episode with a torrent hash
	var episodeHash sql.NullString
	err := database.DB.QueryRow(`
		SELECT e.torrent_hash 
		FROM episodes e
		JOIN seasons s ON e.season_id = s.id
		WHERE s.show_id = $1 
		AND e.torrent_hash IS NOT NULL 
		AND e.torrent_hash != ''
		LIMIT 1`, showID).Scan(&episodeHash)

	if err != nil || !episodeHash.Valid {
		return ""
	}

	normalizedHash := strings.ToLower(episodeHash.String)

	// Look up the request via downloads table
	var requestTVDBID sql.NullString
	err = database.DB.QueryRow(`
		SELECT r.tvdb_id 
		FROM requests r 
		JOIN downloads d ON r.id = d.request_id 
		WHERE LOWER(d.torrent_hash) = $1 
		AND r.media_type = 'show'
		AND r.tvdb_id IS NOT NULL 
		AND r.tvdb_id != ''
		LIMIT 1`, normalizedHash).Scan(&requestTVDBID)

	if err == nil && requestTVDBID.Valid {
		return requestTVDBID.String
	}

	return ""
}

func ScanShows(ctx context.Context, cfg *config.Config, onlyIncoming bool) error {
	scanType := ScanShowLibrary
	if onlyIncoming {
		scanType = ScanIncomingShows
	}

	if !scanShowsMutex.TryLock() {
		slog.Info("Show scan already in progress, skipping")
		return nil
	}
	defer func() {
		scanShowsMutex.Unlock()
		FinishScan(scanType)
	}()

	slog.Info("Starting show scan", "scan_type", scanType, "workers", DefaultWorkerCount)

	// Clean up missing files first
	PurgeMissingShows()

	type showTask struct {
		root string
		name string
	}

	taskChan := make(chan showTask, TaskChannelBufferSize)
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
					processShowDir(cfg, task.root, task.name)
				}
			}
		}()
	}

	// Scan paths based on preference
	var paths []string
	if onlyIncoming {
		paths = []string{cfg.IncomingShowsPath}
	} else {
		paths = []string{cfg.ShowsPath}
	}

	for _, p := range paths {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); os.IsNotExist(err) {
			slog.Debug("Path does not exist, skipping", "path", p)
			continue
		}

		entries, err := os.ReadDir(p)
		if err != nil {
			continue
		}

		stopped := false
		for _, entry := range entries {
			select {
			case <-ctx.Done():
				stopped = true
			default:
				if entry.IsDir() {
					taskChan <- showTask{root: p, name: entry.Name()}
				}
			}
			if stopped {
				break
			}
		}
		if stopped {
			break
		}
	}

	close(taskChan)
	wg.Wait()

	if ctx.Err() == context.Canceled {
		slog.Info("Show scan cancelled", "scan_type", scanType)
	} else {
		slog.Info("Show scan complete", "scan_type", scanType)
	}

	return nil
}

func processShowDir(cfg *config.Config, root string, name string) {
	showPath := filepath.Join(root, name)
	title, year, tmdbID, tvdbID, imdbID := ParseMediaName(name) // Use the new shared ParseMediaName

	// Look for local poster
	posterPath := findLocalPoster(showPath)

	// If this is an incoming show, try to find a request linked to torrent hash
	// This allows us to use the exact TVDB ID from the request instead of fuzzy matching
	if strings.HasPrefix(showPath, cfg.IncomingShowsPath) {
		requestTVDBID := findRequestTVDBIDFromPath(cfg, showPath)
		if requestTVDBID != "" {
			slog.Info("Found request TVDB ID for incoming show", "path", showPath, "tvdb_id", requestTVDBID, "parsed_title", title)
			tvdbID = requestTVDBID
		}
	}

	slog.Debug("Processing show", "title", title, "year", year, "path", showPath)

	// For incoming shows, check if a show with the same TVDB ID or title/year already exists
	// This prevents creating duplicate show entries for different torrent folders
	var showID int
	if strings.HasPrefix(showPath, cfg.IncomingShowsPath) {
		// First try to match by TVDB ID if available
		if tvdbID != "" {
			var existingID int
			err := database.DB.QueryRow(`
				SELECT id FROM shows 
				WHERE tvdb_id = $1 
				LIMIT 1`,
				tvdbID).Scan(&existingID)
			if err == nil && existingID > 0 {
				showID = existingID
				slog.Debug("Reusing existing show by TVDB ID",
					"show_id", showID,
					"tvdb_id", tvdbID,
					"new_path", showPath)
			}
		}

		// If no match by TVDB ID, try matching by title and year
		if showID == 0 && title != "" {
			var existingID int
			var existingYear sql.NullInt64
			err := database.DB.QueryRow(`
				SELECT id, year FROM shows 
				WHERE LOWER(title) = LOWER($1)
				LIMIT 1`,
				title).Scan(&existingID, &existingYear)
			if err == nil && existingID > 0 {
				// Match year if both have years and they match, or if neither has a year
				yearMatch := (year == 0 && !existingYear.Valid) ||
					(year != 0 && existingYear.Valid && year == int(existingYear.Int64))
				if yearMatch {
					showID = existingID
					slog.Debug("Reusing existing incoming show by title/year",
						"show_id", showID,
						"title", title,
						"year", year,
						"new_path", showPath)
				}
			}
		}
	}

	// If no existing show found, create/update normally
	if showID == 0 {
		var err error
		showID, err = upsertShow(models.Show{
			Title:      title,
			Year:       year,
			TMDBID:     tmdbID,
			TVDBID:     tvdbID,
			IMDBID:     imdbID,
			Path:       showPath,
			PosterPath: posterPath,
			Status:     "discovered",
		})
		if err != nil {
			slog.Error("Error upserting show", "title", title, "error", err)
			return
		}
	} else {
		// Update existing show's metadata if needed (but keep the original path)
		// We don't update the path because episodes can come from multiple folders
		// But we do update TVDB/TMDB IDs if they're missing
		_, err := database.DB.Exec(`
			UPDATE shows 
			SET tmdb_id = COALESCE(NULLIF($1, ''), tmdb_id),
				tvdb_id = COALESCE(NULLIF($2, ''), tvdb_id),
				imdb_id = COALESCE(NULLIF($3, ''), imdb_id),
				poster_path = COALESCE(NULLIF($4, ''), poster_path),
				updated_at = CURRENT_TIMESTAMP
			WHERE id = $5`,
			tmdbID, tvdbID, imdbID, posterPath, showID)
		if err != nil {
			slog.Warn("Failed to update existing show metadata", "show_id", showID, "error", err)
		} else {
			slog.Debug("Updated existing show metadata", "show_id", showID, "tvdb_id", tvdbID)
		}
	}

	// Fetch metadata immediately
	MatchShow(cfg, showID)

	// Always scan the current path's seasons/episodes
	// Even if we reused an existing show, we need to scan this new path
	scanSeasons(showID, showPath)

	// After scanning episodes, check if we can find a request TVDB ID from episode torrent hashes
	// This is a fallback in case the initial check didn't find a match
	if strings.HasPrefix(showPath, cfg.IncomingShowsPath) {
		// Check if show still doesn't have a TVDB ID
		var currentTVDBID sql.NullString
		err := database.DB.QueryRow("SELECT tvdb_id FROM shows WHERE id = $1", showID).Scan(&currentTVDBID)
		if err == nil && (!currentTVDBID.Valid || currentTVDBID.String == "") {
			// Try to find TVDB ID from episode torrent hashes
			episodeTVDBID := findRequestTVDBIDFromEpisodes(showID)
			if episodeTVDBID != "" {
				slog.Info("Found request TVDB ID from episode torrent hashes",
					"show_id", showID,
					"show_path", showPath,
					"tvdb_id", episodeTVDBID)
				// Update the show with the TVDB ID and re-match
				database.DB.Exec("UPDATE shows SET tvdb_id = $1 WHERE id = $2", episodeTVDBID, showID)
				MatchShow(cfg, showID)
			}
		}
	}
}

func upsertShow(show models.Show) (int, error) {
	var id int
	query := `
		INSERT INTO shows (title, year, tmdb_id, tvdb_id, imdb_id, path, poster_path, status, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, CURRENT_TIMESTAMP)
		ON CONFLICT (path) DO UPDATE SET
			title = EXCLUDED.title,
			year = EXCLUDED.year,
			tmdb_id = COALESCE(NULLIF(EXCLUDED.tmdb_id, ''), shows.tmdb_id),
			tvdb_id = COALESCE(NULLIF(EXCLUDED.tvdb_id, ''), shows.tvdb_id),
			imdb_id = COALESCE(NULLIF(EXCLUDED.imdb_id, ''), shows.imdb_id),
			poster_path = COALESCE(NULLIF(EXCLUDED.poster_path, ''), shows.poster_path),
			updated_at = CURRENT_TIMESTAMP
		RETURNING id
	`
	err := database.DB.QueryRow(query, show.Title, show.Year, show.TMDBID, show.TVDBID, show.IMDBID, show.Path, show.PosterPath, show.Status).Scan(&id)
	return id, err
}

func scanSeasons(showID int, showPath string) {
	entries, err := os.ReadDir(showPath)
	if err != nil {
		return
	}

	seasonRegex := regexp.MustCompile(`(?i)Season\s+(\d+)`)
	// Also match patterns like [S02], S02, Season02, etc.
	altSeasonRegex := regexp.MustCompile(`(?i)[\[_]?S(?:eason)?\s*(\d+)[\]_]?`)

	var foundSeasonFolders bool

	// First pass: look for standard "Season XX" folders
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		matches := seasonRegex.FindStringSubmatch(entry.Name())
		if len(matches) < 2 {
			continue
		}

		foundSeasonFolders = true
		seasonNum, _ := strconv.Atoi(matches[1])
		seasonPath := filepath.Join(showPath, entry.Name())

		seasonID, err := upsertSeason(showID, seasonNum)
		if err != nil {
			continue
		}

		scanEpisodes(seasonID, seasonPath)
	}

	// Second pass: if no standard Season folders found, try alternative patterns
	if !foundSeasonFolders {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			matches := altSeasonRegex.FindStringSubmatch(entry.Name())
			if len(matches) < 2 {
				continue
			}

			seasonNum, _ := strconv.Atoi(matches[1])
			seasonPath := filepath.Join(showPath, entry.Name())

			seasonID, err := upsertSeason(showID, seasonNum)
			if err != nil {
				continue
			}

			scanEpisodes(seasonID, seasonPath)
		}
	}

	// Third pass: if still no season folders found, check if episodes are directly in show folder
	// Extract season number from show folder name if possible (e.g., "[S02]" or "Season 1" in folder name)
	if !foundSeasonFolders {
		showDirName := filepath.Base(showPath)

		// Try altSeasonRegex first (matches [S02], S02, Season02, etc.)
		matches := altSeasonRegex.FindStringSubmatch(showDirName)
		if len(matches) < 2 {
			// Try explicit "Season X" pattern (handles "Fargo Season 1")
			matches = seasonRegex.FindStringSubmatch(showDirName)
		}

		if len(matches) >= 2 {
			// Show folder itself contains season info (e.g., "Show Name [S02]" or "Show Name Season 1")
			seasonNum, _ := strconv.Atoi(matches[1])
			seasonID, err := upsertSeason(showID, seasonNum)
			if err == nil {
				scanEpisodes(seasonID, showPath)
			}
		} else {
			// No season info in folder name, try to detect season from episode files
			// Scan episodes directly from show folder and try to infer season from filenames
			scanEpisodesFromShowFolder(showID, showPath)
		}
	}
}

// scanEpisodesFromShowFolder scans episodes directly from show folder and infers season from filenames
func scanEpisodesFromShowFolder(showID int, showPath string) {
	entries, err := os.ReadDir(showPath)
	if err != nil {
		return
	}

	// Match SXXEXX pattern to extract both season and episode
	episodeRegex := regexp.MustCompile(`(?i)S(\d+)E(\d+)`)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if !MovieExtensions[ext] {
			continue
		}

		matches := episodeRegex.FindStringSubmatch(entry.Name())
		if len(matches) < 3 {
			continue
		}

		seasonNum, _ := strconv.Atoi(matches[1])
		episodeNum, _ := strconv.Atoi(matches[2])
		episodePath := filepath.Join(showPath, entry.Name())

		seasonID, err := upsertSeason(showID, seasonNum)
		if err != nil {
			continue
		}

		// Clean the episode title
		epNameOnly := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		epTitle, _, _, _, _ := ParseMediaName(epNameOnly)
		if epTitle == "" {
			epTitle = fmt.Sprintf("Episode %d", episodeNum)
		}

		info, _ := entry.Info()
		size := info.Size()
		quality := DetectQuality(episodePath)

		upsertEpisode(seasonID, episodeNum, epTitle, episodePath, quality, size)

		// Try to link torrent hash if file is in incoming folder
		cfg := config.Load()
		if strings.HasPrefix(episodePath, cfg.IncomingShowsPath) {
			if qb, err := NewQBittorrentClient(cfg); err == nil {
				LinkTorrentHashToFile(cfg, qb, episodePath, "show")
			}
		}
	}
}

func upsertSeason(showID int, seasonNum int) (int, error) {
	var id int
	query := `
		INSERT INTO seasons (show_id, season_number, updated_at)
		VALUES ($1, $2, CURRENT_TIMESTAMP)
		ON CONFLICT (show_id, season_number) DO UPDATE SET
			updated_at = CURRENT_TIMESTAMP
		RETURNING id
	`
	err := database.DB.QueryRow(query, showID, seasonNum).Scan(&id)
	return id, err
}

func scanEpisodes(seasonID int, seasonPath string) {
	entries, err := os.ReadDir(seasonPath)
	if err != nil {
		return
	}

	// Match SXXEXX
	episodeRegex := regexp.MustCompile(`(?i)S\d+E(\d+)`)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if !MovieExtensions[ext] {
			continue
		}

		matches := episodeRegex.FindStringSubmatch(entry.Name())
		if len(matches) < 2 {
			continue
		}

		episodeNum, _ := strconv.Atoi(matches[1])
		episodePath := filepath.Join(seasonPath, entry.Name())

		// Clean the episode title (remove SXXEXX, tags like - IMPORTED, etc.)
		epNameOnly := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		epTitle, _, _, _, _ := ParseMediaName(epNameOnly)

		// If ParseMediaName left it empty or it's just the show title, use a better default
		if epTitle == "" {
			epTitle = fmt.Sprintf("Episode %d", episodeNum)
		}

		// Get file info, handle potential nil/error gracefully
		var size int64
		info, err := entry.Info()
		if err == nil && info != nil {
			size = info.Size()
		} else {
			// Fallback: try to get file size directly if entry.Info() fails
			if fileInfo, err := os.Stat(episodePath); err == nil {
				size = fileInfo.Size()
			}
		}
		quality := DetectQuality(episodePath)

		upsertEpisode(seasonID, episodeNum, epTitle, episodePath, quality, size)

		// Try to link torrent hash if file is in incoming folder
		cfg := config.Load()
		if strings.HasPrefix(episodePath, cfg.IncomingShowsPath) {
			if qb, err := NewQBittorrentClient(cfg); err == nil {
				LinkTorrentHashToFile(cfg, qb, episodePath, "show")
			}
		}
	}
}

func upsertEpisode(seasonID int, episodeNum int, title string, path string, quality string, size int64) {
	query := `
		INSERT INTO episodes (season_id, episode_number, title, file_path, quality, size, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, CURRENT_TIMESTAMP)
		ON CONFLICT (file_path) DO UPDATE SET
			episode_number = EXCLUDED.episode_number,
			title = EXCLUDED.title,
			quality = EXCLUDED.quality,
			size = EXCLUDED.size,
			updated_at = CURRENT_TIMESTAMP
	`
	if _, err := database.DB.Exec(query, seasonID, episodeNum, title, path, quality, size); err != nil {
		slog.Error("Error upserting episode", "season_id", seasonID, "episode", episodeNum, "path", path, "error", err)
	}
}

func GetShowCount(excludeIncomingPath string) (int, error) {
	var count int
	var err error
	if excludeIncomingPath != "" {
		err = database.DB.QueryRow("SELECT COUNT(*) FROM shows WHERE path NOT LIKE $1 || '%'", excludeIncomingPath).Scan(&count)
	} else {
		err = database.DB.QueryRow("SELECT COUNT(*) FROM shows").Scan(&count)
	}
	return count, err
}

func PurgeMissingShows() {
	slog.Debug("Checking for missing shows")

	// Check Shows
	rows, err := database.DB.Query("SELECT id, path FROM shows")
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
			slog.Info("Removing missing show from DB", "show_id", id, "path", path)
			database.DB.Exec("DELETE FROM shows WHERE id = $1", id)
		}
	}

	// Also check individual episodes
	epRows, err := database.DB.Query("SELECT id, file_path FROM episodes")
	if err != nil {
		return
	}
	defer epRows.Close()

	for epRows.Next() {
		var id int
		var path string
		if err := epRows.Scan(&id, &path); err != nil {
			continue
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			slog.Info("Removing missing episode from DB", "episode_id", id, "path", path)
			database.DB.Exec("DELETE FROM episodes WHERE id = $1", id)
		}
	}
}

func SearchShowsLocal(query string) ([]models.Show, error) {
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
		SELECT id, title, year, tvdb_id, imdb_id, path, overview, poster_path, genres, status, created_at, updated_at 
		FROM shows 
		WHERE %s
		ORDER BY title ASC
	`, strings.Join(conditions, " OR "))

	rows, err := database.DB.Query(dbQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var shows []models.Show
	for rows.Next() {
		var s models.Show
		var tvdbID, imdbID, overview, posterPath, genres sql.NullString
		err := rows.Scan(&s.ID, &s.Title, &s.Year, &tvdbID, &imdbID, &s.Path, &overview, &posterPath, &genres, &s.Status, &s.CreatedAt, &s.UpdatedAt)
		if err != nil {
			return nil, err
		}
		s.TVDBID = tvdbID.String
		s.IMDBID = imdbID.String
		s.Overview = overview.String
		s.PosterPath = posterPath.String
		s.Genres = genres.String
		shows = append(shows, s)
	}
	return shows, nil
}

func GetShows() ([]models.Show, error) {
	query := `SELECT id, title, year, tvdb_id, imdb_id, path, overview, poster_path, genres, status, created_at, updated_at FROM shows ORDER BY title ASC`
	rows, err := database.DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	shows := []models.Show{}
	for rows.Next() {
		var s models.Show
		var tvdbID, imdbID, overview, posterPath, genres sql.NullString
		err := rows.Scan(&s.ID, &s.Title, &s.Year, &tvdbID, &imdbID, &s.Path, &overview, &posterPath, &genres, &s.Status, &s.CreatedAt, &s.UpdatedAt)
		if err != nil {
			return nil, err
		}
		s.TVDBID = tvdbID.String
		s.IMDBID = imdbID.String
		s.Overview = overview.String
		s.PosterPath = posterPath.String
		s.Genres = genres.String
		shows = append(shows, s)
	}
	return shows, nil
}

func GetIncomingShows(incomingPath string) ([]models.Show, error) {
	// A show is "incoming" if it has episodes in the incoming path that haven't been imported
	query := `
		SELECT DISTINCT s.id, s.title, s.year, s.tvdb_id, s.imdb_id, s.path, s.overview, s.poster_path, s.genres, s.status, s.created_at, s.updated_at
		FROM shows s
		JOIN seasons sn ON s.id = sn.show_id
		JOIN episodes e ON sn.id = e.season_id
		WHERE e.file_path LIKE $1 || '%' AND e.imported_at IS NULL
		ORDER BY s.title ASC`
	rows, err := database.DB.Query(query, incomingPath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	shows := []models.Show{}
	for rows.Next() {
		var s models.Show
		var tvdbID, imdbID, overview, posterPath, genres sql.NullString
		err := rows.Scan(&s.ID, &s.Title, &s.Year, &tvdbID, &imdbID, &s.Path, &overview, &posterPath, &genres, &s.Status, &s.CreatedAt, &s.UpdatedAt)
		if err != nil {
			return nil, err
		}
		s.TVDBID = tvdbID.String
		s.IMDBID = imdbID.String
		s.Overview = overview.String
		s.PosterPath = posterPath.String
		s.Genres = genres.String
		shows = append(shows, s)
	}
	return shows, nil
}

func GetShowByID(id int) (*models.Show, error) {
	query := `SELECT id, title, year, tvdb_id, imdb_id, path, overview, poster_path, genres, status, created_at, updated_at FROM shows WHERE id = $1`
	var s models.Show
	var tvdbID, imdbID, overview, posterPath, genres sql.NullString
	err := database.DB.QueryRow(query, id).Scan(&s.ID, &s.Title, &s.Year, &tvdbID, &imdbID, &s.Path, &overview, &posterPath, &genres, &s.Status, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	s.TVDBID = tvdbID.String
	s.IMDBID = imdbID.String
	s.Overview = overview.String
	s.PosterPath = posterPath.String
	s.Genres = genres.String
	return &s, nil
}

type SeasonWithEpisodes struct {
	models.Season
	Episodes []models.Episode
}

func GetShowSeasons(showID int) ([]SeasonWithEpisodes, error) {
	query := `SELECT id, show_id, season_number, overview FROM seasons WHERE show_id = $1 ORDER BY season_number ASC`
	rows, err := database.DB.Query(query, showID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var seasons []SeasonWithEpisodes
	for rows.Next() {
		var s SeasonWithEpisodes
		var overview sql.NullString
		err := rows.Scan(&s.ID, &s.ShowID, &s.SeasonNumber, &overview)
		if err != nil {
			return nil, err
		}
		s.Overview = overview.String

		// Fetch episodes for this season
		episodes, err := GetSeasonEpisodes(s.ID)
		if err != nil {
			slog.Error("Error getting episodes for season", "season_id", s.ID, "error", err)
		} else {
			s.Episodes = episodes
		}

		seasons = append(seasons, s)
	}
	return seasons, nil
}

func GetSeasonEpisodes(seasonID int) ([]models.Episode, error) {
	query := `SELECT id, season_id, episode_number, title, file_path, quality, size FROM episodes WHERE season_id = $1 ORDER BY episode_number ASC`
	rows, err := database.DB.Query(query, seasonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var episodes []models.Episode
	for rows.Next() {
		var e models.Episode
		var title, quality sql.NullString
		err := rows.Scan(&e.ID, &e.SeasonID, &e.EpisodeNumber, &title, &e.FilePath, &quality, &e.Size)
		if err != nil {
			return nil, err
		}
		e.Title = title.String
		e.Quality = quality.String
		episodes = append(episodes, e)
	}
	return episodes, nil
}
