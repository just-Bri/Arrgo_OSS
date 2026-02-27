package handlers

import (
	"Arrgo/config"
	"Arrgo/database"
	"Arrgo/models"
	"Arrgo/services"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"log/slog"
	"net/http"
	"sort"
	"time"
)

var adminTmpl *template.Template

func init() {
	var err error
	funcMap := GetFuncMap()
	adminTmpl, err = template.New("admin").Funcs(funcMap).ParseFiles(
		"templates/layouts/base.html",
		"templates/pages/admin.html",
		"templates/components/navigation.html",
	)
	if err != nil {
		log.Fatal("Failed to parse admin template:", err)
	}
}

type IncomingMovieWithSeeding struct {
	models.Movie
	SeedingStatus *services.SeedingStatus
	IsDownloading bool
}

type IncomingShowWithSeasons struct {
	models.Show
	Seasons       []int                   // Season numbers that are in incoming
	SeedingStatus *services.SeedingStatus // Seeding status (from any episode with torrent_hash)
	IsDownloading bool
}

type AdminPageData struct {
	Username       string
	IsAdmin        bool
	CurrentPage    string
	SearchQuery    string
	IncomingMovies []IncomingMovieWithSeeding
	IncomingShows  []IncomingShowWithSeasons
	Users          []models.User

	ScanningIncomingMovies bool
	ScanningIncomingShows  bool
	ScanningMovieLibrary   bool
	ScanningShowLibrary    bool
}

