package services

import (
	"Arrgo/config"
	"Arrgo/database"
	"Arrgo/models"
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"
)

type LibraryStatus struct {
	Exists           bool   `json:"exists"`
	LocalID          int    `json:"local_id,omitempty"`
	Message          string `json:"message"`
	Seasons          []int  `json:"seasons,omitempty"`           // Season numbers already in library
	RequestedSeasons []int  `json:"requested_seasons,omitempty"` // Season numbers already requested
}

func CreateRequest(req models.Request) error {
	// If it's a show, we might be adding seasons to an existing request
	if req.MediaType == "show" {
		var existingID int
		var existingSeasons string
		// Look for active requests (pending or downloading) to append seasons to
		err := database.DB.QueryRow("SELECT id, seasons FROM requests WHERE tvdb_id = $1 AND media_type = 'show' AND status IN ('pending', 'downloading')", req.TVDBID).Scan(&existingID, &existingSeasons)
		if err == nil {
			// Update existing request
			newSeasons := existingSeasons
			reqSeasons := strings.Split(req.Seasons, ",")
			existingSeasonsList := strings.Split(existingSeasons, ",")

			for _, rs := range reqSeasons {
				found := slices.Contains(existingSeasonsList, rs)
				if !found {
					if newSeasons != "" {
						newSeasons += ","
					}
					newSeasons += rs
				}
			}

			slog.Info("Updating existing show request with additional seasons", "request_id", existingID, "title", req.Title, "existing_seasons", existingSeasons, "new_seasons", newSeasons)
			_, err = database.DB.Exec("UPDATE requests SET seasons = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2", newSeasons, existingID)
			if err == nil {
				slog.Info("Successfully updated existing request", "request_id", existingID, "seasons", newSeasons)
			}
			return err
		}
	}

	slog.Info("Creating new request", "title", req.Title, "media_type", req.MediaType, "seasons", req.Seasons, "user_id", req.UserID)
	query := `
		INSERT INTO requests (user_id, title, media_type, tmdb_id, tvdb_id, year, poster_path, overview, seasons, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'pending', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`
	_, err := database.DB.Exec(query, req.UserID, req.Title, req.MediaType, req.TMDBID, req.TVDBID, req.Year, req.PosterPath, req.Overview, req.Seasons)
	if err == nil {
		slog.Info("Successfully created new request", "title", req.Title, "media_type", req.MediaType, "status", "pending")
	}
	return err
}

func GetRequests() ([]models.Request, error) {
	query := `
		SELECT r.id, r.user_id, u.username, r.title, r.media_type, r.tmdb_id, r.tvdb_id, r.imdb_id, r.year, r.poster_path, r.overview, r.seasons, r.status, r.retry_count, r.last_search_at, r.created_at, r.updated_at
		FROM requests r
		JOIN users u ON r.user_id = u.id
		ORDER BY r.created_at DESC
	`
	rows, err := database.DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var requests []models.Request
	for rows.Next() {
		var req models.Request
		var tmdbID, tvdbID, imdbID, seasons sql.NullString
		var lastSearchAt sql.NullTime
		err := rows.Scan(&req.ID, &req.UserID, &req.Username, &req.Title, &req.MediaType, &tmdbID, &tvdbID, &imdbID, &req.Year, &req.PosterPath, &req.Overview, &seasons, &req.Status, &req.RetryCount, &lastSearchAt, &req.CreatedAt, &req.UpdatedAt)
		if err != nil {
			return nil, err
		}
		// Decode unicode escape sequences in title (e.g., \u0026 -> &)
		req.Title = decodeUnicodeEscapes(req.Title)
		req.TMDBID = tmdbID.String
		req.TVDBID = tvdbID.String
		req.IMDBID = imdbID.String
		req.Seasons = seasons.String
		if lastSearchAt.Valid {
			req.LastSearchAt = &lastSearchAt.Time
		}
		requests = append(requests, req)
	}
	return requests, nil
}

func GetPendingRequestCounts() (int, int, error) {
	var movieCount, showCount int

	err := database.DB.QueryRow("SELECT COUNT(*) FROM requests WHERE media_type = 'movie' AND status = 'pending'").Scan(&movieCount)
	if err != nil {
		return 0, 0, err
	}

	err = database.DB.QueryRow("SELECT COUNT(*) FROM requests WHERE media_type = 'show' AND status = 'pending'").Scan(&showCount)
	if err != nil {
		return 0, 0, err
	}

	return movieCount, showCount, nil
}

