package services

import (
	"Arrgo/database"
	"Arrgo/models"
	"database/sql"
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
		// Look for either pending or approved requests to append seasons to
		err := database.DB.QueryRow("SELECT id, seasons FROM requests WHERE tvdb_id = $1 AND media_type = 'show' AND status IN ('pending', 'approved')", req.TVDBID).Scan(&existingID, &existingSeasons)
		if err == nil {
			// Update existing request
			newSeasons := existingSeasons
			reqSeasons := strings.Split(req.Seasons, ",")
			existingSeasonsList := strings.Split(existingSeasons, ",")
			
			for _, rs := range reqSeasons {
				found := false
				for _, es := range existingSeasonsList {
					if rs == es {
						found = true
						break
					}
				}
				if !found {
					if newSeasons != "" {
						newSeasons += ","
					}
					newSeasons += rs
				}
			}
			
			_, err = database.DB.Exec("UPDATE requests SET seasons = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2", newSeasons, existingID)
			return err
		}
	}

	query := `
		INSERT INTO requests (user_id, title, media_type, tmdb_id, tvdb_id, year, poster_path, overview, seasons, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'approved', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`
	_, err := database.DB.Exec(query, req.UserID, req.Title, req.MediaType, req.TMDBID, req.TVDBID, req.Year, req.PosterPath, req.Overview, req.Seasons)
	return err
}

func GetRequests() ([]models.Request, error) {
	query := `
		SELECT r.id, r.user_id, u.username, r.title, r.media_type, r.tmdb_id, r.tvdb_id, r.year, r.poster_path, r.overview, r.seasons, r.status, r.created_at, r.updated_at
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
		var tmdbID, tvdbID, seasons sql.NullString
		err := rows.Scan(&req.ID, &req.UserID, &req.Username, &req.Title, &req.MediaType, &tmdbID, &tvdbID, &req.Year, &req.PosterPath, &req.Overview, &seasons, &req.Status, &req.CreatedAt, &req.UpdatedAt)
		if err != nil {
			return nil, err
		}
		req.TMDBID = tmdbID.String
		req.TVDBID = tvdbID.String
		req.Seasons = seasons.String
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

func UpdateRequestStatus(id int, status string) error {
	_, err := database.DB.Exec("UPDATE requests SET status = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2", status, id)
	return err
}

func CheckLibraryStatus(mediaType string, externalID string) (LibraryStatus, error) {
	status := LibraryStatus{Exists: false}

	if mediaType == "movie" {
		var id int
		err := database.DB.QueryRow("SELECT id FROM movies WHERE tmdb_id = $1", externalID).Scan(&id)
		if err == nil {
			status.Exists = true
			status.LocalID = id
			status.Message = "Already in library"
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
		err := database.DB.QueryRow("SELECT id FROM shows WHERE tvdb_id = $1", externalID).Scan(&showID)
		if err == nil {
			status.Exists = true
			status.LocalID = showID

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
		var reqSeasons sql.NullString
		var reqStatus string
		err = database.DB.QueryRow("SELECT seasons, status FROM requests WHERE tvdb_id = $1 AND media_type = 'show' AND status != 'cancelled' AND status != 'completed'", externalID).Scan(&reqSeasons, &reqStatus)
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
