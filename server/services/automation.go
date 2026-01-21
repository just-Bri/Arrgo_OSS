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

	// Update download progress every 60 minutes
	updateTicker := time.NewTicker(60 * time.Minute)
	defer updateTicker.Stop()

	// Check subtitle queue every 60 minutes
	subtitleTicker := time.NewTicker(60 * time.Minute)
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
	// For shows with multiple seasons, process each season separately
	if r.MediaType == "show" && r.Seasons != "" {
		return s.processShowRequestWithSeasons(ctx, r)
	}

	// For movies or single-season shows, use the original logic
	return s.processSingleRequest(ctx, r)
}

func (s *AutomationService) processShowRequestWithSeasons(ctx context.Context, r models.Request) error {
	// Parse requested seasons
	requestedSeasons := strings.Split(r.Seasons, ",")
	var seasonsToProcess []string

	// Get existing downloads for this request and check which seasons might already have torrents
	rows, err := database.DB.Query(`
		SELECT torrent_hash, title 
		FROM downloads 
		WHERE request_id = $1 
		AND torrent_hash IS NOT NULL 
		AND torrent_hash != ''`, r.ID)
	existingHashes := make(map[string]bool)
	existingSeasons := make(map[string]bool) // Track which seasons we've found torrents for

	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var hash, title string
			if err := rows.Scan(&hash, &title); err == nil {
				normalizedHash := strings.ToLower(hash)
				existingHashes[normalizedHash] = true
				// Check if torrent still exists in qBittorrent
				existingTorrent, err := s.qb.GetTorrentByHash(ctx, normalizedHash)
				if err == nil && existingTorrent != nil {
					// Update download status
					database.DB.Exec(`
						UPDATE downloads
						SET progress = $1, status = $2, updated_at = NOW()
						WHERE LOWER(torrent_hash) = $3`,
						existingTorrent.Progress, existingTorrent.State, normalizedHash)

					// Try to determine which season this torrent is for by checking the title
					torrentTitle := strings.ToLower(existingTorrent.Name)
					for _, seasonStr := range requestedSeasons {
						seasonStr = strings.TrimSpace(seasonStr)
						if seasonStr == "" {
							continue
						}
						seasonNum, err := strconv.Atoi(seasonStr)
						if err != nil {
							continue
						}
						// Check for season patterns in torrent title
						// Use word boundaries to avoid false matches (e.g., "S10" matching "S1")
						seasonPatterns := []string{
							fmt.Sprintf("s%02d", seasonNum),       // S02
							fmt.Sprintf("s%d", seasonNum),         // S2
							fmt.Sprintf("season %d", seasonNum),   // Season 2
							fmt.Sprintf("season %02d", seasonNum), // Season 02
							fmt.Sprintf("s%02de", seasonNum),      // S02E (episode format)
						}
						matched := false
						for _, pattern := range seasonPatterns {
							if strings.Contains(torrentTitle, pattern) {
								// Additional check: ensure we're not matching a higher season number
								// e.g., "S10" shouldn't match "S1"
								if seasonNum < 10 {
									// For single-digit seasons, check that it's not part of a larger number
									patternIndex := strings.Index(torrentTitle, pattern)
									if patternIndex >= 0 {
										// Check character after the pattern to ensure it's not a digit
										if patternIndex+len(pattern) < len(torrentTitle) {
											nextChar := torrentTitle[patternIndex+len(pattern)]
											if nextChar >= '0' && nextChar <= '9' {
												// This might be part of a larger number (e.g., S1 in S10)
												continue
											}
										}
										matched = true
										break
									}
								} else {
									matched = true
									break
								}
							}
						}
						if matched {
							existingSeasons[seasonStr] = true
							slog.Debug("Found existing torrent for season",
								"request_id", r.ID,
								"season", seasonStr,
								"torrent_title", existingTorrent.Name)
						}
					}
				}
			}
		}
	}

	// Determine which seasons still need processing
	for _, seasonStr := range requestedSeasons {
		seasonStr = strings.TrimSpace(seasonStr)
		if seasonStr == "" {
			continue
		}
		// Only process if we haven't found a torrent for this season yet
		if !existingSeasons[seasonStr] {
			seasonsToProcess = append(seasonsToProcess, seasonStr)
		} else {
			slog.Debug("Season already has torrent, skipping", "request_id", r.ID, "season", seasonStr)
		}
	}

	if len(seasonsToProcess) == 0 {
		slog.Info("All seasons already have torrents", "request_id", r.ID)
		// Check if all torrents are completed
		allCompleted := true
		rows, _ := database.DB.Query(`
			SELECT d.torrent_hash 
			FROM downloads d
			WHERE d.request_id = $1 
			AND d.torrent_hash IS NOT NULL`, r.ID)
		if rows != nil {
			defer rows.Close()
			for rows.Next() {
				var hash string
				if err := rows.Scan(&hash); err == nil {
					torrent, err := s.qb.GetTorrentByHash(ctx, strings.ToLower(hash))
					if err != nil || torrent == nil || (torrent.Progress < 1.0 && torrent.State != "uploading" && torrent.State != "stalledUP") {
						allCompleted = false
						break
					}
				}
			}
		}
		if allCompleted {
			database.DB.Exec("UPDATE requests SET status = 'completed', updated_at = NOW() WHERE id = $1", r.ID)
		} else {
			database.DB.Exec("UPDATE requests SET status = 'downloading', updated_at = NOW() WHERE id = $1", r.ID)
		}
		return nil
	}

	// Process each season separately
	anyProcessed := false
	slog.Info("Processing seasons for show request",
		"request_id", r.ID,
		"title", r.Title,
		"total_seasons", len(seasonsToProcess),
		"seasons", strings.Join(seasonsToProcess, ","))

	for i, seasonStr := range seasonsToProcess {
		seasonStr = strings.TrimSpace(seasonStr)
		if seasonStr == "" {
			continue
		}

		// Create a temporary request for this single season
		singleSeasonReq := r
		singleSeasonReq.Seasons = seasonStr

		slog.Info("Processing season for show request",
			"request_id", r.ID,
			"season", seasonStr,
			"season_index", i+1,
			"total_seasons", len(seasonsToProcess),
			"title", r.Title)
		if err := s.processSingleSeason(ctx, singleSeasonReq); err != nil {
			slog.Error("Failed to process season",
				"request_id", r.ID,
				"season", seasonStr,
				"error", err)
			// Continue with other seasons even if one fails
		} else {
			anyProcessed = true
			slog.Info("Successfully processed season",
				"request_id", r.ID,
				"season", seasonStr)
		}
	}

	// Update request status
	if anyProcessed {
		database.DB.Exec("UPDATE requests SET status = 'downloading', updated_at = NOW() WHERE id = $1", r.ID)
	}

	return nil
}

