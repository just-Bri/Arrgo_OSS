package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"

	"Arrgo/config"
	"Arrgo/database"
	"Arrgo/models"
)

type AutomationService struct {
	cfg        *config.Config
	qb         *QBittorrentClient
	httpClient *http.Client
}

func NewAutomationService(cfg *config.Config, qb *QBittorrentClient) *AutomationService {
	return &AutomationService{
		cfg:        cfg,
		qb:         qb,
		httpClient: &http.Client{Timeout: 30 * time.Second},
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

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.ProcessApprovedRequests(ctx)
		case <-updateTicker.C:
			s.UpdateDownloadStatus(ctx)
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
	searchURL := fmt.Sprintf("%s/search?q=%s&type=%s&format=json",
		s.cfg.IndexerURL, url.QueryEscape(r.Title), r.MediaType)

	resp, err := s.httpClient.Get(searchURL)
	if err != nil {
		return fmt.Errorf("failed to call indexer: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
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
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
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
		category = "arrgo-tv"
		savePath = s.cfg.IncomingTVPath
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
