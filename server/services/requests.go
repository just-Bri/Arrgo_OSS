package services

import (
	"Arrgo/database"
	"Arrgo/models"
	"database/sql"
)

type LibraryStatus struct {
	Exists    bool   `json:"exists"`
	Message   string `json:"message"`
	Seasons   []int  `json:"seasons,omitempty"` // Season numbers already in library
}

func CreateRequest(req models.Request) error {
	query := `
		INSERT INTO requests (user_id, title, media_type, tmdb_id, tvdb_id, year, poster_path, overview, seasons, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'pending', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
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

func CheckLibraryStatus(mediaType string, externalID string) (LibraryStatus, error) {
	status := LibraryStatus{Exists: false}

	if mediaType == "movie" {
		var id int
		err := database.DB.QueryRow("SELECT id FROM movies WHERE tmdb_id = $1", externalID).Scan(&id)
		if err == nil {
			status.Exists = true
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
			
			// Get seasons
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
			
			if len(status.Seasons) > 0 {
				status.Message = "Partial/Full library match"
			} else {
				status.Message = "Show exists but no seasons found"
			}
		} else if err == sql.ErrNoRows {
			// Not in library, check if already requested
			var reqStatus string
			err = database.DB.QueryRow("SELECT status FROM requests WHERE tvdb_id = $1 AND media_type = 'show'", externalID).Scan(&reqStatus)
			if err == nil {
				status.Message = "Already requested (Status: " + reqStatus + ")"
			}
		}
	}

	return status, nil
}
