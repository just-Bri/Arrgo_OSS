package services

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"Arrgo/config"
	"Arrgo/database"
	"Arrgo/models"

	sharedhttp "github.com/justbri/arrgo/shared/http"
)

type AutomationService struct {
	cfg *config.Config
	qb  *QBittorrentClient
}

// Global instance for immediate triggering
var globalAutomationService *AutomationService

// SetGlobalAutomationService sets the global automation service instance
func SetGlobalAutomationService(service *AutomationService) {
	globalAutomationService = service
}

// GetGlobalAutomationService returns the global automation service instance
func GetGlobalAutomationService() *AutomationService {
	return globalAutomationService
}

type TorrentSearchResult struct {
	Title      string `json:"title"`
	MagnetLink string `json:"magnet_link"`
	InfoHash   string `json:"info_hash"`
	Seeds      int    `json:"seeds"`
	Size       string `json:"size"`
	Resolution string `json:"resolution"`
	Quality    string `json:"quality"`
}

func NewAutomationService(cfg *config.Config, qb *QBittorrentClient) *AutomationService {
	return &AutomationService{
		cfg: cfg,
		qb:  qb,
	}
}

func (s *AutomationService) Start(ctx context.Context) {
	slog.Info("Starting Automation Service")

	// Check for approved requests every hour
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	// Update download progress every 30 seconds
	updateTicker := time.NewTicker(30 * time.Second)
	defer updateTicker.Stop()

	// Check subtitle queue every 15 minutes
	subtitleTicker := time.NewTicker(15 * time.Minute)
	defer subtitleTicker.Stop()

	// Wait for qBittorrent to be available before processing requests
	slog.Info("Waiting for qBittorrent to be available before processing requests")
	if err := s.waitForQBittorrent(ctx); err != nil {
		slog.Error("Failed to connect to qBittorrent after retries, requests will be processed on next cycle", "error", err)
	} else {
		slog.Info("qBittorrent is available, processing approved requests on startup")
		s.ProcessApprovedRequests(ctx)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.ProcessApprovedRequests(ctx)
		case <-updateTicker.C:
			s.UpdateDownloadStatus(ctx)
		case <-subtitleTicker.C:
			s.ProcessSubtitleQueue(ctx)
		}
	}
}

// waitForQBittorrent waits for qBittorrent to be available with retries
func (s *AutomationService) waitForQBittorrent(ctx context.Context) error {
	maxRetries := 10
	retryDelay := 5 * time.Second

	for i := 0; i < maxRetries; i++ {
		if err := s.qb.Login(ctx); err == nil {
			return nil
		}
		if i < maxRetries-1 {
			slog.Debug("qBittorrent not ready yet, retrying", "attempt", i+1, "max_retries", maxRetries, "retry_delay", retryDelay)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryDelay):
				// Continue retrying
			}
		}
	}
	return fmt.Errorf("qBittorrent not available after %d retries", maxRetries)
}

// TriggerImmediateProcessing triggers immediate processing of approved requests
func (s *AutomationService) TriggerImmediateProcessing(ctx context.Context) {
	slog.Info("Triggering immediate processing of approved requests")
	s.ProcessApprovedRequests(ctx)
}