func (s *AutomationService) processSingleRequest(ctx context.Context, r models.Request) error {
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

	return s.processSingleSeason(ctx, r)
}

func (s *AutomationService) processSingleSeason(ctx context.Context, r models.Request) error {

	// 1. Build search query with season info for shows
	searchType := r.MediaType
	searchQuery := r.Title

	if r.MediaType == "show" {
		searchType = "show"
		// Parse seasons and enhance search query
		if r.Seasons != "" {
			seasons := strings.Split(r.Seasons, ",")
			// Build query with season info: "Show Name S02" or "Show Name Season 2"
			// For single season requests, use the specific season in the query
			seasonNum := strings.TrimSpace(seasons[0])
			if seasonNum != "" {
				// Convert to int for proper zero-padding
				if num, err := strconv.Atoi(seasonNum); err == nil {
					// Format: "Show Name S02" (most common)
					searchQuery = fmt.Sprintf("%s S%02d", r.Title, num)
				} else {
					// Fallback to string format if conversion fails
					searchQuery = fmt.Sprintf("%s S%s", r.Title, seasonNum)
				}
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

	// Add public trackers to magnet link to help qBittorrent fetch metadata faster
	magnetLink = addTrackersToMagnet(magnetLink, infoHash)
	slog.Debug("Enhanced magnet link with trackers",
		"request_id", r.ID,
		"has_trackers", strings.Contains(magnetLink, "&tr="),
		"tracker_count", strings.Count(magnetLink, "&tr="))
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

		// Check if torrent is stuck downloading metadata (metadl state for more than 10 minutes)
		if strings.ToLower(t.State) == "metadl" {
			// Check when this download was last updated
			var lastUpdated time.Time
			err := database.DB.QueryRow(`
				SELECT updated_at FROM downloads WHERE LOWER(torrent_hash) = $1`,
				normalizedHash).Scan(&lastUpdated)
			if err == nil {
				// If stuck for more than 10 minutes, try to force reannounce
				if time.Since(lastUpdated) > 10*time.Minute {
					slog.Warn("Torrent stuck downloading metadata, forcing reannounce",
						"hash", normalizedHash,
						"name", t.Name,
						"stuck_duration", time.Since(lastUpdated))
					if err := s.qb.ReannounceTorrent(ctx, normalizedHash); err != nil {
						slog.Error("Failed to reannounce stuck torrent", "error", err, "hash", normalizedHash)
					} else {
						slog.Info("Successfully reannounced stuck torrent", "hash", normalizedHash)
					}
				}
			}
		}

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

// addTrackersToMagnet adds public trackers to a magnet link to help qBittorrent fetch metadata faster
func addTrackersToMagnet(magnetLink string, infoHash string) string {
	// Mix of UDP and HTTP trackers - HTTP trackers work better through VPNs
	// Some VPNs block UDP, so HTTP trackers are essential
	publicTrackers := []string{
		// HTTP trackers (work better through VPNs)
		"http://tracker.opentrackr.org:1337/announce",
		"http://tracker.openbittorrent.com:80/announce",
		"http://tracker.coppersurfer.tk:6969/announce",
		"http://tracker.leechers-paradise.org:6969/announce",
		"http://tracker.internetwarriors.net:1337/announce",
		"http://exodus.desync.com:6969/announce",
		"http://open.stealth.si:80/announce",
		"http://tracker.torrent.eu.org:451/announce",
		"http://tracker.tiny-vps.com:6969/announce",
		"http://tracker.cyberia.is:6969/announce",
		"http://tracker.dler.org:6969/announce",
		"http://tracker1.itzmx.com:8080/announce",
		"http://tracker2.itzmx.com:6961/announce",
		"http://tracker3.itzmx.com:6961/announce",
		"http://tracker4.itzmx.com:2710/announce",
		// UDP trackers (backup)
		"udp://tracker.opentrackr.org:1337/announce",
		"udp://tracker.openbittorrent.com:80/announce",
		"udp://tracker.coppersurfer.tk:6969/announce",
		"udp://tracker.leechers-paradise.org:6969/announce",
		"udp://tracker.internetwarriors.net:1337/announce",
		"udp://exodus.desync.com:6969/announce",
		"udp://open.stealth.si:80/announce",
		"udp://tracker.torrent.eu.org:451/announce",
		"udp://tracker.tiny-vps.com:6969/announce",
		"udp://tracker.cyberia.is:6969/announce",
	}

	// Extract info hash if not provided
	if infoHash == "" {
		infoHash = extractInfoHashFromMagnet(magnetLink)
	}
	if infoHash == "" {
		// Can't enhance without info hash
		return magnetLink
	}

	// Check if magnet link already has trackers
	if strings.Contains(magnetLink, "&tr=") {
		// Already has trackers, just return as-is
		return magnetLink
	}

	// Build enhanced magnet link with trackers
	enhancedLink := fmt.Sprintf("magnet:?xt=urn:btih:%s", strings.ToLower(infoHash))

	// Add display name if present in original
	if idx := strings.Index(magnetLink, "&dn="); idx != -1 {
		end := strings.Index(magnetLink[idx+4:], "&")
		if end == -1 {
			enhancedLink += magnetLink[idx:]
		} else {
			enhancedLink += magnetLink[idx : idx+4+end]
		}
	}

	// Add all trackers (trackers in magnet links don't need URL encoding)
	for _, tracker := range publicTrackers {
		enhancedLink += "&tr=" + tracker
	}

	return enhancedLink
}
