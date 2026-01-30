package services

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"Arrgo/config"
	"Arrgo/database"
	"Arrgo/models"

	sharedconfig "github.com/justbri/arrgo/shared/config"
	sharedhttp "github.com/justbri/arrgo/shared/http"
	"golang.org/x/net/html"
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
	Peers      int    `json:"peers"`
	Size       string `json:"size"`
	Source     string `json:"source"`
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

	// Check for pending requests every hour
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
		slog.Info("qBittorrent is available, processing all pending requests on startup")
		// First, check and fix any "downloading" requests that don't actually have active torrents
		s.ValidateDownloadingRequests(ctx)
		// Then process all pending requests
		s.ProcessPendingRequestsOnStartup(ctx)
	}

	// Check for missing subtitles on startup
	go s.CheckMediaSubtitles(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.ProcessPendingRequests(ctx)
		case <-updateTicker.C:
			s.UpdateDownloadStatus(ctx)
		case <-subtitleTicker.C:
			s.ProcessSubtitleQueue(ctx)
		}
	}
}

func (s *AutomationService) CheckMediaSubtitles(ctx context.Context) {
	slog.Info("Checking all media for missing subtitles")

	// 1. Check Movies
	movieRows, err := database.DB.Query("SELECT id, path FROM movies")
	if err != nil {
		slog.Error("Error querying movies for subtitle check", "error", err)
	} else {
		defer movieRows.Close()
		movieCount := 0
		queuedCount := 0
		for movieRows.Next() {
			var id int
			var path string
			if err := movieRows.Scan(&id, &path); err == nil {
				movieCount++
				if !HasSubtitles(path) {
					if err := QueueSubtitleDownload("movie", id); err != nil {
						slog.Error("Failed to queue movie subtitle download", "movie_id", id, "error", err)
					} else {
						queuedCount++
					}
				}
			}
		}
		slog.Info("Movie subtitle check completed", "total_movies", movieCount, "queued_subtitles", queuedCount)
	}

	// 2. Check Episodes
	episodeRows, err := database.DB.Query("SELECT id, file_path FROM episodes")
	if err != nil {
		slog.Error("Error querying episodes for subtitle check", "error", err)
	} else {
		defer episodeRows.Close()
		episodeCount := 0
		queuedCount := 0
		for episodeRows.Next() {
			var id int
			var path string
			if err := episodeRows.Scan(&id, &path); err == nil {
				episodeCount++
				if !HasSubtitles(path) {
					if err := QueueSubtitleDownload("episode", id); err != nil {
						slog.Error("Failed to queue episode subtitle download", "episode_id", id, "error", err)
					} else {
						queuedCount++
					}
				}
			}
		}
		slog.Info("Episode subtitle check completed", "total_episodes", episodeCount, "queued_subtitles", queuedCount)
	}
}

// waitForQBittorrent waits for qBittorrent to be available with retries
// This accounts for VPN containers that need time to establish VPN connection before qBittorrent web UI is available
// Phase 1: Check every 30 seconds for 10 minutes (20 attempts) - allows time for VPN setup
// Phase 2: If still not ready, check every 5 minutes indefinitely until ready
func (s *AutomationService) waitForQBittorrent(ctx context.Context) error {
	// Phase 1: Frequent checks during initial startup (VPN setup period)
	phase1Retries := 20                                          // 20 attempts
	phase1Delay := 30 * time.Second                              // Every 30 seconds
	phase1Duration := time.Duration(phase1Retries) * phase1Delay // 10 minutes total

	// Phase 2: Less frequent checks if still not ready
	phase2Delay := 5 * time.Minute // Every 5 minutes

	var attempt int

	slog.Info("Starting qBittorrent readiness check",
		"phase1_retries", phase1Retries,
		"phase1_delay", phase1Delay,
		"phase2_delay", phase2Delay)

	// Phase 1: Check every 30 seconds for 10 minutes
	for attempt = 0; attempt < phase1Retries; attempt++ {
		err := s.qb.Login(ctx)
		if err == nil {
			slog.Info("qBittorrent is now available", "attempt", attempt+1, "phase", "1")
			return nil
		}

		slog.Debug("qBittorrent not ready yet (phase 1), retrying",
			"attempt", attempt+1,
			"max_phase1_retries", phase1Retries,
			"retry_delay", phase1Delay,
			"error", err)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(phase1Delay):
			// Continue retrying
		}
	}

	// Phase 2: Check every 5 minutes until ready
	slog.Info("qBittorrent still not ready after initial wait period, switching to periodic checks",
		"phase1_duration", phase1Duration,
		"phase2_delay", phase2Delay)

	for {
		err := s.qb.Login(ctx)
		if err == nil {
			slog.Info("qBittorrent is now available", "attempt", attempt+1, "phase", "2")
			return nil
		}

		attempt++

		slog.Info("qBittorrent not ready yet (phase 2), will retry",
			"attempt", attempt+1,
			"retry_delay", phase2Delay,
			"error", err,
			"url", s.cfg.QBittorrentURL)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(phase2Delay):
			// Continue retrying
		}
	}
}

// TriggerImmediateProcessing triggers immediate processing of pending requests
func (s *AutomationService) TriggerImmediateProcessing(ctx context.Context) {
	slog.Info("Triggering immediate processing of pending requests")
	s.ProcessPendingRequests(ctx)
}

// ValidateDownloadingRequests checks requests marked as "downloading" and resets them to "pending" if they don't have active torrents
func (s *AutomationService) ValidateDownloadingRequests(ctx context.Context) {
	query := `SELECT id, title FROM requests WHERE status = 'downloading'`
	
	slog.Debug("Validating downloading requests on startup")
	rows, err := database.DB.Query(query)
	if err != nil {
		slog.Error("Error querying downloading requests", "error", err)
		return
	}
	defer rows.Close()

	var resetCount int
	for rows.Next() {
		var requestID int
		var title string
		if err := rows.Scan(&requestID, &title); err != nil {
			slog.Error("Error scanning downloading request", "error", err)
			continue
		}

		// Check if this request has an active torrent
		var existingHash string
		err := database.DB.QueryRow(`
			SELECT torrent_hash 
			FROM downloads 
			WHERE request_id = $1 
			AND torrent_hash IS NOT NULL 
			AND torrent_hash != ''
			ORDER BY created_at DESC 
			LIMIT 1`, requestID).Scan(&existingHash)

		if err != nil || existingHash == "" {
			// No torrent hash in downloads table, reset to pending
			slog.Warn("Downloading request has no torrent hash, resetting to pending", 
				"request_id", requestID, 
				"title", title)
			database.DB.Exec("UPDATE requests SET status = 'pending', updated_at = NOW() WHERE id = $1", requestID)
			resetCount++
			continue
		}

		// Check if torrent exists in qBittorrent
		normalizedHash := strings.ToLower(existingHash)
		existingTorrent, err := s.qb.GetTorrentByHash(ctx, normalizedHash)
		if err != nil || existingTorrent == nil {
			// Torrent doesn't exist in qBittorrent, reset to pending
			// Get request details for better logging
			var requestYear int
			var requestTVDBID string
			var downloadTitle string
			database.DB.QueryRow("SELECT year, tvdb_id FROM requests WHERE id = $1", requestID).Scan(&requestYear, &requestTVDBID)
			database.DB.QueryRow("SELECT title FROM downloads WHERE request_id = $1 AND LOWER(torrent_hash) = $2 LIMIT 1", requestID, normalizedHash).Scan(&downloadTitle)
			
			slog.Warn("Downloading request has no active torrent in qBittorrent, resetting to pending", 
				"request_id", requestID, 
				"title", title,
				"year", requestYear,
				"tvdb_id", requestTVDBID,
				"torrent_hash", normalizedHash,
				"download_title", downloadTitle,
				"error", err)
			database.DB.Exec("UPDATE requests SET status = 'pending', updated_at = NOW() WHERE id = $1", requestID)
			// Also clean up the downloads record
			database.DB.Exec("DELETE FROM downloads WHERE LOWER(torrent_hash) = $1", normalizedHash)
			resetCount++
		} else {
			// Torrent exists, update status based on progress
			if existingTorrent.Progress >= 1.0 || existingTorrent.State == "uploading" || existingTorrent.State == "stalledUP" {
				database.DB.Exec("UPDATE requests SET status = 'completed', updated_at = NOW() WHERE id = $1", requestID)
			}
			// Update download status
			database.DB.Exec(`
				UPDATE downloads
				SET progress = $1, status = $2, updated_at = NOW()
				WHERE LOWER(torrent_hash) = $3`,
				existingTorrent.Progress, existingTorrent.State, normalizedHash)
		}
	}

	if resetCount > 0 {
		slog.Info("Reset downloading requests to pending", "count", resetCount)
	}
}

