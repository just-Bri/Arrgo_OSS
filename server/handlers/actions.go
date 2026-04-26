package handlers

import (
	"Arrgo/config"
	"Arrgo/database"
	"Arrgo/models"
	"Arrgo/services"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

var (
	importMoviesMutex sync.Mutex
	importShowsMutex  sync.Mutex
)

func ScanMoviesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	_, err := RequireAdmin(r)
	if err != nil {
		http.Error(w, "Unauthorized: Admin only", http.StatusUnauthorized)
		return
	}

	cfg := config.Load()
	ctx, _ := services.StartScan(services.ScanMovieLibrary)
	if ctx == nil {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	go services.ScanMovies(ctx, cfg, false)

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func ScanShowsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	_, err := RequireAdmin(r)
	if err != nil {
		http.Error(w, "Unauthorized: Admin only", http.StatusUnauthorized)
		return
	}

	cfg := config.Load()
	ctx, _ := services.StartScan(services.ScanShowLibrary)
	if ctx == nil {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	go services.ScanShows(ctx, cfg, false)

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func ScanIncomingMoviesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	_, err := RequireAdmin(r)
	if err != nil {
		http.Error(w, "Unauthorized: Admin only", http.StatusUnauthorized)
		return
	}

	cfg := config.Load()
	ctx, _ := services.StartScan(services.ScanIncomingMovies)
	if ctx == nil {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	go services.ScanMovies(ctx, cfg, true)

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func ScanIncomingShowsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	_, err := RequireAdmin(r)
	if err != nil {
		http.Error(w, "Unauthorized: Admin only", http.StatusUnauthorized)
		return
	}

	cfg := config.Load()
	ctx, _ := services.StartScan(services.ScanIncomingShows)
	if ctx == nil {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	go services.ScanShows(ctx, cfg, true)

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func StopScanHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	_, err := RequireAdmin(r)
	if err != nil {
		http.Error(w, "Unauthorized: Admin only", http.StatusUnauthorized)
		return
	}

	scanType := services.ScanType(r.URL.Query().Get("type"))
	services.StopScan(scanType)

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func ImportAllMoviesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !importMoviesMutex.TryLock() {
		slog.Info("Mass movie import already in progress, skipping")
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	defer importMoviesMutex.Unlock()

	_, err := RequireAdmin(r)
	if err != nil {
		http.Error(w, "Unauthorized: Admin only", http.StatusUnauthorized)
		return
	}

	cfg := config.Load()
	allMovies, err := services.GetMovies()
	if err != nil {
		slog.Error("Error getting movies for mass import", "error", err)
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	var moviesToImport []models.Movie
	for _, m := range allMovies {
		if strings.HasPrefix(m.Path, cfg.IncomingMoviesPath) && m.Status == "matched" {
			moviesToImport = append(moviesToImport, m)
		}
	}

	if len(moviesToImport) == 0 {
		slog.Info("No movies found to import")
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	slog.Info("Starting mass movie import", "count", len(moviesToImport), "workers", services.DefaultWorkerCount)

	type importFailure struct {
		ID    int
		Title string
		Err   string
	}

	var succeeded int
	var failures []importFailure
	var mu sync.Mutex
	var wg sync.WaitGroup
	movieChan := make(chan models.Movie, len(moviesToImport))

	// Start workers
	for range services.DefaultWorkerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for m := range movieChan {
				// Don't cleanup during import - we'll do it once at the end
				if err := services.RenameAndMoveMovieWithCleanup(cfg, m.ID, false); err != nil {
					slog.Error("Error importing movie", "movie_id", m.ID, "title", m.Title, "error", err)
					mu.Lock()
					failures = append(failures, importFailure{ID: m.ID, Title: m.Title, Err: err.Error()})
					mu.Unlock()
				} else {
					mu.Lock()
					succeeded++
					mu.Unlock()
				}
			}
		}()
	}

	// Dispatch movies
	for _, m := range moviesToImport {
		movieChan <- m
	}
	close(movieChan)
	wg.Wait()

	// Final cleanup pass
	services.CleanupEmptyDirs(cfg.IncomingMoviesPath)

	slog.Info("Mass movie import complete", "succeeded", succeeded, "failed", len(failures), "total", len(moviesToImport))
	for _, f := range failures {
		slog.Warn("Movie import failed", "movie_id", f.ID, "title", f.Title, "error", f.Err)
	}

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func RenameAllLibraryMoviesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	_, err := RequireAdmin(r)
	if err != nil {
		http.Error(w, "Unauthorized: Admin only", http.StatusUnauthorized)
		return
	}

	cfg := config.Load()
	allMovies, err := services.GetMovies()
	if err != nil {
		slog.Error("Error getting movies for renaming", "error", err)
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	slog.Info("Starting mass movie renaming", "count", len(allMovies))

	count := 0
	for _, m := range allMovies {
		if m.Status == "matched" || m.Status == "ready" {
			if err := services.RenameAndMoveMovie(cfg, m.ID); err != nil {
				slog.Error("Error renaming movie", "movie_id", m.ID, "title", m.Title, "error", err)
			} else {
				count++
			}
		}
	}

	slog.Info("Mass movie renaming complete", "movies_renamed", count)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func ImportAllShowsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !importShowsMutex.TryLock() {
		slog.Info("Mass show import already in progress, skipping")
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	defer importShowsMutex.Unlock()

	_, err := RequireAdmin(r)
	if err != nil {
		http.Error(w, "Unauthorized: Admin only", http.StatusUnauthorized)
		return
	}

	cfg := config.Load()
	allShows, err := services.GetShows()
	if err != nil {
		slog.Error("Error getting shows for mass import", "error", err)
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	var showsToImport []models.Show
	for _, s := range allShows {
		if strings.HasPrefix(s.Path, cfg.IncomingShowsPath) && s.Status == "matched" {
			showsToImport = append(showsToImport, s)
		}
	}

	if len(showsToImport) == 0 {
		slog.Info("No shows found to import")
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	slog.Info("Starting mass show import", "count", len(showsToImport), "workers", services.DefaultWorkerCount)

	type importFailure struct {
		ID    int
		Title string
		Err   string
	}

	var succeeded int
	var failures []importFailure
	var mu sync.Mutex
	var wg sync.WaitGroup
	showChan := make(chan models.Show, len(showsToImport))

	// Start workers
	for i := 0; i < services.DefaultWorkerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for s := range showChan {
				// Don't cleanup during import - we'll do it once at the end
				if err := services.RenameAndMoveShowWithCleanup(cfg, s.ID, false); err != nil {
					slog.Error("Error importing show", "show_id", s.ID, "title", s.Title, "error", err)
					mu.Lock()
					failures = append(failures, importFailure{ID: s.ID, Title: s.Title, Err: err.Error()})
					mu.Unlock()
				} else {
					mu.Lock()
					succeeded++
					mu.Unlock()
				}
			}
		}()
	}

	// Dispatch shows
	for _, s := range showsToImport {
		showChan <- s
	}
	close(showChan)
	wg.Wait()

	// Final cleanup pass
	services.CleanupEmptyDirs(cfg.IncomingShowsPath)

	slog.Info("Mass show import complete", "succeeded", succeeded, "failed", len(failures), "total", len(showsToImport))
	for _, f := range failures {
		slog.Warn("Show import failed", "show_id", f.ID, "title", f.Title, "error", f.Err)
	}

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func RenameAllLibraryShowsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	_, err := RequireAdmin(r)
	if err != nil {
		http.Error(w, "Unauthorized: Admin only", http.StatusUnauthorized)
		return
	}

	cfg := config.Load()
	allShows, err := services.GetShows()
	if err != nil {
		slog.Error("Error getting shows for renaming", "error", err)
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	slog.Info("Starting mass show renaming", "count", len(allShows))

	count := 0
	for _, s := range allShows {
		if s.Status == "matched" || s.Status == "ready" {
			if err := services.RenameAndMoveShow(cfg, s.ID); err != nil {
				slog.Error("Error renaming show", "show_id", s.ID, "title", s.Title, "error", err)
			} else {
				count++
			}
		}
	}

	slog.Info("Mass show renaming complete", "shows_renamed", count)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func RenameMovieHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := r.URL.Query().Get("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	cfg := config.Load()
	if err := services.RenameAndMoveMovie(cfg, id); err != nil {
		slog.Error("Error renaming movie", "movie_id", id, "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func RenameShowHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := r.URL.Query().Get("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	cfg := config.Load()

	if err := services.RenameAndMoveShow(cfg, id); err != nil {
		slog.Error("Error renaming show", "show_id", id, "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (h *Handlers) DownloadSubtitlesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user, err := GetCurrentUser(r)
	if err != nil || user == nil || !user.IsAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	mediaType := r.URL.Query().Get("type")
	idStr := r.URL.Query().Get("id")
	id, _ := strconv.Atoi(idStr)

	switch mediaType {
	case "movie":
		m, err := services.GetMovieByID(id)
		if err != nil {
			http.Error(w, "Movie not found", http.StatusNotFound)
			return
		}

		// If IMDB ID is missing, try to re-match it first
		if m.IMDBID == "" {
			slog.Info("IMDB ID missing for movie, attempting re-match", "movie_id", m.ID, "title", m.Title)
			if err := h.Metadata.MatchMovie(m.ID); err == nil {
				// Reload movie to get new IMDB ID
				m, _ = services.GetMovieByID(id)
			}
		}

		if m.IMDBID == "" {
			http.Error(w, "IMDB ID missing and re-match failed", http.StatusBadRequest)
			return
		}

		if err := h.Subtitle.DownloadSubtitlesForMovie(m.ID); err != nil {
			slog.Error("Manual subtitle download failed for movie", "movie_id", m.ID, "title", m.Title, "error", err)
			http.Error(w, "Download failed", http.StatusInternalServerError)
			return
		}
	case "episode":
		var e models.Episode
		var sh models.Show
		var s models.Season
		query := `
			SELECT e.id, e.episode_number, e.file_path, s.season_number, sh.id, sh.title, sh.imdb_id
			FROM episodes e
			JOIN seasons s ON e.season_id = s.id
			JOIN shows sh ON s.show_id = sh.id
			WHERE e.id = $1
		`
		err := database.DB.QueryRow(query, id).Scan(&e.ID, &e.EpisodeNumber, &e.FilePath, &s.SeasonNumber, &sh.ID, &sh.Title, &sh.IMDBID)
		if err != nil {
			slog.Error("Error fetching episode for subtitle download", "episode_id", id, "error", err)
			http.Error(w, "Episode not found", http.StatusNotFound)
			return
		}

		// If IMDB ID is missing, try to re-match the parent show first
		if sh.IMDBID == "" {
			slog.Info("IMDB ID missing for show, attempting re-match", "show_id", sh.ID, "title", sh.Title)
			if err := h.Metadata.MatchShow(sh.ID); err == nil {
				// Reload IMDB ID
				database.DB.QueryRow("SELECT imdb_id FROM shows WHERE id = $1", sh.ID).Scan(&sh.IMDBID)
			}
		}

		if sh.IMDBID == "" {
			http.Error(w, "IMDB ID missing and re-match failed", http.StatusBadRequest)
			return
		}

		if err := h.Subtitle.DownloadSubtitlesForEpisode(e.ID); err != nil {
			slog.Error("Manual subtitle download failed for episode",
				"episode_id", e.ID,
				"show_title", sh.Title,
				"season", s.SeasonNumber,
				"episode", e.EpisodeNumber,
				"error", err)
			http.Error(w, "Download failed", http.StatusInternalServerError)
			return
		}
	default:
		http.Error(w, "Invalid media type", http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Subtitle downloaded successfully"))
}

func NukeLibraryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user, err := RequireAdmin(r)
	if err != nil {
		http.Error(w, "Unauthorized: Admin only", http.StatusUnauthorized)
		return
	}

	slog.Warn("NUKE operation started", "user", user.Username, "user_id", user.ID)

	// Start a transaction for safety
	tx, err := database.DB.Begin()
	if err != nil {
		slog.Error("Failed to start nuke transaction", "error", err, "user", user.Username)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// Order matters if foreign keys aren't all cascading, but ours are mostly.
	// Deleting shows cascades to seasons and episodes.
	// Use TRUNCATE with RESTART IDENTITY to reset auto-incrementing IDs to 1.
	// CASCADE ensures dependent records in other tables are also cleared.
	nukeQuery := `TRUNCATE TABLE 
		episodes, 
		seasons, 
		shows, 
		movies, 
		requests, 
		subtitle_queue, 
		settings, 
		downloads, 
		tvdb_episodes 
		RESTART IDENTITY CASCADE`

	if _, err := tx.Exec(nukeQuery); err != nil {
		tx.Rollback()
		slog.Error("Failed to execute nuke query", "error", err, "user", user.Username)
		http.Error(w, "Failed to clear tables and reset sequences", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		slog.Error("Failed to commit nuke transaction", "error", err, "user", user.Username)
		http.Error(w, "Failed to commit changes", http.StatusInternalServerError)
		return
	}

	slog.Warn("NUKE operation completed successfully", "user", user.Username)

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// GetMovieAlternativesHandler returns alternative matches for a movie
func (h *Handlers) GetMovieAlternativesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	_, err := RequireAdmin(r)
	if err != nil {
		http.Error(w, "Unauthorized: Admin only", http.StatusUnauthorized)
		return
	}

	movieID, err := ParseIDFromQuery(r, "id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	alternatives, err := h.Metadata.GetMovieAlternatives(movieID)
	if err != nil {
		slog.Error("Error getting movie alternatives", "movie_id", movieID, "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(alternatives)
}

// GetShowAlternativesHandler returns alternative matches for a show
func (h *Handlers) GetShowAlternativesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	_, err := RequireAdmin(r)
	if err != nil {
		http.Error(w, "Unauthorized: Admin only", http.StatusUnauthorized)
		return
	}

	showID, err := ParseIDFromQuery(r, "id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	alternatives, err := h.Metadata.GetShowAlternatives(showID)
	if err != nil {
		slog.Error("Error getting show alternatives", "show_id", showID, "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(alternatives)
}

// RematchMovieHandler updates a movie with a new TMDB ID
func (h *Handlers) RematchMovieHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	_, err := RequireAdmin(r)
	if err != nil {
		http.Error(w, "Unauthorized: Admin only", http.StatusUnauthorized)
		return
	}

	movieID, err := ParseIDFromQuery(r, "id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var req struct {
		TMDBID string `json:"tmdb_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.TMDBID == "" {
		http.Error(w, "tmdb_id is required", http.StatusBadRequest)
		return
	}

	if err := h.Metadata.RematchMovie(movieID, req.TMDBID); err != nil {
		slog.Error("Error rematching movie", "movie_id", movieID, "tmdb_id", req.TMDBID, "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// RematchShowHandler updates a show with a new TVDB ID
func (h *Handlers) RematchShowHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	_, err := RequireAdmin(r)
	if err != nil {
		http.Error(w, "Unauthorized: Admin only", http.StatusUnauthorized)
		return
	}

	showID, err := ParseIDFromQuery(r, "id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var req struct {
		TVDBID string `json:"tvdb_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.TVDBID == "" {
		http.Error(w, "tvdb_id is required", http.StatusBadRequest)
		return
	}

	if err := h.Metadata.RematchShow(showID, req.TVDBID); err != nil {
		slog.Error("Error rematching show", "show_id", showID, "tvdb_id", req.TVDBID, "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}
