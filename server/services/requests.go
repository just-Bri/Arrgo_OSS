package services

import (
	"Arrgo/config"
	"Arrgo/database"
	"Arrgo/models"
	"context"
	"database/sql"
	"log/slog"
	"slices"
	"strconv"
	"strings"
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
		// Look for active requests (approved or downloading) to append seasons to
		err := database.DB.QueryRow("SELECT id, seasons FROM requests WHERE tvdb_id = $1 AND media_type = 'show' AND status IN ('approved', 'downloading')", req.TVDBID).Scan(&existingID, &existingSeasons)
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
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'approved', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`
	_, err := database.DB.Exec(query, req.UserID, req.Title, req.MediaType, req.TMDBID, req.TVDBID, req.Year, req.PosterPath, req.Overview, req.Seasons)
	if err == nil {
		slog.Info("Successfully created new request", "title", req.Title, "media_type", req.MediaType, "status", "approved")
	}
	return err
}

func GetRequests() ([]models.Request, error) {
	query := `
		SELECT r.id, r.user_id, u.username, r.title, r.media_type, r.tmdb_id, r.tvdb_id, r.imdb_id, r.year, r.poster_path, r.overview, r.seasons, r.status, r.created_at, r.updated_at
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
		err := rows.Scan(&req.ID, &req.UserID, &req.Username, &req.Title, &req.MediaType, &tmdbID, &tvdbID, &imdbID, &req.Year, &req.PosterPath, &req.Overview, &seasons, &req.Status, &req.CreatedAt, &req.UpdatedAt)
		if err != nil {
			return nil, err
		}
		req.TMDBID = tmdbID.String
		req.TVDBID = tvdbID.String
		req.IMDBID = imdbID.String
		req.Seasons = seasons.String
		requests = append(requests, req)
	}
	return requests, nil
}

func GetPendingRequestCounts() (int, int, error) {
	var movieCount, showCount int

	err := database.DB.QueryRow("SELECT COUNT(*) FROM requests WHERE media_type = 'movie' AND status = 'approved'").Scan(&movieCount)
	if err != nil {
		return 0, 0, err
	}

	err = database.DB.QueryRow("SELECT COUNT(*) FROM requests WHERE media_type = 'show' AND status = 'approved'").Scan(&showCount)
	if err != nil {
		return 0, 0, err
	}

	return movieCount, showCount, nil
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
				// 2. Delete from qBittorrent and remove files
				if qb != nil {
					_ = qb.DeleteTorrent(context.Background(), hash, true)
				}
			}
		}
	}

	// 3. Delete from database (cascades to downloads table due to foreign key)
	_, err = database.DB.Exec("DELETE FROM requests WHERE id = $1", id)
	return err
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

			// Get seasons in library
			rows, err := database.DB.Query("SELECT season_number FROM seasons WHERE show_id = $1 ORDER BY season_number", showID)
			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var sn int
					if err := rows.Scan(&sn); err == nil {
						status.Seasons = append(status.Seasons, sn)
					}
				}
			}
		}

		// Always check for requested seasons, even if show exists (partial match)
		// Check for active requests (approved or downloading)
		var reqSeasons sql.NullString
		var reqStatus string
		err = database.DB.QueryRow("SELECT seasons, status FROM requests WHERE tvdb_id = $1 AND media_type = 'show' AND status IN ('approved', 'downloading')", externalID).Scan(&reqSeasons, &reqStatus)
		if err == nil {
			if reqSeasons.Valid && reqSeasons.String != "" {
				seasonStrs := strings.Split(reqSeasons.String, ",")
				for _, s := range seasonStrs {
					if sn, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
						status.RequestedSeasons = append(status.RequestedSeasons, sn)
					}
				}
			}
			if !status.Exists {
				status.Message = "Already requested (Status: " + reqStatus + ")"
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
