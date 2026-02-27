package handlers

import (
	"Arrgo/config"
	"Arrgo/database"
	"Arrgo/models"
	"Arrgo/services"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"log/slog"
	"net/http"
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
}

type IncomingShowWithSeasons struct {
	models.Show
	Seasons       []int                   // Season numbers that are in incoming
	SeedingStatus *services.SeedingStatus // Seeding status (from any episode with torrent_hash)
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
		allTorrents, err = qb.GetTorrentsDetailed(ctx, "")
		if err != nil {
			slog.Warn("Failed to get torrents from qBittorrent", "error", err)
			allTorrents = nil
		}
	}

	// Get incoming movies and shows using shared helpers
	allMovies, err := services.GetMovies()
	if err != nil {
		slog.Error("Error getting movies for admin", "error", err)
		allMovies = []models.Movie{}
	}
	_, incomingMoviesRaw := SeparateIncomingMovies(allMovies, cfg, true, allTorrents)

	// Add seeding status to incoming movies
	incomingMovies := make([]IncomingMovieWithSeeding, 0, len(incomingMoviesRaw))
	for _, movie := range incomingMoviesRaw {
		var seedingStatus *services.SeedingStatus
		if movie.TorrentHash != "" && allTorrents != nil {
			seedingStatus = services.GetSeedingStatusFromList(allTorrents, movie.TorrentHash)
		}
		incomingMovies = append(incomingMovies, IncomingMovieWithSeeding{
			Movie:         movie,
			SeedingStatus: seedingStatus,
		})
	}

	allShows, err := services.GetShows()
	if err != nil {
		slog.Error("Error getting shows for admin", "error", err)
		allShows = []models.Show{}
	}
	_, incomingShowsRaw := SeparateIncomingShows(allShows, cfg, true, allTorrents)

	// Add season information and seeding status to incoming shows
	incomingShows := make([]IncomingShowWithSeasons, 0, len(incomingShowsRaw))
	for _, show := range incomingShowsRaw {
		seasons := getIncomingSeasonsForShow(show.ID, cfg.IncomingShowsPath)

		// Get seeding status from any episode with a torrent hash
		var seedingStatus *services.SeedingStatus
		if allTorrents != nil {
			var torrentHash string
			err := database.DB.QueryRow(`
				SELECT e.torrent_hash FROM episodes e
				JOIN seasons s ON e.season_id = s.id
				WHERE s.show_id = $1 AND e.torrent_hash IS NOT NULL AND e.torrent_hash != ''
				LIMIT 1`, show.ID).Scan(&torrentHash)
			if err == nil && torrentHash != "" {
				seedingStatus = services.GetSeedingStatusFromList(allTorrents, torrentHash)
			}
		}

		incomingShows = append(incomingShows, IncomingShowWithSeasons{
			Show:          show,
			Seasons:       seasons,
			SeedingStatus: seedingStatus,
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