func (s *AutomationService) ProcessApprovedRequests(ctx context.Context) {
	var requests []models.Request
	query := `SELECT id, title, media_type, year, tmdb_id, tvdb_id, imdb_id, seasons FROM requests WHERE status = 'approved'`

	slog.Debug("Checking for approved requests to process")
	rows, err := database.DB.Query(query)
	if err != nil {
		slog.Error("Error querying approved requests", "error", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var r models.Request
		var tmdbID, tvdbID, imdbID, seasons sql.NullString
		if err := rows.Scan(&r.ID, &r.Title, &r.MediaType, &r.Year, &tmdbID, &tvdbID, &imdbID, &seasons); err != nil {
			slog.Error("Error scanning request", "error", err)
			continue
		}
		r.TMDBID = tmdbID.String
		r.TVDBID = tvdbID.String
		r.IMDBID = imdbID.String
		r.Seasons = seasons.String
		requests = append(requests, r)
	}

	if len(requests) == 0 {
		slog.Debug("No approved requests found to process")
		return
	}

	slog.Info("Found approved requests to process", "count", len(requests))
	for _, r := range requests {
		slog.Info("Processing approved request", "request_id", r.ID, "title", r.Title, "media_type", r.MediaType, "seasons", r.Seasons)
		if err := s.processRequest(ctx, r); err != nil {
			slog.Error("Failed to process request", "request_id", r.ID, "title", r.Title, "error", err)
		}
	}
}

func (s *AutomationService) processRequest(ctx context.Context, r models.Request) error {
	// 0. Check if this request already has an active download/torrent
	var existingHash string
	err := database.DB.QueryRow(`
		SELECT torrent_hash 
		FROM downloads 
		WHERE request_id = $1 
		AND torrent_hash IS NOT NULL 
		AND torrent_hash != ''
		ORDER BY created_at DESC 
		LIMIT 1`, r.ID).Scan(&existingHash)
	
	if err == nil && existingHash != "" {
		// Check if torrent still exists in qBittorrent
		normalizedHash := strings.ToLower(existingHash)
		existingTorrent, err := s.qb.GetTorrentByHash(ctx, normalizedHash)
		if err == nil && existingTorrent != nil {
			slog.Info("Request already has active torrent in qBittorrent, skipping processing", 
				"request_id", r.ID, 
				"torrent_hash", normalizedHash, 
				"torrent_name", existingTorrent.Name,
				"progress", existingTorrent.Progress,
				"state", existingTorrent.State)
			
			// Update download status to match current torrent state
			database.DB.Exec(`
				UPDATE downloads
				SET progress = $1, status = $2, updated_at = NOW()
				WHERE LOWER(torrent_hash) = $3`,
				existingTorrent.Progress, existingTorrent.State, normalizedHash)
			
			// Update request status if torrent is completed
			if existingTorrent.Progress >= 1.0 || existingTorrent.State == "uploading" || existingTorrent.State == "stalledUP" {
				database.DB.Exec("UPDATE requests SET status = 'completed', updated_at = NOW() WHERE id = $1", r.ID)
			} else {
				database.DB.Exec("UPDATE requests SET status = 'downloading', updated_at = NOW() WHERE id = $1", r.ID)
			}
			
			return nil // Skip processing, torrent already exists
		} else {
			// Torrent hash exists in DB but not in qBittorrent - might have been deleted
			// Continue with processing to re-add it
			slog.Debug("Request has torrent hash in DB but not found in qBittorrent, will re-process", 
				"request_id", r.ID, 
				"torrent_hash", normalizedHash)
		}
	}

	// 1. Build search query with season info for shows
	searchType := r.MediaType
	searchQuery := r.Title

	if r.MediaType == "show" {
		searchType = "show"
		// Parse seasons and enhance search query
		if r.Seasons != "" {
			seasons := strings.Split(r.Seasons, ",")
			// Build query with season info: "Show Name S02" or "Show Name Season 2"
			// Try multiple formats to catch different naming conventions
			seasonQueries := []string{}
			for _, seasonStr := range seasons {
				seasonNum := strings.TrimSpace(seasonStr)
				if seasonNum != "" {
					// Format: "Show Name S02" (most common)
					seasonQueries = append(seasonQueries, fmt.Sprintf("%s S%02s", r.Title, seasonNum))
					// Format: "Show Name Season 2"
					seasonQueries = append(seasonQueries, fmt.Sprintf("%s Season %s", r.Title, seasonNum))
				}
			}
			// Use the first season query for now (we'll filter results by season match)
			if len(seasonQueries) > 0 {
				searchQuery = seasonQueries[0]
			}
		}
	}

	searchURL := sharedhttp.BuildQueryURL(s.cfg.IndexerURL+"/search", map[string]string{
		"q":      searchQuery,
		"type":   searchType,
		"format": "json",
	})

	// Add season parameter for show searches
	if r.MediaType == "show" && r.Seasons != "" {
		searchURL = sharedhttp.BuildQueryURL(s.cfg.IndexerURL+"/search", map[string]string{
			"q":       searchQuery,
			"type":    searchType,
			"seasons": r.Seasons,
			"format":  "json",
		})
	}

	slog.Info("Searching indexer for request", "request_id", r.ID, "title", r.Title, "indexer_url", searchURL)
	resp, err := sharedhttp.MakeRequest(ctx, searchURL, sharedhttp.LongTimeoutClient)
	if err != nil {
		slog.Error("Failed to call indexer", "request_id", r.ID, "error", err, "indexer_url", searchURL)
		return fmt.Errorf("failed to call indexer: %w", err)
	}
	// MakeRequest already checks status code and returns error on non-200, so resp is guaranteed to be OK here

	var results []TorrentSearchResult
	if err := sharedhttp.DecodeJSONResponse(resp, &results); err != nil {
		slog.Error("Failed to decode indexer response", "request_id", r.ID, "error", err)
		return fmt.Errorf("failed to decode indexer response: %w", err)
	}

	slog.Info("Indexer search completed", "request_id", r.ID, "results_count", len(results))

	if len(results) == 0 {
		slog.Info("No results found for request", "request_id", r.ID, "title", r.Title)
		// Update request to track retry attempts - add a retry_count column or use updated_at
		// For now, increment a counter in a comment field or leave as-is for retry
		// The request will be retried on next cycle (5 minutes)
		return nil
	}

	// Log first few results for debugging
	for i, result := range results {
		if i < 3 {
			slog.Debug("Indexer result", "index", i, "title", result.Title, "seeds", result.Seeds, "quality", result.Quality, "resolution", result.Resolution, "has_info_hash", result.InfoHash != "", "has_magnet", result.MagnetLink != "")
		}
	}

	// 2. Choose best result - prioritize 1080p, match seasons, sort by seeds
	best := selectBestResult(results, r.MediaType, r.Seasons)
	if best == nil {
		slog.Warn("No suitable results found after filtering", "request_id", r.ID, "title", r.Title, "total_results", len(results), "seasons", r.Seasons)
		return nil
	}

	slog.Info("Selected best torrent result", "request_id", r.ID, "title", best.Title, "seeds", best.Seeds, "quality", best.Quality, "resolution", best.Resolution, "info_hash", best.InfoHash, "has_magnet", best.MagnetLink != "")

	// Extract or validate InfoHash
	infoHash := best.InfoHash
	if infoHash == "" {
		magnetPreview := best.MagnetLink
		if len(magnetPreview) > 100 {
			magnetPreview = magnetPreview[:100] + "..."
		}
		slog.Debug("InfoHash not in result, extracting from magnet link", "request_id", r.ID, "magnet_preview", magnetPreview)
		// Try to extract from magnet link
		infoHash = extractInfoHashFromMagnet(best.MagnetLink)
		if infoHash == "" {
			slog.Error("Could not extract info hash from magnet link", "request_id", r.ID, "magnet_link", best.MagnetLink)
			return fmt.Errorf("could not extract info hash from magnet link")
		}
		slog.Debug("Extracted info hash from magnet", "request_id", r.ID, "info_hash", infoHash)
	}

	// Validate InfoHash format (should be 40 hex characters)
	if len(infoHash) != 40 {
		slog.Error("Invalid info hash format", "request_id", r.ID, "info_hash", infoHash, "length", len(infoHash))
		return fmt.Errorf("invalid info hash format: %s (expected 40 characters)", infoHash)
	}

	// Normalize to lowercase for consistency (qBittorrent returns lowercase)
	infoHash = strings.ToLower(infoHash)

	slog.Debug("InfoHash validated successfully", "request_id", r.ID, "info_hash", infoHash)

	// 3. Begin Database Transaction FIRST
	tx, err := database.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Update request status to downloading
	_, err = tx.Exec("UPDATE requests SET status = 'downloading', updated_at = NOW() WHERE id = $1", r.ID)
	if err != nil {
		return fmt.Errorf("failed to update request status: %w", err)
	}

	// Add to downloads table
	_, err = tx.Exec(`
		INSERT INTO downloads (request_id, torrent_hash, title, status, updated_at)
		VALUES ($1, $2, $3, 'downloading', NOW())
		ON CONFLICT (torrent_hash) DO NOTHING`,
		r.ID, infoHash, best.Title)
	if err != nil {
		return fmt.Errorf("failed to insert download record: %w", err)
	}

	// Commit transaction before adding to qBittorrent
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// 4. Add to qBittorrent AFTER database is updated
	category := "arrgo-movies"
	savePath := s.cfg.IncomingMoviesPath
	if r.MediaType == "show" {
		category = "arrgo-shows"
		savePath = s.cfg.IncomingShowsPath
	}

	// Check if torrent already exists in qBittorrent before adding
	normalizedHash := strings.ToLower(infoHash)
	existingTorrent, err := s.qb.GetTorrentByHash(ctx, normalizedHash)
	if err == nil && existingTorrent != nil {
		slog.Info("Torrent already exists in qBittorrent, skipping add", "request_id", r.ID, "info_hash", normalizedHash, "torrent_name", existingTorrent.Name)
		// Torrent already exists, update download status to match
		database.DB.Exec(`
			UPDATE downloads
			SET progress = $1, status = $2, updated_at = NOW()
			WHERE LOWER(torrent_hash) = $3`,
			existingTorrent.Progress, existingTorrent.State, normalizedHash)
		return nil
	}

	slog.Info("Adding torrent to qBittorrent", "request_id", r.ID, "info_hash", infoHash, "category", category, "save_path", savePath)
	// If the indexer result does not include a magnet link but has an info hash,
	// construct a magnet link fallback so qBittorrent can add the torrent by info-hash.
	magnetLink := best.MagnetLink
	if magnetLink == "" && infoHash != "" {
		magnetLink = fmt.Sprintf("magnet:?xt=urn:btih:%s", strings.ToLower(infoHash))
		slog.Debug("Constructed magnet link from info hash", "request_id", r.ID, "magnet_preview", magnetLink)
	}
	if err := s.qb.AddTorrent(ctx, magnetLink, category, savePath); err != nil {
		// If qBittorrent add fails, check if it's because torrent already exists
		// (qBittorrent might return an error even if torrent exists)
		existingTorrent, checkErr := s.qb.GetTorrentByHash(ctx, normalizedHash)
		if checkErr == nil && existingTorrent != nil {
			slog.Info("Torrent exists in qBittorrent despite add error, continuing", "request_id", r.ID, "info_hash", normalizedHash)
			// Torrent exists, update download status
			database.DB.Exec(`
				UPDATE downloads
				SET progress = $1, status = $2, updated_at = NOW()
				WHERE LOWER(torrent_hash) = $3`,
				existingTorrent.Progress, existingTorrent.State, normalizedHash)
			return nil
		}

		// If qBittorrent add fails and torrent doesn't exist, rollback the database changes
		// Reset request back to approved so it can be retried
		slog.Error("Failed to add torrent to qBittorrent, resetting request", "request_id", r.ID, "error", err)
		database.DB.Exec("UPDATE requests SET status = 'approved', updated_at = NOW() WHERE id = $1", r.ID)
		database.DB.Exec("DELETE FROM downloads WHERE LOWER(torrent_hash) = $1", normalizedHash)
		return fmt.Errorf("failed to add torrent to qBittorrent: %w", err)
	}

	slog.Info("Successfully processed request", "request_id", r.ID, "title", r.Title, "status", "downloading")
	return nil
}

func (s *AutomationService) UpdateDownloadStatus(ctx context.Context) {
	torrents, err := s.qb.GetTorrents(ctx, "all")
	if err != nil {
		slog.Error("Error getting torrents from qBittorrent", "error", err)
		return
	}

	// Build map of active hashes (normalized to lowercase for comparison)
	activeHashes := make(map[string]bool)
	slog.Debug("Updating download status", "torrent_count", len(torrents))
	for _, t := range torrents {
		// Normalize hash to lowercase for consistent comparison
		normalizedHash := strings.ToLower(t.Hash)
		activeHashes[normalizedHash] = true
		slog.Debug("Found active torrent", "hash_original", t.Hash, "hash_normalized", normalizedHash, "state", t.State, "progress", t.Progress)
		
		// Update our downloads table (use normalized hash for WHERE clause)
		_, err := database.DB.Exec(`
			UPDATE downloads
			SET progress = $1, status = $2, updated_at = NOW()
			WHERE LOWER(torrent_hash) = $3`,
			t.Progress, t.State, normalizedHash)
		if err != nil {
			slog.Error("Error updating download status", "error", err, "torrent_hash", normalizedHash)
			continue
		}

		// If finished, update request status
		if t.Progress >= 1.0 || t.State == "uploading" || t.State == "stalledUP" {
			_, err = database.DB.Exec(`
				UPDATE requests
				SET status = 'completed', updated_at = NOW()
				WHERE id = (SELECT request_id FROM downloads WHERE LOWER(torrent_hash) = $1)`,
				normalizedHash)
			if err != nil {
				slog.Error("Error updating request status to completed", "error", err, "torrent_hash", normalizedHash)
			}
		}
	}

	// SELF-HEALING: Reset requests that are active but missing from qBittorrent
	// We use a 15-minute grace period to avoid transient qBittorrent issues purging our state.
	rows, err := database.DB.Query(`
		SELECT request_id, torrent_hash
		FROM downloads
		WHERE status NOT IN ('completed', 'cancelled')
		AND updated_at < NOW() - INTERVAL '15 minutes'`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var reqID int
			var hash string
			if err := rows.Scan(&reqID, &hash); err == nil {
				// Normalize hash to lowercase for comparison
				normalizedHash := strings.ToLower(hash)
				if !activeHashes[normalizedHash] {
					// Log all active hashes for debugging
					var activeHashList []string
					for h := range activeHashes {
						activeHashList = append(activeHashList, h)
					}
					slog.Warn("Download vanished from qBittorrent for over 15 minutes, resetting request to approved", 
						"torrent_hash", hash, 
						"normalized_hash", normalizedHash, 
						"request_id", reqID,
						"active_hashes_count", len(activeHashes),
						"sample_active_hashes", func() []string {
							if len(activeHashList) > 5 {
								return activeHashList[:5]
							}
							return activeHashList
						}())
					database.DB.Exec("UPDATE requests SET status = 'approved' WHERE id = $1", reqID)
					database.DB.Exec("DELETE FROM downloads WHERE LOWER(torrent_hash) = $1", normalizedHash)
				} else {
					slog.Debug("Download still active in qBittorrent", "torrent_hash", hash, "normalized_hash", normalizedHash, "request_id", reqID)
				}
			}
		}
	}
}