func AdminHandler(w http.ResponseWriter, r *http.Request) {
	user, err := GetCurrentUser(r)
	if err != nil || user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if !user.IsAdmin {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	cfg := config.Load()
	ctx := context.Background()

	// Get qBittorrent client for seeding status
	qb, err := services.NewQBittorrentClient(cfg)
	if err != nil {
		slog.Warn("Failed to create qBittorrent client for seeding status", "error", err)
		qb = nil
	}

	var allTorrents []services.TorrentStatus
	if qb != nil {
		// Set a timeout for qBittorrent call to avoid hanging the handler
		qbCtx, qbCancel := context.WithTimeout(ctx, 5*time.Second)
		defer qbCancel()

		allTorrents, err = qb.GetTorrentsDetailed(qbCtx, "")
		if err != nil {
			slog.Warn("Failed to get torrents from qBittorrent within timeout", "error", err)
			allTorrents = nil
		}
	}

	// Get incoming movies and shows using optimized service calls
	incomingMoviesRaw, err := services.GetIncomingMovies(cfg.IncomingMoviesPath)
	if err != nil {
		slog.Error("Error getting incoming movies for admin", "error", err)
		incomingMoviesRaw = []models.Movie{}
	}

	// Add seeding status to incoming movies
	incomingMovies := make([]IncomingMovieWithSeeding, 0, len(incomingMoviesRaw))
	for _, movie := range incomingMoviesRaw {
		var seedingStatus *services.SeedingStatus
		isDownloading := false
		if movie.TorrentHash != "" && allTorrents != nil {
			seedingStatus = services.GetSeedingStatusFromList(allTorrents, movie.TorrentHash)
			isDownloading = services.IsTorrentStillDownloadingFromList(allTorrents, movie.TorrentHash)
		}
		incomingMovies = append(incomingMovies, IncomingMovieWithSeeding{
			Movie:         movie,
			SeedingStatus: seedingStatus,
			IsDownloading: isDownloading,
		})
	}

	incomingShowsRaw, err := services.GetIncomingShows(cfg.IncomingShowsPath)
	if err != nil {
		slog.Error("Error getting incoming shows for admin", "error", err)
		incomingShowsRaw = []models.Show{}
	}

	// Optimization: Batch fetch all season/torrent info for incoming shows in one query
	showSeasonsMap := make(map[int][]int)
	showTorrentHashMap := make(map[int]string)

	// We'll also build the downloading status map in the same pass
	downloadingShowMap := make(map[int]bool)

	if user.IsAdmin {
		query := `
			SELECT s.show_id, s.season_number, e.torrent_hash
			FROM episodes e
			JOIN seasons s ON e.season_id = s.id
			WHERE e.file_path LIKE $1 || '%'
			AND e.imported_at IS NULL
		`
		rows, err := database.DB.Query(query, cfg.IncomingShowsPath)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var sid, snum int
				var thash sql.NullString
				if err := rows.Scan(&sid, &snum, &thash); err == nil {
					// Add season if not already there
					exists := false
					for _, s := range showSeasonsMap[sid] {
						if s == snum {
							exists = true
							break
						}
					}
					if !exists {
						showSeasonsMap[sid] = append(showSeasonsMap[sid], snum)
					}

					// Important: if it has a hash, check if it's still downloading
					if thash.Valid && thash.String != "" {
						if showTorrentHashMap[sid] == "" {
							showTorrentHashMap[sid] = thash.String
						}

						isDownloading := false
						if allTorrents != nil {
							// Use provided list for speed
							isDownloading = services.IsTorrentStillDownloadingFromList(allTorrents, thash.String)
						}
						if isDownloading {
							downloadingShowMap[sid] = true
						}
					}
				}
			}
		}
	}

	// Filter and build results
	incomingShows := make([]IncomingShowWithSeasons, 0)
	for _, show := range incomingShowsRaw {
		seasons := showSeasonsMap[show.ID]
		sort.Ints(seasons)

		// Get seeding status from any episode with a torrent hash
		var seedingStatus *services.SeedingStatus
		if allTorrents != nil {
			torrentHash := showTorrentHashMap[show.ID]
			if torrentHash != "" {
				seedingStatus = services.GetSeedingStatusFromList(allTorrents, torrentHash)
			}
		}

		incomingShows = append(incomingShows, IncomingShowWithSeasons{
			Show:          show,
			Seasons:       seasons,
			SeedingStatus: seedingStatus,
			IsDownloading: downloadingShowMap[show.ID],
		})
	}

	allUsers, err := services.GetAllUsers()
	if err != nil {
		slog.Error("Error getting all users for admin", "error", err)
		allUsers = []models.User{}
	}

	data := AdminPageData{
		Username:       user.Username,
		IsAdmin:        user.IsAdmin,
		CurrentPage:    "/admin",
		SearchQuery:    "",
		IncomingMovies: incomingMovies,
		IncomingShows:  incomingShows,
		Users:          allUsers,

		ScanningIncomingMovies: services.IsScanning(services.ScanIncomingMovies),
		ScanningIncomingShows:  services.IsScanning(services.ScanIncomingShows),
		ScanningMovieLibrary:   services.IsScanning(services.ScanMovieLibrary),
		ScanningShowLibrary:    services.IsScanning(services.ScanShowLibrary),
	}

	if err := adminTmpl.ExecuteTemplate(w, "base", data); err != nil {
		slog.Error("Error rendering admin template", "error", err)
		// Don't call http.Error if we've already started writing to w
		return
	}
}

func ScanStatusHandler(w http.ResponseWriter, r *http.Request) {
	user, err := GetCurrentUser(r)
	if err != nil || user == nil || !user.IsAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	status := map[string]bool{
		"incoming_movies": services.IsScanning(services.ScanIncomingMovies),
		"incoming_shows":  services.IsScanning(services.ScanIncomingShows),
		"movie_library":   services.IsScanning(services.ScanMovieLibrary),
		"show_library":    services.IsScanning(services.ScanShowLibrary),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func ScanSubtitlesHandler(w http.ResponseWriter, r *http.Request) {
	user, err := GetCurrentUser(r)
	if err != nil || user == nil || !user.IsAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check for force refresh parameter
	forceRefresh := r.URL.Query().Get("force") == "true"

	result, err := services.ScanAllMediaForSubtitles(forceRefresh)
	if err != nil {
		slog.Error("Error scanning subtitles", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func QueueMissingSubtitlesHandler(w http.ResponseWriter, r *http.Request) {
	user, err := GetCurrentUser(r)
	if err != nil || user == nil || !user.IsAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	queuedCount, err := services.QueueMissingSubtitles()
	if err != nil {
		slog.Error("Error queueing missing subtitles", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"queued_count": queuedCount,
		"message":      fmt.Sprintf("Queued %d media items for subtitle download", queuedCount),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