// decodeUnicodeEscapes decodes unicode escape sequences like \u0026 to their actual characters
func decodeUnicodeEscapes(s string) string {
	// Use json.Unmarshal to decode unicode escape sequences
	// We need to wrap it in quotes to make it a valid JSON string
	quoted := `"` + s + `"`
	var decoded string
	if err := json.Unmarshal([]byte(quoted), &decoded); err == nil {
		return decoded
	}
	// If decoding fails, return original string
	return s
}

func UpdateRequestStatus(id int, status string) error {
	_, err := database.DB.Exec("UPDATE requests SET status = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2", status, id)
	return err
}

func DeleteRequest(id int, qb *QBittorrentClient) error {
	// 1. Get associated torrent hashes from downloads table
	rows, err := database.DB.Query("SELECT torrent_hash FROM downloads WHERE request_id = $1", id)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var hash string
			if err := rows.Scan(&hash); err == nil {
				// 2. IMPORTANT: Only remove torrent from qBittorrent WITHOUT deleting files
				// Files are only deleted if they've been imported (handled by cleanup logic)
				// This ensures files are preserved until they're imported
				if qb != nil {
					_ = qb.DeleteTorrent(context.Background(), hash, false)
				}
			}
		}
	}

	// 3. Delete from database (cascades to downloads table due to foreign key)
	_, err = database.DB.Exec("DELETE FROM requests WHERE id = $1", id)
	return err
}

// DeleteCompletedRequest deletes only the request record from the database.
// This is used for cleaning up completed requests - it does NOT delete torrents or files.
// The movies/shows remain in the library.
func DeleteCompletedRequest(id int) error {
	// Delete only the request record (cascades to downloads table due to foreign key)
	// We intentionally do NOT delete torrents or files - they should remain in the library
	_, err := database.DB.Exec("DELETE FROM requests WHERE id = $1", id)
	if err == nil {
		slog.Info("Deleted completed request", "request_id", id)
	}
	return err
}

// CleanupCompletedRequests deletes all completed requests from the database.
// This keeps movies/shows in the library but removes the request records.
func CleanupCompletedRequests() (int, error) {
	result, err := database.DB.Exec("DELETE FROM requests WHERE status = 'completed'")
	if err != nil {
		return 0, err
	}

	count, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	if count > 0 {
		slog.Info("Cleaned up completed requests", "count", count)
	}

	return int(count), nil
}