func (s *AutomationService) ProcessSubtitleQueue(ctx context.Context) {
	// 1. Check if we are still in quota lockdown
	var resetStr string
	err := database.DB.QueryRow("SELECT value FROM settings WHERE key = 'opensubtitles_quota_reset'").Scan(&resetStr)
	if err == nil {
		if t, err := time.Parse(time.RFC3339, resetStr); err == nil {
			if time.Now().Before(t.Add(5 * time.Minute)) {
				// Still in lockdown
				slog.Debug("Still in OpenSubtitles quota lockdown", "reset_time", t.Add(5*time.Minute))
				return
			}
		}
	}

	// 2. Fetch pending jobs that are ready for retry
	rows, err := database.DB.Query("SELECT id, media_type, media_id FROM subtitle_queue WHERE next_retry <= CURRENT_TIMESTAMP")
	if err != nil {
		slog.Error("Error querying subtitle queue", "error", err)
		return
	}
	defer rows.Close()

	type job struct {
		id    int
		mType string
		mID   int
	}
	var jobs []job
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.id, &j.mType, &j.mID); err == nil {
			jobs = append(jobs, j)
		}
	}

	for _, j := range jobs {
		slog.Info("Retrying subtitle download", "media_type", j.mType, "media_id", j.mID)
		var err error
		if j.mType == "movie" {
			err = DownloadSubtitlesForMovie(s.cfg, j.mID)
		} else {
			err = DownloadSubtitlesForEpisode(s.cfg, j.mID)
		}

		if err == nil {
			// Success! Remove from queue
			database.DB.Exec("DELETE FROM subtitle_queue WHERE id = $1", j.id)
			slog.Info("Successfully downloaded subtitles on retry", "media_type", j.mType, "media_id", j.mID)
		} else {
			// Check if it was a quota error again
			if strings.Contains(err.Error(), "406") {
				// Quota hit again, next_retry was updated by QueueSubtitleDownload called inside DownloadSubtitlesForX
				slog.Warn("Hit quota again while retrying subtitle download", "media_type", j.mType, "media_id", j.mID)
				break // Stop processing queue for now
			} else {
				// Some other error, increment retry count and back off
				database.DB.Exec("UPDATE subtitle_queue SET retry_count = retry_count + 1, next_retry = CURRENT_TIMESTAMP + interval '1 hour' WHERE id = $1", j.id)

				var retries int
				database.DB.QueryRow("SELECT retry_count FROM subtitle_queue WHERE id = $1", j.id).Scan(&retries)
				if retries > 5 {
					slog.Warn("Giving up on subtitles after 5 retries", "media_type", j.mType, "media_id", j.mID, "retries", retries)
					database.DB.Exec("DELETE FROM subtitle_queue WHERE id = $1", j.id)
				}
			}
		}
	}
}