// ProcessPendingRequestsOnStartup processes all pending requests on startup, ignoring retry timing
func (s *AutomationService) ProcessPendingRequestsOnStartup(ctx context.Context) {
	var requests []models.Request
	query := `SELECT id, title, media_type, year, tmdb_id, tvdb_id, imdb_id, seasons, retry_count, last_search_at FROM requests WHERE status = 'pending'`

	slog.Debug("Checking for pending requests to process on startup")
	rows, err := database.DB.Query(query)
	if err != nil {
		slog.Error("Error querying pending requests", "error", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var r models.Request
		var tmdbID, tvdbID, imdbID, seasons sql.NullString
		var lastSearchAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.Title, &r.MediaType, &r.Year, &tmdbID, &tvdbID, &imdbID, &seasons, &r.RetryCount, &lastSearchAt); err != nil {
			slog.Error("Error scanning request", "error", err)
			continue
		}
		// Decode unicode escape sequences in title (e.g., \u0026 -> &)
		r.Title = decodeUnicodeEscapes(r.Title)
		r.TMDBID = tmdbID.String
		r.TVDBID = tvdbID.String
		r.IMDBID = imdbID.String
		r.Seasons = seasons.String
		if lastSearchAt.Valid {
			r.LastSearchAt = &lastSearchAt.Time
		}

		// If retries are exhausted but still marked as pending, mark as not_found
		if r.RetryCount >= 54 {
			slog.Warn("Request has exhausted retries but still marked as pending, marking as not_found", "request_id", r.ID, "retry_count", r.RetryCount)
			database.DB.Exec("UPDATE requests SET status = 'not_found', updated_at = NOW() WHERE id = $1", r.ID)
			continue
		}

		// On startup, process all pending requests regardless of retry timing
		requests = append(requests, r)
	}

	if len(requests) == 0 {
		slog.Debug("No pending requests to process on startup")
		return
	}

	slog.Info("Found pending requests to process on startup", "count", len(requests))
	for _, r := range requests {
		slog.Info("Processing pending request on startup", "request_id", r.ID, "title", r.Title, "media_type", r.MediaType, "seasons", r.Seasons, "retry_count", r.RetryCount)
		if err := s.processRequest(ctx, r); err != nil {
			slog.Error("Failed to process request", "request_id", r.ID, "title", r.Title, "error", err)
		}
	}
}

func (s *AutomationService) ProcessPendingRequests(ctx context.Context) {
	var requests []models.Request
	query := `SELECT id, title, media_type, year, tmdb_id, tvdb_id, imdb_id, seasons, retry_count, last_search_at FROM requests WHERE status = 'pending'`

	slog.Debug("Checking for pending requests to process")
	rows, err := database.DB.Query(query)
	if err != nil {
		slog.Error("Error querying pending requests", "error", err)
		return
	}
	defer rows.Close()

	now := time.Now()
	for rows.Next() {
		var r models.Request
		var tmdbID, tvdbID, imdbID, seasons sql.NullString
		var lastSearchAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.Title, &r.MediaType, &r.Year, &tmdbID, &tvdbID, &imdbID, &seasons, &r.RetryCount, &lastSearchAt); err != nil {
			slog.Error("Error scanning request", "error", err)
			continue
		}
		// Decode unicode escape sequences in title (e.g., \u0026 -> &)
		r.Title = decodeUnicodeEscapes(r.Title)
		r.TMDBID = tmdbID.String
		r.TVDBID = tvdbID.String
		r.IMDBID = imdbID.String
		r.Seasons = seasons.String
		if lastSearchAt.Valid {
			r.LastSearchAt = &lastSearchAt.Time
		}

		// If retries are exhausted but still marked as pending, mark as not_found
		if r.RetryCount >= 54 {
			slog.Warn("Request has exhausted retries but still marked as pending, marking as not_found", "request_id", r.ID, "retry_count", r.RetryCount)
			database.DB.Exec("UPDATE requests SET status = 'not_found', updated_at = NOW() WHERE id = $1", r.ID)
			continue
		}

		// Check if this request is ready for retry based on retry count
		if !s.isReadyForRetry(r, now) {
			timeSinceLastSearch := now.Sub(*r.LastSearchAt)
			var requiredWait time.Duration
			if r.RetryCount < 24 {
				requiredWait = 1 * time.Hour
			} else if r.RetryCount < 54 {
				requiredWait = 24 * time.Hour
			}
			timeUntilReady := requiredWait - timeSinceLastSearch
			slog.Debug("Request not ready for retry yet", 
				"request_id", r.ID, 
				"retry_count", r.RetryCount, 
				"last_search_at", r.LastSearchAt,
				"time_since_last_search", timeSinceLastSearch,
				"required_wait", requiredWait,
				"time_until_ready", timeUntilReady)
			continue
		}

		requests = append(requests, r)
	}

	if len(requests) == 0 {
		slog.Debug("No pending requests ready to process")
		return
	}

	slog.Info("Found pending requests to process", "count", len(requests))
	for _, r := range requests {
		slog.Info("Processing pending request", "request_id", r.ID, "title", r.Title, "media_type", r.MediaType, "seasons", r.Seasons, "retry_count", r.RetryCount)
		if err := s.processRequest(ctx, r); err != nil {
			slog.Error("Failed to process request", "request_id", r.ID, "title", r.Title, "error", err)
		}
	}
}