func CheckLibraryStatus(mediaType string, externalID string) (LibraryStatus, error) {
	status := LibraryStatus{Exists: false}

	if mediaType == "movie" {
		var id int
		var path string
		err := database.DB.QueryRow("SELECT id, path FROM movies WHERE tmdb_id = $1", externalID).Scan(&id, &path)
		if err == nil {
			cfg := config.Load()
			status.Exists = true
			status.LocalID = id
			if strings.HasPrefix(path, cfg.IncomingMoviesPath) {
				status.Message = "Downloading/Processing"
			} else {
				status.Message = "Already in library"
			}
		} else if err == sql.ErrNoRows {
			// Not in library, check if already requested
			var reqStatus string
			err = database.DB.QueryRow("SELECT status FROM requests WHERE tmdb_id = $1 AND media_type = 'movie'", externalID).Scan(&reqStatus)
			if err == nil {
				status.Message = "Already requested (Status: " + reqStatus + ")"
			}
		}
	} else if mediaType == "show" {
		var showID int
		var path string
		err := database.DB.QueryRow("SELECT id, path FROM shows WHERE tvdb_id = $1", externalID).Scan(&showID, &path)
		if err == nil {
			cfg := config.Load()
			status.Exists = true
			status.LocalID = showID
			if strings.HasPrefix(path, cfg.IncomingShowsPath) {
				status.Message = "Downloading/Processing"
			} else {
				status.Message = "Already in library"
			}

			// Get seasons in library - only include seasons where ALL episodes are present
			// This prevents marking incomplete seasons as "in library"
			// Fetch expected episodes from TVDB once (if available) to compare against
			var expectedEpisodes []TVDBEpisode
			if cfg.TVDBAPIKey != "" {
				expectedEpisodes, _ = GetTVDBShowEpisodes(cfg, externalID)
			}
			
			// Build map of expected episode counts per season
			expectedCountsBySeason := make(map[int]int)
			for _, ep := range expectedEpisodes {
				expectedCountsBySeason[ep.SeasonNumber]++
			}
			
			rows, err := database.DB.Query("SELECT season_number FROM seasons WHERE show_id = $1 ORDER BY season_number", showID)
			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var sn int
					if err := rows.Scan(&sn); err == nil {
						// Get expected episode count for this season
						expectedCount := expectedCountsBySeason[sn]
						
						// Count actual episodes we have for this season
						var actualCount int
						err := database.DB.QueryRow(`
							SELECT COUNT(*) 
							FROM episodes e
							JOIN seasons s ON e.season_id = s.id
							WHERE s.show_id = $1 AND s.season_number = $2
							AND e.file_path IS NOT NULL 
							AND e.file_path != ''`, showID, sn).Scan(&actualCount)
						
						if err == nil {
							// Only mark season as "in library" if:
							// 1. We have episodes (actualCount > 0)
							// 2. Either we have all expected episodes, OR we don't have TVDB data to compare
							if actualCount > 0 {
								if expectedCount == 0 {
									// No TVDB data - assume complete if we have episodes
									status.Seasons = append(status.Seasons, sn)
								} else if actualCount >= expectedCount {
									// We have all or more episodes than expected
									status.Seasons = append(status.Seasons, sn)
								} else {
									// We have episodes but not all of them - season is incomplete
									slog.Debug("Season marked as incomplete - missing episodes",
										"show_id", showID,
										"season", sn,
										"actual_count", actualCount,
										"expected_count", expectedCount)
								}
							}
						}
					}
				}
			}
		}

		// Always check for requested seasons, even if show exists (partial match)
		// Check for active requests (pending or downloading)
		// Also check completed requests - if episodes are missing, allow re-requesting
		var reqSeasons sql.NullString
		var reqStatus string
		err = database.DB.QueryRow("SELECT seasons, status FROM requests WHERE tvdb_id = $1 AND media_type = 'show' AND status IN ('pending', 'downloading')", externalID).Scan(&reqSeasons, &reqStatus)
		if err == nil {
			if reqSeasons.Valid && reqSeasons.String != "" {
				seasonStrs := strings.Split(reqSeasons.String, ",")
				for _, s := range seasonStrs {
					if sn, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
						// Only add to requested seasons if the season is actually complete in library
						// If season is incomplete, don't block re-requesting
						if slices.Contains(status.Seasons, sn) {
							status.RequestedSeasons = append(status.RequestedSeasons, sn)
						}
					}
				}
			}
			if !status.Exists {
				status.Message = "Already requested (Status: " + reqStatus + ")"
			}
		}
		
		// Also check completed requests - if they're incomplete, don't block re-requesting
		var completedReqSeasons sql.NullString
		err = database.DB.QueryRow("SELECT seasons FROM requests WHERE tvdb_id = $1 AND media_type = 'show' AND status = 'completed'", externalID).Scan(&completedReqSeasons)
		if err == nil && completedReqSeasons.Valid && completedReqSeasons.String != "" {
			// Check if any requested seasons are incomplete - if so, allow re-requesting
			seasonStrs := strings.Split(completedReqSeasons.String, ",")
			for _, s := range seasonStrs {
				if sn, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
					// If season was completed but is not in library (incomplete), don't block
					// The season will only be in status.Seasons if it's complete
					if !slices.Contains(status.Seasons, sn) {
						slog.Debug("Completed request has incomplete season - allowing re-request",
							"tvdb_id", externalID,
							"season", sn)
					}
				}
			}
		}

		if status.Exists {
			if len(status.Seasons) > 0 {
				status.Message = "Partial/Full library match"
			} else {
				status.Message = "Show exists but no seasons found"
			}
		}
	}

	return status, nil
}

// StartCompletedRequestsCleanupWorker starts a background worker that periodically deletes completed requests.
// This keeps movies/shows in the library but removes request records once they're completed.
func StartCompletedRequestsCleanupWorker() {
	slog.Info("Starting completed requests cleanup background worker")

	go func() {
		ticker := time.NewTicker(1 * time.Hour) // Check every 1 hour
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				slog.Debug("Running completed requests cleanup check")
				count, err := CleanupCompletedRequests()
				if err != nil {
					slog.Error("Error during completed requests cleanup", "error", err)
				} else if count > 0 {
					slog.Info("Completed requests cleanup finished", "requests_deleted", count)
				}
			}
		}
	}()
}
