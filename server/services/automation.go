package services

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
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

func NewAutomationService(cfg *config.Config, qb *QBittorrentClient) *AutomationService {
	return &AutomationService{
		cfg: cfg,
		qb:  qb,
	}
}

func (s *AutomationService) Start(ctx context.Context) {
	log.Println("Starting Automation Service...")

	// Check for approved requests every 5 minutes
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	// Update download progress every 30 seconds
	updateTicker := time.NewTicker(30 * time.Second)
	defer updateTicker.Stop()

	// Check subtitle queue every 15 minutes
	subtitleTicker := time.NewTicker(15 * time.Minute)
	defer subtitleTicker.Stop()

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

func (s *AutomationService) ProcessApprovedRequests(ctx context.Context) {
	var requests []models.Request
	query := `SELECT id, title, media_type, year, tmdb_id, tvdb_id, imdb_id, seasons FROM requests WHERE status = 'approved'`

	rows, err := database.DB.Query(query)
	if err != nil {
		log.Printf("Error querying approved requests: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var r models.Request
		var tmdbID, tvdbID, imdbID, seasons sql.NullString
		if err := rows.Scan(&r.ID, &r.Title, &r.MediaType, &r.Year, &tmdbID, &tvdbID, &imdbID, &seasons); err != nil {
			log.Printf("Error scanning request: %v", err)
			continue
		}
		r.TMDBID = tmdbID.String
		r.TVDBID = tvdbID.String
		r.IMDBID = imdbID.String
		r.Seasons = seasons.String
		requests = append(requests, r)
	}

	for _, r := range requests {
		log.Printf("Processing approved request: %s (%d)", r.Title, r.ID)
		if err := s.processRequest(ctx, r); err != nil {
			log.Printf("Failed to process request %d: %v", r.ID, err)
		}
	}
}

func (s *AutomationService) processRequest(ctx context.Context, r models.Request) error {
	// 1. Search Indexer
	searchType := r.MediaType
	if r.MediaType == "show" {
		searchType = "show" // Updated to use "show" instead of "solid"
	}

	searchURL := sharedhttp.BuildQueryURL(s.cfg.IndexerURL+"/search", map[string]string{
		"q":      r.Title,
		"type":   searchType,
		"format": "json",
	})

	resp, err := sharedhttp.MakeRequest(ctx, searchURL, sharedhttp.LongTimeoutClient)
	if err != nil {
		return fmt.Errorf("failed to call indexer: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := sharedhttp.ReadResponseBody(resp)
		return fmt.Errorf("indexer returned status %d: %s", resp.StatusCode, string(body))
	}

	type SearchResult struct {
		Title      string `json:"title"`
		MagnetLink string `json:"magnet_link"`
		InfoHash   string `json:"info_hash"`
		Seeds      int    `json:"seeds"`
		Size       string `json:"size"`
	}

	var results []SearchResult
	if err := sharedhttp.DecodeJSONResponse(resp, &results); err != nil {
		return fmt.Errorf("failed to decode indexer response: %w", err)
	}

	if len(results) == 0 {
		log.Printf("No results found for %s", r.Title)
		return nil
	}

	// 2. Choose best result (simplest: first one with most seeds)
	// The indexer already sorts by seeds for YTS
	best := results[0]

	// 3. Add to qBittorrent
	category := "arrgo-movies"
	savePath := s.cfg.IncomingMoviesPath
	if r.MediaType == "show" {
		category = "arrgo-shows"
		savePath = s.cfg.IncomingShowsPath
	}

	if err := s.qb.AddTorrent(ctx, best.MagnetLink, category, savePath); err != nil {
		return fmt.Errorf("failed to add torrent to qBittorrent: %w", err)
	}

	// 4. Update Database
	tx, err := database.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Update request status
	_, err = tx.Exec("UPDATE requests SET status = 'downloading', updated_at = NOW() WHERE id = $1", r.ID)
	if err != nil {
		return err
	}

	// Add to downloads table
	_, err = tx.Exec(`
		INSERT INTO downloads (request_id, torrent_hash, title, status, updated_at)
		VALUES ($1, $2, $3, 'downloading', NOW())
		ON CONFLICT (torrent_hash) DO NOTHING`,
		r.ID, best.InfoHash, best.Title)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (s *AutomationService) UpdateDownloadStatus(ctx context.Context) {
	torrents, err := s.qb.GetTorrents(ctx, "all")
	if err != nil {
		log.Printf("Error getting torrents from qBittorrent: %v", err)
		return
	}

	activeHashes := make(map[string]bool)
	for _, t := range torrents {
		activeHashes[t.Hash] = true
		// Update our downloads table
		_, err := database.DB.Exec(`
			UPDATE downloads 
			SET progress = $1, status = $2, updated_at = NOW() 
			WHERE torrent_hash = $3`,
			t.Progress, t.State, t.Hash)
		if err != nil {
			log.Printf("Error updating download status: %v", err)
			continue
		}

		// If finished, update request status
		if t.Progress >= 1.0 || t.State == "uploading" || t.State == "stalledUP" {
			_, err = database.DB.Exec(`
				UPDATE requests 
				SET status = 'completed', updated_at = NOW() 
				WHERE id = (SELECT request_id FROM downloads WHERE torrent_hash = $1)`,
				t.Hash)
			if err != nil {
				log.Printf("Error updating request status to completed: %v", err)
			}
		}
	}

	// SELF-HEALING: Reset requests that are "downloading" but missing from qBittorrent
	rows, err := database.DB.Query("SELECT request_id, torrent_hash FROM downloads WHERE status = 'downloading'")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var reqID int
			var hash string
			if err := rows.Scan(&reqID, &hash); err == nil {
				if !activeHashes[hash] {
					log.Printf("Download %s vanished from qBittorrent, resetting request %d to approved", hash, reqID)
					database.DB.Exec("UPDATE requests SET status = 'approved' WHERE id = $1", reqID)
					database.DB.Exec("DELETE FROM downloads WHERE torrent_hash = $1", hash)
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
				log.Printf("[SUBTITLES] Still in OpenSubtitles quota lockdown until %v", t.Add(5*time.Minute))
				return
			}
		}
	}

	// 2. Fetch pending jobs that are ready for retry
	rows, err := database.DB.Query("SELECT id, media_type, media_id FROM subtitle_queue WHERE next_retry <= CURRENT_TIMESTAMP")
	if err != nil {
		log.Printf("Error querying subtitle queue: %v", err)
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
		log.Printf("[SUBTITLES] Retrying subtitle download for %s %d", j.mType, j.mID)
		var err error
		if j.mType == "movie" {
			err = DownloadSubtitlesForMovie(s.cfg, j.mID)
		} else {
			err = DownloadSubtitlesForEpisode(s.cfg, j.mID)
		}

		if err == nil {
			// Success! Remove from queue
			database.DB.Exec("DELETE FROM subtitle_queue WHERE id = $1", j.id)
			log.Printf("[SUBTITLES] Successfully downloaded subtitles for %s %d on retry", j.mType, j.mID)
		} else {
			// Check if it was a quota error again
			if strings.Contains(err.Error(), "406") {
				// Quota hit again, next_retry was updated by QueueSubtitleDownload called inside DownloadSubtitlesForX
				log.Printf("[SUBTITLES] Hit quota again while retrying %s %d", j.mType, j.mID)
				break // Stop processing queue for now
			} else {
				// Some other error, increment retry count and back off
				database.DB.Exec("UPDATE subtitle_queue SET retry_count = retry_count + 1, next_retry = CURRENT_TIMESTAMP + interval '1 hour' WHERE id = $1", j.id)

				var retries int
				database.DB.QueryRow("SELECT retry_count FROM subtitle_queue WHERE id = $1", j.id).Scan(&retries)
				if retries > 5 {
					log.Printf("[SUBTITLES] Giving up on subtitles for %s %d after 5 retries", j.mType, j.mID)
					database.DB.Exec("DELETE FROM subtitle_queue WHERE id = $1", j.id)
				}
			}
		}
	}
}