// isReadyForRetry determines if a request is ready to be retried based on retry count and timing
// Returns true if the request should be searched now
func (s *AutomationService) isReadyForRetry(r models.Request, now time.Time) bool {
	// First search attempt - always ready
	if r.RetryCount == 0 && r.LastSearchAt == nil {
		return true
	}

	if r.LastSearchAt == nil {
		return true
	}

	timeSinceLastSearch := now.Sub(*r.LastSearchAt)

	// First 24 retries: search every hour
	if r.RetryCount < 24 {
		return timeSinceLastSearch >= 1*time.Hour
	}

	// Retries 24-54 (30 more attempts): search every 24 hours
	if r.RetryCount < 54 {
		return timeSinceLastSearch >= 24*time.Hour
	}

	// After 54 retries (24 hourly + 30 daily), mark as not found
	return false
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

					// Try to determine which season(s) this torrent covers by checking the title
					torrentTitle := strings.ToLower(existingTorrent.Name)
					
					// First, check if this is a multi-season torrent (e.g., "Season 1-4", "S01-S04", "S1-S4")
					multiSeasonRegex := regexp.MustCompile(`(?:season|s)\s*(\d+)\s*[-–]\s*(?:season|s)?\s*(\d+)`)
					multiSeasonMatch := multiSeasonRegex.FindStringSubmatch(torrentTitle)
					if len(multiSeasonMatch) == 3 {
						// Found a multi-season range
						startSeason, err1 := strconv.Atoi(multiSeasonMatch[1])
						endSeason, err2 := strconv.Atoi(multiSeasonMatch[2])
						if err1 == nil && err2 == nil && startSeason <= endSeason {
							// Mark all seasons in the range as covered
							for seasonNum := startSeason; seasonNum <= endSeason; seasonNum++ {
								seasonStr := fmt.Sprintf("%d", seasonNum)
								// Check if this season was requested
								for _, reqSeason := range requestedSeasons {
									if strings.TrimSpace(reqSeason) == seasonStr {
										existingSeasons[seasonStr] = true
										slog.Debug("Found multi-season torrent covering season",
											"request_id", r.ID,
											"season", seasonStr,
											"torrent_title", existingTorrent.Name,
											"range", fmt.Sprintf("%d-%d", startSeason, endSeason))
									}
								}
							}
							continue // Skip individual season matching for multi-season torrents
						}
					}
					
					// Also check for patterns like "S01S02S03" or "S01 S02 S03"
					allSeasonsRegex := regexp.MustCompile(`(?:s|season)\s*(\d+)`)
					allSeasonsMatches := allSeasonsRegex.FindAllStringSubmatch(torrentTitle, -1)
					if len(allSeasonsMatches) > 1 {
						// Found multiple season numbers - check if they're all requested seasons
						foundSeasons := make(map[int]bool)
						for _, match := range allSeasonsMatches {
							if seasonNum, err := strconv.Atoi(match[1]); err == nil {
								foundSeasons[seasonNum] = true
							}
						}
						// Mark all found seasons that are requested as covered
						for _, seasonStr := range requestedSeasons {
							seasonStr = strings.TrimSpace(seasonStr)
							if seasonNum, err := strconv.Atoi(seasonStr); err == nil {
								if foundSeasons[seasonNum] {
									existingSeasons[seasonStr] = true
									slog.Debug("Found multi-season torrent covering season",
										"request_id", r.ID,
										"season", seasonStr,
										"torrent_title", existingTorrent.Name,
										"all_seasons_in_torrent", foundSeasons)
								}
							}
						}
						continue // Skip individual season matching
					}
					
					// Fallback: Check for individual season patterns
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
	// Check if this season is already covered by an existing multi-season torrent
	// BUT: We should still allow downloading a multi-season torrent if it covers seasons we don't have
	// (e.g., if we have season 3 but find a 1-4 torrent, we should download it for seasons 1, 2, 4)
	if r.MediaType == "show" && r.Seasons != "" {
		rows, err := database.DB.Query(`
			SELECT d.torrent_hash 
			FROM downloads d
			WHERE d.request_id = $1 
			AND d.torrent_hash IS NOT NULL 
			AND d.torrent_hash != ''`, r.ID)
		if err == nil {
			defer rows.Close()
			currentSeasonStr := strings.TrimSpace(strings.Split(r.Seasons, ",")[0])
			currentSeasonNum, _ := strconv.Atoi(currentSeasonStr)
			
			// Get all requested seasons for this request to check if ALL are covered
			var allRequestedSeasons []int
			allRequestedSeasonsStr := r.Seasons
			// Try to get the full list from the request if available
			var fullSeasonsList string
			err := database.DB.QueryRow("SELECT seasons FROM requests WHERE id = $1", r.ID).Scan(&fullSeasonsList)
			if err == nil && fullSeasonsList != "" {
				allRequestedSeasonsStr = fullSeasonsList
			}
			for _, sStr := range strings.Split(allRequestedSeasonsStr, ",") {
				if sn, err := strconv.Atoi(strings.TrimSpace(sStr)); err == nil {
					allRequestedSeasons = append(allRequestedSeasons, sn)
				}
			}
			
			for rows.Next() {
				var hash string
				if err := rows.Scan(&hash); err == nil {
					// Get the actual torrent name from qBittorrent (more accurate than DB title)
					normalizedHash := strings.ToLower(hash)
					existingTorrent, err := s.qb.GetTorrentByHash(ctx, normalizedHash)
					if err != nil || existingTorrent == nil {
						continue
					}
					torrentTitle := strings.ToLower(existingTorrent.Name)
					
					// Check if this is a multi-season torrent that covers the current season
					multiSeasonRegex := regexp.MustCompile(`(?:season|s)\s*(\d+)\s*[-–]\s*(?:season|s)?\s*(\d+)`)
					multiSeasonMatch := multiSeasonRegex.FindStringSubmatch(torrentTitle)
					if len(multiSeasonMatch) == 3 {
						startSeason, err1 := strconv.Atoi(multiSeasonMatch[1])
						endSeason, err2 := strconv.Atoi(multiSeasonMatch[2])
						if err1 == nil && err2 == nil && startSeason <= endSeason {
							if currentSeasonNum >= startSeason && currentSeasonNum <= endSeason {
								// Check if ALL requested seasons are covered by this torrent
								allCovered := true
								for _, reqSeason := range allRequestedSeasons {
									if reqSeason < startSeason || reqSeason > endSeason {
										allCovered = false
										break
									}
								}
								if allCovered {
									slog.Info("All requested seasons already covered by multi-season torrent",
										"request_id", r.ID,
										"season", currentSeasonStr,
										"torrent_title", existingTorrent.Name,
										"range", fmt.Sprintf("%d-%d", startSeason, endSeason))
									return nil // All seasons covered, skip processing
								} else {
									// Some seasons are covered, but not all - allow processing to continue
									// The multi-season torrent we find might cover the missing seasons
									slog.Debug("Some seasons covered by existing torrent, but not all - will check for better multi-season torrent",
										"request_id", r.ID,
										"season", currentSeasonStr,
										"existing_torrent", existingTorrent.Name)
								}
							}
						}
					}
					
					// Also check for multiple season numbers in title (e.g., "S01S02S03" or "S01 S02 S03")
					allSeasonsRegex := regexp.MustCompile(`(?:s|season)\s*(\d+)`)
					allSeasonsMatches := allSeasonsRegex.FindAllStringSubmatch(torrentTitle, -1)
					if len(allSeasonsMatches) > 1 {
						foundSeasons := make(map[int]bool)
						for _, match := range allSeasonsMatches {
							if seasonNum, err := strconv.Atoi(match[1]); err == nil {
								foundSeasons[seasonNum] = true
							}
						}
						// Check if current season is covered
						if foundSeasons[currentSeasonNum] {
							// Check if ALL requested seasons are covered
							allCovered := true
							for _, reqSeason := range allRequestedSeasons {
								if !foundSeasons[reqSeason] {
									allCovered = false
									break
								}
							}
							if allCovered {
								slog.Info("All requested seasons already covered by multi-season torrent",
									"request_id", r.ID,
									"season", currentSeasonStr,
									"torrent_title", existingTorrent.Name)
								return nil // All seasons covered, skip processing
							} else {
								// Some seasons covered, but not all - allow processing to continue
								slog.Debug("Some seasons covered by existing torrent, but not all - will check for better multi-season torrent",
									"request_id", r.ID,
									"season", currentSeasonStr,
									"existing_torrent", existingTorrent.Name)
							}
						}
					}
				}
			}
		}
	}

	// 1. Build search query with season info for shows, year for movies
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
					// Include year if available to distinguish between different versions (e.g., Matlock 1986 vs 2024)
					if r.Year > 0 {
						searchQuery = fmt.Sprintf("%s %d S%02d", r.Title, r.Year, num)
					} else {
						searchQuery = fmt.Sprintf("%s S%02d", r.Title, num)
					}
				} else {
					// Fallback to string format if conversion fails
					if r.Year > 0 {
						searchQuery = fmt.Sprintf("%s %d S%s", r.Title, r.Year, seasonNum)
					} else {
						searchQuery = fmt.Sprintf("%s S%s", r.Title, seasonNum)
					}
				}
			} else if r.Year > 0 {
				// No season specified but year available - include year
				searchQuery = fmt.Sprintf("%s %d", r.Title, r.Year)
			}
		} else if r.Year > 0 {
			// No seasons specified but year available - include year
			searchQuery = fmt.Sprintf("%s %d", r.Title, r.Year)
		}
	} else if r.MediaType == "movie" && r.Year > 0 {
		// For movies, include year in search query to improve matching
		// Format: "Movie Title 2003"
		searchQuery = fmt.Sprintf("%s %d", r.Title, r.Year)
	}

	// Get search variants (e.g., "In & Out" -> ["In & Out", "In and Out"])
	variants := ExpandSearchQuery(searchQuery)

	// Track seen results by info hash to avoid duplicates
	seenHashes := make(map[string]bool)
	allResults := make([]TorrentSearchResult, 0)

	// Search each variant and merge results using the search service directly
	for _, variant := range variants {
		seasonsParam := ""
		if r.MediaType == "show" && r.Seasons != "" {
			seasonsParam = r.Seasons
		}

		slog.Info("Searching indexers for request", "request_id", r.ID, "title", r.Title, "variant", variant, "type", searchType)

		searchResults, err := SearchTorrents(ctx, variant, searchType, seasonsParam)
		if err != nil {
			slog.Warn("Failed to search indexers for variant", "request_id", r.ID, "variant", variant, "error", err)
			// Continue with next variant if one fails
			continue
		}

		// Convert SearchResult to TorrentSearchResult
		for _, result := range searchResults {
			hash := strings.ToLower(result.InfoHash)
			if hash == "" {
				// If no info hash, try to extract from magnet link
				hash = extractInfoHashFromMagnet(result.MagnetLink)
				hash = strings.ToLower(hash)
			}

			// Use title as fallback key if no hash available
			key := hash
			if key == "" {
				key = strings.ToLower(result.Title)
			}

			if !seenHashes[key] {
				seenHashes[key] = true
				allResults = append(allResults, TorrentSearchResult{
					Title:      result.Title,
					Size:       result.Size,
					Seeds:      result.Seeds,
					Peers:      result.Peers,
					MagnetLink: result.MagnetLink,
					InfoHash:   result.InfoHash,
					Source:     result.Source,
					Resolution: result.Resolution,
					Quality:    result.Quality,
				})
			}
		}
	}

	results := allResults
	slog.Info("Indexer search completed", "request_id", r.ID, "results_count", len(results), "variants_searched", len(variants))

	if len(results) == 0 {
		slog.Info("No results found for request", "request_id", r.ID, "title", r.Title, "retry_count", r.RetryCount)
		s.incrementRetryCount(r.ID, r.RetryCount)
		return nil
	}

	// Log first few results for debugging
	for i, result := range results {
		if i < 3 {
			slog.Debug("Indexer result", "index", i, "title", result.Title, "seeds", result.Seeds, "quality", result.Quality, "resolution", result.Resolution, "has_info_hash", result.InfoHash != "", "has_magnet", result.MagnetLink != "")
		}
	}

	// 2. Choose best result - prioritize quality (1080p > 4K > 720p > other), match seasons, sort by seeds, filter by title/year for movies
	// Also prefer results with direct info hashes or magnet links to avoid URL extraction issues
	best := selectBestResult(results, r.MediaType, r.Seasons, r.Title, r.Year)
	if best == nil {
		slog.Warn("No suitable results found after filtering", "request_id", r.ID, "title", r.Title, "total_results", len(results), "seasons", r.Seasons, "retry_count", r.RetryCount)
		s.incrementRetryCount(r.ID, r.RetryCount)
		return nil
	}

	// For shows: Check if the selected torrent is a multi-season torrent that covers seasons we don't have
	// Even if some seasons are already downloaded, we should still download if it covers missing seasons
	if r.MediaType == "show" && r.Seasons != "" {
		bestTitleLower := strings.ToLower(best.Title)
		currentSeasonStr := strings.TrimSpace(strings.Split(r.Seasons, ",")[0])
		currentSeasonNum, _ := strconv.Atoi(currentSeasonStr)
		
		// Check if this is a multi-season torrent
		multiSeasonRegex := regexp.MustCompile(`(?:season|s)\s*(\d+)\s*[-–]\s*(?:season|s)?\s*(\d+)`)
		multiSeasonMatch := multiSeasonRegex.FindStringSubmatch(bestTitleLower)
		if len(multiSeasonMatch) == 3 {
			startSeason, err1 := strconv.Atoi(multiSeasonMatch[1])
			endSeason, err2 := strconv.Atoi(multiSeasonMatch[2])
			if err1 == nil && err2 == nil && startSeason <= endSeason {
				// Check if this torrent would cover the current season being processed
				if currentSeasonNum >= startSeason && currentSeasonNum <= endSeason {
					// Check if we already have this exact torrent (by hash)
					bestHash := best.InfoHash
					if bestHash == "" && best.MagnetLink != "" {
						bestHash = extractInfoHashFromMagnet(best.MagnetLink)
					}
					if bestHash != "" {
						normalizedHash := strings.ToLower(bestHash)
						existingTorrent, err := s.qb.GetTorrentByHash(ctx, normalizedHash)
						if err == nil && existingTorrent != nil {
							// Torrent already exists - check if it covers all needed seasons
							existingTitleLower := strings.ToLower(existingTorrent.Name)
							// If the existing torrent is the same multi-season torrent, skip
							if strings.Contains(existingTitleLower, fmt.Sprintf("s%02d", startSeason)) && 
							   strings.Contains(existingTitleLower, fmt.Sprintf("s%02d", endSeason)) {
								slog.Info("Multi-season torrent already exists, skipping download",
									"request_id", r.ID,
									"season", currentSeasonStr,
									"torrent_title", existingTorrent.Name,
									"range", fmt.Sprintf("%d-%d", startSeason, endSeason))
								return nil
							}
						}
					}
					// Multi-season torrent found and doesn't exist yet - proceed with download
					// This will cover multiple seasons even if we're only processing one
					slog.Info("Found multi-season torrent that will cover multiple seasons",
						"request_id", r.ID,
						"current_season", currentSeasonStr,
						"torrent_title", best.Title,
						"range", fmt.Sprintf("%d-%d", startSeason, endSeason))
				}
			}
		}
	}

	slog.Info("Selected best torrent result", "request_id", r.ID, "title", best.Title, "seeds", best.Seeds, "quality", best.Quality, "resolution", best.Resolution, "info_hash", best.InfoHash, "has_magnet", best.MagnetLink != "", "source", best.Source)

	// Extract or validate InfoHash with fallback to next best result if extraction fails
	infoHash := best.InfoHash
	if infoHash == "" {
		magnetPreview := best.MagnetLink
		if len(magnetPreview) > 100 {
			magnetPreview = magnetPreview[:100] + "..."
		}
		slog.Debug("InfoHash not in result, extracting from magnet link", "request_id", r.ID, "magnet_preview", magnetPreview)

		// Check if MagnetLink is actually a URL (not a magnet URI)
		magnetLink := best.MagnetLink
		if strings.HasPrefix(magnetLink, "http://") || strings.HasPrefix(magnetLink, "https://") {
			slog.Debug("MagnetLink is a URL, fetching magnet link from page", "request_id", r.ID, "url", magnetLink)
			// Fetch the page and extract the magnet link
			extractedMagnet, err := extractMagnetLinkFromURL(ctx, magnetLink)
			if err != nil {
				slog.Warn("Failed to extract magnet link from URL, will try fallback", "request_id", r.ID, "error", err, "source", best.Source)
				// Try fallback: find next best result with direct magnet/info hash
				fallbackBest := findFallbackResult(results, best, r.MediaType, r.Seasons, r.Title, r.Year)
				if fallbackBest != nil {
					slog.Info("Using fallback result with direct magnet/info hash", "request_id", r.ID, "fallback_title", fallbackBest.Title, "fallback_source", fallbackBest.Source)
					best = fallbackBest
					magnetLink = best.MagnetLink
					if best.InfoHash != "" {
						infoHash = best.InfoHash
					} else if strings.HasPrefix(magnetLink, "magnet:") {
						infoHash = extractInfoHashFromMagnet(magnetLink)
					}
				} else {
					slog.Error("Could not extract info hash from magnet link and no fallback available", "request_id", r.ID, "magnet_link", magnetLink)
					return fmt.Errorf("could not extract info hash from magnet link")
				}
			} else if extractedMagnet != "" {
				magnetLink = extractedMagnet
				// Update best.MagnetLink so it's available for qBittorrent later
				best.MagnetLink = extractedMagnet
				slog.Debug("Successfully extracted magnet link from URL", "request_id", r.ID)
				infoHash = extractInfoHashFromMagnet(magnetLink)
			} else {
				// Extraction returned empty - try fallback
				fallbackBest := findFallbackResult(results, best, r.MediaType, r.Seasons, r.Title, r.Year)
				if fallbackBest != nil {
					slog.Info("Using fallback result after empty extraction", "request_id", r.ID, "fallback_title", fallbackBest.Title, "fallback_source", fallbackBest.Source)
					best = fallbackBest
					magnetLink = best.MagnetLink
					if best.InfoHash != "" {
						infoHash = best.InfoHash
					} else if strings.HasPrefix(magnetLink, "magnet:") {
						infoHash = extractInfoHashFromMagnet(magnetLink)
					}
				} else {
					slog.Error("Could not extract info hash from magnet link and no fallback available", "request_id", r.ID, "magnet_link", magnetLink)
					return fmt.Errorf("could not extract info hash from magnet link")
				}
			}
		} else {
			// Direct magnet link - extract info hash
			infoHash = extractInfoHashFromMagnet(magnetLink)
		}

		if infoHash == "" {
			slog.Error("Could not extract info hash from magnet link", "request_id", r.ID, "magnet_link", magnetLink)
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

	// Update request status to downloading and reset retry count (we found a torrent!)
	_, err = tx.Exec("UPDATE requests SET status = 'downloading', retry_count = 0, last_search_at = NULL, updated_at = NOW() WHERE id = $1", r.ID)
	if err != nil {
		return fmt.Errorf("failed to update request status: %w", err)
	}

	// Add to downloads table
	// Check if this hash is already used by another request (hash collision) - check within transaction
	var existingRequestID int
	err = tx.QueryRow(`
		SELECT request_id FROM downloads WHERE LOWER(torrent_hash) = $1 LIMIT 1`,
		infoHash).Scan(&existingRequestID)
	if err == nil && existingRequestID > 0 && existingRequestID != r.ID {
		// Hash collision - this torrent hash is already associated with a different request
		// Get details of the conflicting request for logging
		var conflictTitle string
		var conflictYear int
		var conflictTVDBID string
		database.DB.QueryRow("SELECT title, year, tvdb_id FROM requests WHERE id = $1", existingRequestID).Scan(&conflictTitle, &conflictYear, &conflictTVDBID)
		
		slog.Warn("Torrent hash collision detected - same torrent hash for different requests",
			"current_request_id", r.ID,
			"current_title", r.Title,
			"current_year", r.Year,
			"current_tvdb_id", r.TVDBID,
			"conflict_request_id", existingRequestID,
			"conflict_title", conflictTitle,
			"conflict_year", conflictYear,
			"conflict_tvdb_id", conflictTVDBID,
			"torrent_hash", infoHash,
			"torrent_title", best.Title)
		
		// Rollback transaction - don't mark this request as downloading
		tx.Rollback()
		// Reset request to pending so it can retry with better matching
		database.DB.Exec("UPDATE requests SET status = 'pending', updated_at = NOW() WHERE id = $1", r.ID)
		return fmt.Errorf("torrent hash collision: hash %s already used by request %d (%s %d)", infoHash, existingRequestID, conflictTitle, conflictYear)
	}
	
	result, err := tx.Exec(`
		INSERT INTO downloads (request_id, torrent_hash, title, status, updated_at)
		VALUES ($1, $2, $3, 'downloading', NOW())
		ON CONFLICT (torrent_hash) DO NOTHING`,
		r.ID, infoHash, best.Title)
	if err != nil {
		return fmt.Errorf("failed to insert download record: %w", err)
	}
	
	// Check if insert actually succeeded (not skipped due to conflict)
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		// Insert was skipped - check if it's because hash already exists for this request
		var checkRequestID int
		err = tx.QueryRow(`
			SELECT request_id FROM downloads WHERE LOWER(torrent_hash) = $1 LIMIT 1`,
			infoHash).Scan(&checkRequestID)
		if err == nil && checkRequestID == r.ID {
			// Hash already exists for this request - that's okay, continue
			slog.Debug("Download record already exists for this request and hash", "request_id", r.ID, "hash", infoHash)
		} else {
			// Hash exists for different request - this is a problem
			tx.Rollback()
			database.DB.Exec("UPDATE requests SET status = 'pending', updated_at = NOW() WHERE id = $1", r.ID)
			return fmt.Errorf("torrent hash %s already exists for a different request", infoHash)
		}
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

	slog.Info("Adding torrent to qBittorrent",
		"request_id", r.ID,
		"title", r.Title,
		"year", r.Year,
		"tvdb_id", r.TVDBID,
		"torrent_title", best.Title,
		"info_hash", infoHash,
		"category", category,
		"save_path", savePath)

	// Try to fetch .torrent file first (avoids metadata download issues)
	var addErr error
	torrentFileData := fetchTorrentFile(ctx, infoHash)
	if torrentFileData != nil {
		slog.Info("Successfully fetched .torrent file, adding via file upload",
			"request_id", r.ID,
			"info_hash", infoHash,
			"file_size", len(torrentFileData))
		addErr = s.qb.AddTorrentFile(ctx, torrentFileData, category, savePath)
		if addErr == nil {
			slog.Info("Successfully added torrent via .torrent file", "request_id", r.ID)
		} else {
			slog.Warn("Failed to add torrent via .torrent file, will try magnet link",
				"request_id", r.ID,
				"error", addErr)
		}
	} else {
		slog.Debug("Could not fetch .torrent file, will use magnet link", "request_id", r.ID)
	}

	// If .torrent file method failed or wasn't available, fall back to magnet link
	if addErr != nil || torrentFileData == nil {
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
		addErr = s.qb.AddTorrent(ctx, magnetLink, category, savePath)
	}

	if addErr != nil {
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
		// Reset request back to pending so it can be retried
		slog.Error("Failed to add torrent to qBittorrent, resetting request",
			"request_id", r.ID,
			"title", r.Title,
			"year", r.Year,
			"tvdb_id", r.TVDBID,
			"torrent_title", best.Title,
			"info_hash", normalizedHash,
			"error", addErr)
		database.DB.Exec("UPDATE requests SET status = 'pending', updated_at = NOW() WHERE id = $1", r.ID)
		database.DB.Exec("DELETE FROM downloads WHERE LOWER(torrent_hash) = $1", normalizedHash)
		return fmt.Errorf("failed to add torrent to qBittorrent: %w", addErr)
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
			var requestID int
			err = database.DB.QueryRow(`
				SELECT request_id FROM downloads WHERE LOWER(torrent_hash) = $1 LIMIT 1`,
				normalizedHash).Scan(&requestID)
			if err == nil && requestID > 0 {
				// Check if request was already completed to avoid duplicate processing
				var currentStatus string
				err = database.DB.QueryRow("SELECT status FROM requests WHERE id = $1", requestID).Scan(&currentStatus)
				if err == nil {
					if currentStatus != "completed" {
						_, err = database.DB.Exec(`
							UPDATE requests
							SET status = 'completed', updated_at = NOW()
							WHERE id = $1`,
							requestID)
						if err != nil {
							slog.Error("Error updating request status to completed", "error", err, "torrent_hash", normalizedHash, "request_id", requestID)
						}
					}
				}
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
					slog.Warn("Download vanished from qBittorrent for over 15 minutes, resetting request to pending",
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
					database.DB.Exec("UPDATE requests SET status = 'pending' WHERE id = $1", reqID)
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

// selectBestResult selects the best torrent result based on seeds, quality, season matching, title/year matching, and minimum requirements
func selectBestResult(results []TorrentSearchResult, mediaType string, requestedSeasons string, requestedTitle string, requestedYear int) *TorrentSearchResult {
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

	// Filter by minimum seeds and title/year matching (for movies)
	var filtered []TorrentSearchResult
	zeroSeedCount := 0
	titleMismatchCount := 0
	requestedTitleLower := strings.ToLower(requestedTitle)

	for _, r := range results {
		// Filter by seeds
		if r.Seeds == 0 {
			zeroSeedCount++
			continue
		}

		// For movies, filter out results that don't match the requested title
		if mediaType == "movie" && requestedTitle != "" {
			resultTitleLower := strings.ToLower(r.Title)
			titleMatches := false

			// Strategy 1: Check if result title starts with requested title
			// This handles "Dave (2003)" matching "Dave"
			if strings.HasPrefix(resultTitleLower, requestedTitleLower) {
				// Check what comes after - should be year, quality, or nothing
				remaining := resultTitleLower[len(requestedTitleLower):]
				remaining = strings.TrimSpace(remaining)
				// Allow: empty, year (2003), parentheses with year ((2003)), quality tags
				// But NOT additional words like "Chappelle"
				if remaining == "" {
					titleMatches = true
				} else if strings.HasPrefix(remaining, "(") ||
					strings.HasPrefix(remaining, "[") ||
					strings.HasPrefix(remaining, "19") ||
					strings.HasPrefix(remaining, "20") {
					// Check if it's just a year/quality, not another word
					// Years are 4 digits, quality tags are usually short
					remainingClean := strings.Trim(remaining, "()[]")
					if len(remainingClean) <= 6 || // Short enough for year/quality
						strings.HasPrefix(remainingClean, "1080") ||
						strings.HasPrefix(remainingClean, "720") ||
						strings.HasPrefix(remainingClean, "480") ||
						strings.HasPrefix(remainingClean, "bluray") ||
						strings.HasPrefix(remainingClean, "dvd") {
						titleMatches = true
					}
				}
			}

			// Strategy 2: Check for exact match (case-insensitive)
			if !titleMatches && resultTitleLower == requestedTitleLower {
				titleMatches = true
			}

			// Strategy 3: Check if requested title appears as a complete word AND
			// there are no other significant words (to avoid "Dave Chappelle" matching "Dave")
			if !titleMatches {
				words := strings.Fields(resultTitleLower)
				requestedWords := strings.Fields(requestedTitleLower)
				requestedWordSet := make(map[string]bool)
				for _, w := range requestedWords {
					requestedWordSet[strings.Trim(w, ".,!?()[]{}")] = true
				}

				// Check if all significant words in result are in requested title
				allWordsMatch := true
				foundRequestedTitle := false
				for _, word := range words {
					wordClean := strings.Trim(word, ".,!?()[]{}")
					// Skip very short words, years, quality tags
					if len(wordClean) <= 2 ||
						strings.HasPrefix(wordClean, "1080") ||
						strings.HasPrefix(wordClean, "720") ||
						strings.HasPrefix(wordClean, "480") ||
						(len(wordClean) == 4 && strings.HasPrefix(wordClean, "19")) ||
						(len(wordClean) == 4 && strings.HasPrefix(wordClean, "20")) {
						continue
					}

					if requestedWordSet[wordClean] {
						foundRequestedTitle = true
					} else {
						// Found a significant word not in requested title
						allWordsMatch = false
						break
					}
				}

				if foundRequestedTitle && allWordsMatch {
					titleMatches = true
				}
			}

			if !titleMatches {
				titleMismatchCount++
				slog.Debug("Filtered out result due to title mismatch",
					"requested", requestedTitle,
					"result", r.Title)
				continue
			}
		}

		filtered = append(filtered, r)
	}

	if len(filtered) == 0 {
		if zeroSeedCount > 0 || titleMismatchCount > 0 {
			slog.Warn("All results filtered out",
				"total_results", len(results),
				"zero_seed_count", zeroSeedCount,
				"title_mismatch_count", titleMismatchCount)
		}
		// If all results have 0 seeds, still try to use the first one (might be a new torrent)
		if len(results) > 0 {
			slog.Info("Using first result despite filters", "title", results[0].Title, "seeds", results[0].Seeds)
			return &results[0]
		}
		return nil
	}

	slog.Debug("Filtered results",
		"total_results", len(results),
		"filtered_count", len(filtered),
		"zero_seed_count", zeroSeedCount,
		"title_mismatch_count", titleMismatchCount)

	// Score function: higher is better
	scoreResult := func(r *TorrentSearchResult) int {
		score := 0
		titleLower := strings.ToLower(r.Title)
		resolutionLower := strings.ToLower(r.Resolution)

		// Quality priority: 1080p > 4K > 720p > anything else
		// Quality scores are set high enough to ensure quality is always the primary factor
		// Maximum possible other bonuses: 2000 (exact title) + 500 (year) + 500 (season) + 300 (info hash) = 3300
		// So quality differences must be > 3300 to guarantee priority order
		// Check in order of preference (most specific first to avoid false matches)
		if strings.Contains(titleLower, "1080") || strings.Contains(resolutionLower, "1080") {
			score += 10000 // Highest priority: 1080p
		} else if strings.Contains(titleLower, "2160") || strings.Contains(titleLower, "4k") || strings.Contains(titleLower, "uhd") ||
			strings.Contains(resolutionLower, "2160") || strings.Contains(resolutionLower, "4k") || strings.Contains(resolutionLower, "uhd") {
			score += 5500 // Second priority: 4K (2160p, 4k, UHD) - must be > 720p (2000) + max bonuses (3300)
		} else if strings.Contains(titleLower, "720") || strings.Contains(resolutionLower, "720") {
			score += 2000 // Third priority: 720p
		} else if strings.Contains(titleLower, "480") || strings.Contains(resolutionLower, "480") {
			score += 500 // Lower priority: 480p
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

		// Title and year matching bonus (for movies and shows)
		if requestedTitle != "" {
			requestedTitleLower := strings.ToLower(requestedTitle)
			resultTitleLower := strings.ToLower(r.Title)

			// Exact title match gets highest bonus
			if resultTitleLower == requestedTitleLower {
				score += 2000
			} else if strings.Contains(resultTitleLower, requestedTitleLower) {
				// Title contains requested title
				score += 1000
			}

			// Year matching bonus (for both movies and shows)
			// This helps distinguish between different versions (e.g., Matlock 1986 vs Matlock 2024)
			if requestedYear > 0 {
				yearStr := fmt.Sprintf("%d", requestedYear)
				if strings.Contains(resultTitleLower, yearStr) {
					score += 500 // Big bonus for year match
				} else {
					// Penalize results with different years to avoid wrong matches
					// Check for other 4-digit years in the title
					yearPattern := regexp.MustCompile(`\b(19|20)\d{2}\b`)
					yearsInTitle := yearPattern.FindAllString(resultTitleLower, -1)
					for _, y := range yearsInTitle {
						if y != yearStr {
							score -= 300 // Penalty for wrong year
							slog.Debug("Penalizing torrent with wrong year",
								"requested_year", requestedYear,
								"found_year", y,
								"torrent_title", r.Title)
						}
					}
				}
			}
		}

		// Prefer results with direct info hash or magnet link (avoid URL extraction)
		// This helps avoid issues with sites like 1337x that require page scraping
		if r.InfoHash != "" {
			score += 300 // Bonus for having info hash directly
		} else if r.MagnetLink != "" && strings.HasPrefix(r.MagnetLink, "magnet:") {
			score += 200 // Bonus for direct magnet link (not a URL)
		}

		// Seeds contribute to score (but less than quality/season/title match)
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

// findFallbackResult finds the next best result that has a direct info hash or magnet link
// This is used when the best result requires URL extraction which fails
func findFallbackResult(results []TorrentSearchResult, exclude *TorrentSearchResult, mediaType string, requestedSeasons string, requestedTitle string, requestedYear int) *TorrentSearchResult {
	// Filter to only results with direct info hash or magnet link (not URLs)
	var candidates []TorrentSearchResult
	for _, r := range results {
		// Skip the excluded result
		if exclude != nil && r.Title == exclude.Title && r.Source == exclude.Source {
			continue
		}
		// Only consider results with direct info hash or magnet link
		if r.InfoHash != "" || (r.MagnetLink != "" && strings.HasPrefix(r.MagnetLink, "magnet:")) {
			candidates = append(candidates, r)
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	// Use selectBestResult logic but only on candidates
	return selectBestResult(candidates, mediaType, requestedSeasons, requestedTitle, requestedYear)
}

// extractMagnetLinkFromURL fetches a torrent page URL and extracts the magnet link from the HTML
func extractMagnetLinkFromURL(ctx context.Context, targetURL string) (string, error) {
	var htmlContent string
	var err error

	// Check if this is a 1337x URL - these require bypass service and cannot use direct requests
	is1337x := strings.Contains(targetURL, "1337x.to")

	// Try using Cloudflare bypass service if available (for sites like 1337x.to)
	bypassURL := sharedconfig.GetEnv("CLOUDFLARE_BYPASS_URL", "")
	if bypassURL != "" {
		slog.Debug("Fetching URL via bypass service", "url", targetURL, "bypass_url", bypassURL)
		htmlContent, err = fetchViaBypass(ctx, bypassURL, targetURL)
		if err != nil {
			if is1337x {
				// For 1337x, bypass service is required - don't fall back to direct request
				slog.Error("Failed to fetch 1337x URL via bypass service (required)", "url", targetURL, "error", err)
				return "", fmt.Errorf("failed to fetch 1337x URL via bypass service (required): %w", err)
			}
			slog.Debug("Failed to fetch via bypass service, trying direct request", "url", targetURL, "error", err)
			// Fall through to direct request for non-1337x sites
		} else {
			slog.Debug("Successfully fetched URL via bypass service", "url", targetURL)
		}
	} else if is1337x {
		// 1337x requires bypass service - cannot proceed without it
		slog.Error("CLOUDFLARE_BYPASS_URL not configured but required for 1337x URLs", "url", targetURL)
		return "", fmt.Errorf("CLOUDFLARE_BYPASS_URL not configured but required for 1337x URLs")
	}

	// If bypass failed or not available, try direct request (only for non-1337x sites)
	if htmlContent == "" && !is1337x {
		resp, err := sharedhttp.MakeRequest(ctx, targetURL, sharedhttp.DefaultClient)
		if err != nil {
			return "", fmt.Errorf("failed to fetch URL: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
		}

		// Read the HTML content
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("failed to read response body: %w", err)
		}

		htmlContent = string(body)
	}

	// If we still don't have content, return error
	if htmlContent == "" {
		return "", fmt.Errorf("failed to fetch HTML content from URL")
	}

	// Log HTML snippet for debugging (first 500 chars)
	htmlPreview := htmlContent
	if len(htmlPreview) > 500 {
		htmlPreview = htmlPreview[:500] + "..."
	}
	slog.Debug("Extracting magnet link from HTML", "url", targetURL, "html_preview", htmlPreview, "html_length", len(htmlContent))

	// Try to find magnet link using regex first (faster)
	// Match magnet:? followed by URL-encoded or plain characters until quote, space, or HTML tag
	// This handles both plain HTML and HTML entities
	magnetRegex := regexp.MustCompile(`magnet:\?[^"'\s<>]+`)
	matches := magnetRegex.FindString(htmlContent)
	if matches != "" {
		// Clean up any HTML entities that might have been included
		matches = strings.ReplaceAll(matches, "&amp;", "&")
		matches = strings.ReplaceAll(matches, "&quot;", "\"")
		matches = strings.ReplaceAll(matches, "&#39;", "'")
		return matches, nil
	}

	// Also check for info hash patterns that we can construct into magnet links
	// Look for 40-character hex strings (info hash) in various contexts
	// Try multiple patterns: standalone hash, hash in magnet link format, hash in data attributes
	infoHashPatterns := []*regexp.Regexp{
		regexp.MustCompile(`[0-9a-fA-F]{40}`),                                    // Standalone 40-char hex
		regexp.MustCompile(`btih:([0-9a-fA-F]{40})`),                            // In magnet link format
		regexp.MustCompile(`info hash[:\s]+([0-9a-fA-F]{40})`),                  // After "info hash:"
		regexp.MustCompile(`hash[:\s]+([0-9a-fA-F]{40})`),                       // After "hash:"
		regexp.MustCompile(`data-hash=["']([0-9a-fA-F]{40})["']`),               // In data-hash attribute
		regexp.MustCompile(`data-info-hash=["']([0-9a-fA-F]{40})["']`),          // In data-info-hash attribute
		regexp.MustCompile(`"([0-9a-fA-F]{40})"`),                               // Quoted hash
		regexp.MustCompile(`'([0-9a-fA-F]{40})'`),                               // Single-quoted hash
	}
	
	var infoHash string
	for _, pattern := range infoHashPatterns {
		matches := pattern.FindStringSubmatch(htmlContent)
		if len(matches) > 0 {
			// Use the captured group if available, otherwise the full match
			if len(matches) > 1 {
				infoHash = strings.ToLower(matches[1])
			} else {
				infoHash = strings.ToLower(matches[0])
			}
			// Validate it's actually 40 characters
			if len(infoHash) == 40 {
				break
			}
			infoHash = "" // Reset if invalid length
		}
	}
	
	if infoHash != "" {
		// Extract title from page if possible for better magnet link
		titleRegex := regexp.MustCompile(`<title[^>]*>([^<]+)</title>`)
		titleMatch := titleRegex.FindStringSubmatch(htmlContent)
		title := ""
		if len(titleMatch) > 1 {
			title = strings.TrimSpace(titleMatch[1])
			// Clean up title - remove site name and extra info
			title = strings.Split(title, " - ")[0]
			title = strings.Split(title, " | ")[0]
		}
		
		magnetLink := fmt.Sprintf("magnet:?xt=urn:btih:%s", infoHash)
		if title != "" {
			// URL encode the title
			magnetLink += "&dn=" + strings.ReplaceAll(url.QueryEscape(title), "+", "%20")
		}
		slog.Debug("Constructed magnet link from info hash", "info_hash", infoHash, "title", title)
		return magnetLink, nil
	}

	// Fallback: parse HTML to find magnet links in href attributes
	// HTML parser automatically decodes entities, so this is more reliable
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return "", fmt.Errorf("failed to parse HTML: %w", err)
	}

	var magnetLink string
	var findMagnetLink func(*html.Node)
	findMagnetLink = func(n *html.Node) {
		if n.Type == html.ElementNode {
			// Check <a> tags for href
			if n.Data == "a" {
				for _, attr := range n.Attr {
					if attr.Key == "href" && strings.HasPrefix(attr.Val, "magnet:") {
						magnetLink = attr.Val
						return
					}
				}
			}
			
			// Check button elements and other elements for data attributes
			for _, attr := range n.Attr {
				// Check data-magnet, data-url, data-href, etc.
				if (attr.Key == "data-magnet" || attr.Key == "data-url" || attr.Key == "data-href" || 
					attr.Key == "data-link" || attr.Key == "href") && strings.HasPrefix(attr.Val, "magnet:") {
					magnetLink = attr.Val
					return
				}
				
				// Check onclick handlers for magnet links
				if attr.Key == "onclick" && strings.Contains(attr.Val, "magnet:") {
					// Extract magnet link from onclick handler
					magnetMatch := magnetRegex.FindString(attr.Val)
					if magnetMatch != "" {
						magnetLink = magnetMatch
						return
					}
				}
			}
		}
		
		// Also check text nodes for magnet links (in case they're in script tags or comments)
		if n.Type == html.TextNode && strings.Contains(n.Data, "magnet:") {
			magnetMatch := magnetRegex.FindString(n.Data)
			if magnetMatch != "" {
				magnetLink = magnetMatch
				return
			}
		}
		
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findMagnetLink(c)
		}
	}
	findMagnetLink(doc)

	if magnetLink == "" {
		// Debug: Check for any hash-like patterns in the HTML
		hashPatterns := regexp.MustCompile(`[0-9a-fA-F]{32,40}`)
		allHashes := hashPatterns.FindAllString(htmlContent, -1)
		if len(allHashes) > 0 {
			sampleCount := 3
			if len(allHashes) < sampleCount {
				sampleCount = len(allHashes)
			}
			slog.Debug("Found hash-like patterns in HTML but couldn't extract magnet link", "url", targetURL, "hash_count", len(allHashes), "sample_hashes", allHashes[:sampleCount])
		}
		return "", fmt.Errorf("no magnet link found in page")
	}

	return magnetLink, nil
}

// fetchViaBypass uses the Cloudflare bypass service (Flaresolverr-compatible) to fetch a URL
func fetchViaBypass(ctx context.Context, bypassURL, targetURL string) (string, error) {
	// Ensure no trailing slash
	bypassURL = strings.TrimSuffix(bypassURL, "/")

	// Flaresolverr-compatible API format
	// POST to /v1 with JSON body
	requestBody := map[string]interface{}{
		"cmd":        "request.get",
		"url":        targetURL,
		"maxTimeout": 60000,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make POST request to bypass service
	req, err := http.NewRequestWithContext(ctx, "POST", bypassURL+"/v1", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Use a longer timeout client for Flaresolverr requests (can take up to 60s + buffer)
	// Flaresolverr maxTimeout is 60000ms, so we need at least 90s to account for network overhead
	client := &http.Client{
		Timeout: 90 * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call bypass service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("bypass service returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse Flaresolverr response
	var bypassResp struct {
		Status   string `json:"status"`
		Solution struct {
			URL      string        `json:"url"`
			Response string        `json:"response"`
			Cookies  []interface{} `json:"cookies"`
		} `json:"solution"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&bypassResp); err != nil {
		return "", fmt.Errorf("failed to decode bypass response: %w", err)
	}

	if bypassResp.Status != "ok" || bypassResp.Solution.Response == "" {
		return "", fmt.Errorf("bypass service returned invalid response")
	}

	return bypassResp.Solution.Response, nil
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

// fetchTorrentFile attempts to download a .torrent file from trackers using the info hash
// Returns the .torrent file data if successful, nil otherwise
func fetchTorrentFile(ctx context.Context, infoHash string) []byte {
	if infoHash == "" || len(infoHash) != 40 {
		return nil
	}

	// Common tracker patterns for downloading .torrent files
	// Many trackers don't actually serve .torrent files directly, but we try common patterns
	trackerPatterns := []struct {
		baseURL string
		pattern string
	}{
		// Pattern: http://tracker/torrent/{hash}.torrent
		{"http://tracker.opentrackr.org:1337", "/torrent/%s.torrent"},
		{"http://tracker.openbittorrent.com:80", "/torrent/%s.torrent"},
		// Pattern: http://tracker/download.php?info_hash={hash}
		{"http://tracker.opentrackr.org:1337", "/download.php?info_hash=%s"},
		{"http://tracker.openbittorrent.com:80", "/download.php?info_hash=%s"},
		// Pattern: http://tracker/get/{hash}
		{"http://tracker.opentrackr.org:1337", "/get/%s"},
		{"http://tracker.openbittorrent.com:80", "/get/%s"},
		// Pattern: http://tracker/scrape?info_hash={hash} (some trackers serve torrents via scrape)
		{"http://tracker.opentrackr.org:1337", "/scrape?info_hash=%s"},
	}

	// Try public APIs that can convert info hash to .torrent file
	// These services scrape trackers and can provide .torrent files
	publicAPIs := []string{
		fmt.Sprintf("https://itorrents.org/torrent/%s.torrent", strings.ToUpper(infoHash)),
		fmt.Sprintf("https://itorrents.org/torrent/%s.torrent", strings.ToLower(infoHash)),
		fmt.Sprintf("https://api.bitport.io/api/v1/torrents/%s/download", strings.ToLower(infoHash)),
	}

	// Try public APIs first (they're more reliable)
	for _, apiURL := range publicAPIs {
		resp, err := sharedhttp.MakeRequest(ctx, apiURL, sharedhttp.DefaultClient)
		if err == nil {
			defer resp.Body.Close()

			// Check HTTP status code first
			if resp.StatusCode != http.StatusOK {
				slog.Debug("Torrent API returned non-200 status", "url", apiURL, "status", resp.StatusCode)
				continue
			}

			body, err := io.ReadAll(resp.Body)
			if err != nil || len(body) == 0 {
				continue
			}

			// Check if it's HTML (error page) - HTML typically starts with <!DOCTYPE, <html, or <HTML
			previewLen := 100
			if len(body) < previewLen {
				previewLen = len(body)
			}
			bodyPreview := string(body[:previewLen])
			if strings.HasPrefix(strings.ToLower(bodyPreview), "<!doctype") ||
				strings.HasPrefix(strings.ToLower(bodyPreview), "<html") ||
				strings.Contains(strings.ToLower(bodyPreview), "<html") {
				previewLen2 := 200
				if len(bodyPreview) < previewLen2 {
					previewLen2 = len(bodyPreview)
				}
				slog.Debug("Torrent API returned HTML instead of torrent file", "url", apiURL, "preview", bodyPreview[:previewLen2])
				continue
			}

			// Validate it's a torrent file (bencoded, starts with 'd' and contains "info" key)
			if len(body) > 10 && body[0] == 'd' {
				// Better validation: check for "info" key which all torrent files must have
				bodyStr := string(body)
				if strings.Contains(bodyStr, "4:info") || strings.Contains(bodyStr, "info") {
					// Additional check: verify it's actually bencoded (not just text containing "info")
					// Bencoded dictionaries start with 'd' and end with 'e'
					if strings.HasSuffix(bodyStr, "e") {
						slog.Info("Successfully fetched .torrent file from public API",
							"url", apiURL,
							"size", len(body))
						return body
					}
				}
			}
			
			// Log debug info if validation failed
			debugLen := 50
			if len(body) < debugLen {
				debugLen = len(body)
			}
			slog.Debug("Downloaded data doesn't appear to be a valid torrent file",
				"url", apiURL,
				"size", len(body),
				"first_bytes", string(body[:debugLen]))
		}
	}

	// Try tracker patterns (less reliable, but worth trying)
	infoHashUpper := strings.ToUpper(infoHash)
	infoHashLower := strings.ToLower(infoHash)

	for _, pattern := range trackerPatterns {
		// Try uppercase hash
		torrentURL := fmt.Sprintf(pattern.baseURL+pattern.pattern, infoHashUpper)
		resp, err := sharedhttp.MakeRequest(ctx, torrentURL, sharedhttp.DefaultClient)
		if err == nil {
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				continue
			}

			body, err := io.ReadAll(resp.Body)
			if err == nil && len(body) > 0 {
				// Check for HTML error pages
				previewLen := 100
				if len(body) < previewLen {
					previewLen = len(body)
				}
				bodyStr := string(body[:previewLen])
				if strings.HasPrefix(strings.ToLower(bodyStr), "<!doctype") ||
					strings.HasPrefix(strings.ToLower(bodyStr), "<html") {
					continue
				}

				// Torrent files are bencoded and typically start with 'd' (dictionary)
				// Must contain "info" key and end with 'e'
				if len(body) > 10 && body[0] == 'd' {
					bodyStr = string(body)
					if strings.Contains(bodyStr, "4:info") || strings.Contains(bodyStr, "info") {
						if strings.HasSuffix(bodyStr, "e") {
							slog.Info("Successfully fetched .torrent file from tracker",
								"url", torrentURL,
								"size", len(body))
							return body
						}
					}
				}
			}
		}

		// Try lowercase hash
		if infoHashLower != infoHashUpper {
			torrentURL = fmt.Sprintf(pattern.baseURL+pattern.pattern, infoHashLower)
			resp, err = sharedhttp.MakeRequest(ctx, torrentURL, sharedhttp.DefaultClient)
			if err == nil {
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					continue
				}

				body, err := io.ReadAll(resp.Body)
				if err == nil && len(body) > 0 {
					// Check for HTML error pages
					previewLen := 100
					if len(body) < previewLen {
						previewLen = len(body)
					}
					bodyPreview := string(body[:previewLen])
					if strings.HasPrefix(strings.ToLower(bodyPreview), "<!doctype") ||
						strings.HasPrefix(strings.ToLower(bodyPreview), "<html") {
						continue
					}

					if len(body) > 10 && body[0] == 'd' {
						bodyStr := string(body)
						if strings.Contains(bodyStr, "4:info") || strings.Contains(bodyStr, "info") {
							if strings.HasSuffix(bodyStr, "e") {
								slog.Info("Successfully fetched .torrent file from tracker",
									"url", torrentURL,
									"size", len(body))
								return body
							}
						}
					}
				}
			}
		}
	}

	return nil
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

// incrementRetryCount increments the retry count and updates last_search_at for a request
// If retries are exhausted (54 total: 24 hourly + 30 daily), sets status to 'not_found'
func (s *AutomationService) incrementRetryCount(requestID int, currentRetryCount int) {
	newRetryCount := currentRetryCount + 1

	// After 54 retries (24 hourly + 30 daily), mark as not found
	if newRetryCount >= 54 {
		_, err := database.DB.Exec(`
			UPDATE requests 
			SET status = 'not_found', retry_count = $1, last_search_at = NOW(), updated_at = NOW() 
			WHERE id = $2`,
			newRetryCount, requestID)
		if err != nil {
			slog.Error("Failed to mark request as not_found", "request_id", requestID, "retry_count", newRetryCount, "error", err)
		} else {
			slog.Info("Request marked as not_found after exhausting retries", "request_id", requestID, "retry_count", newRetryCount)
		}
	} else {
		// Increment retry count and update last_search_at
		_, err := database.DB.Exec(`
			UPDATE requests 
			SET retry_count = $1, last_search_at = NOW(), updated_at = NOW() 
			WHERE id = $2`,
			newRetryCount, requestID)
		if err != nil {
			slog.Error("Failed to increment retry count", "request_id", requestID, "retry_count", newRetryCount, "error", err)
		} else {
			slog.Debug("Incremented retry count", "request_id", requestID, "retry_count", newRetryCount)
		}
	}
}