// selectBestResult selects the best torrent result based on seeds, quality, season matching, and minimum requirements
func selectBestResult(results []TorrentSearchResult, mediaType string, requestedSeasons string) *TorrentSearchResult {
	if len(results) == 0 {
		return nil
	}

	// Log sample results for debugging
	if len(results) > 0 {
		sample := results[0]
		slog.Debug("Sample torrent result", "title", sample.Title, "seeds", sample.Seeds, "quality", sample.Quality, "resolution", sample.Resolution, "info_hash", sample.InfoHash)
	}

	// Parse requested seasons
	var requestedSeasonNums []int
	if requestedSeasons != "" {
		seasonStrs := strings.Split(requestedSeasons, ",")
		for _, s := range seasonStrs {
			if num, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
				requestedSeasonNums = append(requestedSeasonNums, num)
			}
		}
	}

	// Filter by minimum seeds (at least 1 seed required)
	var filtered []TorrentSearchResult
	zeroSeedCount := 0
	for _, r := range results {
		if r.Seeds > 0 {
			filtered = append(filtered, r)
		} else {
			zeroSeedCount++
		}
	}

	if len(filtered) == 0 {
		slog.Warn("All results filtered out due to zero seeds", "total_results", len(results), "zero_seed_count", zeroSeedCount)
		// If all results have 0 seeds, still try to use the first one (might be a new torrent)
		if len(results) > 0 {
			slog.Info("Using first result despite zero seeds", "title", results[0].Title, "seeds", results[0].Seeds)
			return &results[0]
		}
		return nil
	}

	slog.Debug("Filtered results", "total_results", len(results), "filtered_count", len(filtered), "zero_seed_count", zeroSeedCount)

	// Score function: higher is better
	scoreResult := func(r *TorrentSearchResult) int {
		score := 0
		titleLower := strings.ToLower(r.Title)

		// Prioritize 1080p (highest priority)
		if strings.Contains(titleLower, "1080") || strings.Contains(strings.ToLower(r.Resolution), "1080") {
			score += 1000
		} else if strings.Contains(titleLower, "720") || strings.Contains(strings.ToLower(r.Resolution), "720") {
			score += 500
		} else if strings.Contains(titleLower, "480") || strings.Contains(strings.ToLower(r.Resolution), "480") {
			score += 100
		}

		// Season matching bonus (for shows)
		if mediaType == "show" && len(requestedSeasonNums) > 0 {
			for _, seasonNum := range requestedSeasonNums {
				// Match patterns like S02, S2, Season 2, Season 02
				seasonPatterns := []string{
					fmt.Sprintf("s%02d", seasonNum),
					fmt.Sprintf("s%d", seasonNum),
					fmt.Sprintf("season %d", seasonNum),
					fmt.Sprintf("season %02d", seasonNum),
				}
				for _, pattern := range seasonPatterns {
					if strings.Contains(titleLower, pattern) {
						score += 500 // Big bonus for season match
						break
					}
				}
			}
		}

		// Seeds contribute to score (but less than quality/season match)
		score += r.Seeds

		return score
	}

	// Find best result
	best := &filtered[0]
	bestScore := scoreResult(best)

	for i := 1; i < len(filtered); i++ {
		current := &filtered[i]
		currentScore := scoreResult(current)
		if currentScore > bestScore {
			best = current
			bestScore = currentScore
		}
	}

	slog.Debug("Selected best result", "title", best.Title, "seeds", best.Seeds, "resolution", best.Resolution, "score", bestScore)
	return best
}

// extractInfoHashFromMagnet extracts the info hash from a magnet link
func extractInfoHashFromMagnet(magnetLink string) string {
	// Magnet link format: magnet:?xt=urn:btih:HASH&dn=...
	// Look for "xt=urn:btih:" followed by 40 hex characters
	prefix := "xt=urn:btih:"
	idx := strings.Index(magnetLink, prefix)
	if idx == -1 {
		return ""
	}

	start := idx + len(prefix)
	if start+40 > len(magnetLink) {
		return ""
	}

	hash := magnetLink[start : start+40]
	// Validate it's hex
	for _, c := range hash {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return ""
		}
	}

	return hash
}
