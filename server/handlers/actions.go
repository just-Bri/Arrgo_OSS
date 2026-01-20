package handlers

import (
	"Arrgo/config"
	"Arrgo/database"
	"Arrgo/models"
	"Arrgo/services"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

var (
	importMoviesMutex sync.Mutex
	importShowsMutex  sync.Mutex
)

func ScanHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user, err := GetCurrentUser(r)
	if err != nil || user == nil || !user.IsAdmin {
		http.Error(w, "Unauthorized: Admin only", http.StatusUnauthorized)
		return
	}

	cfg := config.Load()
	log.Printf("Manual full scan triggered")

	var wg sync.WaitGroup
	wg.Add(2)

	// Scan Movies in parallel
	go func() {
		defer wg.Done()
		if err := services.ScanMovies(cfg, false); err != nil {
			log.Printf("Error scanning movies: %v", err)
		}
	}()

	// Scan Shows in parallel
	go func() {
		defer wg.Done()
		if err := services.ScanShows(cfg, false); err != nil {
			log.Printf("Error scanning shows: %v", err)
		}
	}()

	wg.Wait()
	log.Printf("Manual full scan complete")

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func ScanIncomingHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user, err := GetCurrentUser(r)
	if err != nil || user == nil || !user.IsAdmin {
		http.Error(w, "Unauthorized: Admin only", http.StatusUnauthorized)
		return
	}

	cfg := config.Load()
	log.Printf("Manual incoming scan triggered")

	var wg sync.WaitGroup
	wg.Add(2)

	// Scan Movies in parallel
	go func() {
		defer wg.Done()
		if err := services.ScanMovies(cfg, true); err != nil {
			log.Printf("Error scanning movies: %v", err)
		}
	}()

	// Scan Shows in parallel
	go func() {
		defer wg.Done()
		if err := services.ScanShows(cfg, true); err != nil {
			log.Printf("Error scanning shows: %v", err)
		}
	}()

	wg.Wait()
	log.Printf("Manual incoming scan complete")

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func ImportAllMoviesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !importMoviesMutex.TryLock() {
		log.Printf("[IMPORT] Mass movie import already in progress, skipping...")
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	defer importMoviesMutex.Unlock()

	user, err := GetCurrentUser(r)
	if err != nil || user == nil || !user.IsAdmin {
		http.Error(w, "Unauthorized: Admin only", http.StatusUnauthorized)
		return
	}

	cfg := config.Load()
	allMovies, err := services.GetMovies()
	if err != nil {
		log.Printf("Error getting movies for mass import: %v", err)
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
		log.Printf("No movies found to import")
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	log.Printf("[IMPORT] Starting mass movie import for %d movies with 4 workers...", len(moviesToImport))

	count := 0
	var mu sync.Mutex
	var wg sync.WaitGroup
	movieChan := make(chan models.Movie, len(moviesToImport))

	// Start 4 workers
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for m := range movieChan {
				if err := services.RenameAndMoveMovie(cfg, m.ID); err != nil {
					log.Printf("Error importing movie %d (%s): %v", m.ID, m.Title, err)
				} else {
					mu.Lock()
					count++
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

	log.Printf("Mass movie import complete: %d movies moved", count)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func ImportAllShowsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !importShowsMutex.TryLock() {
		log.Printf("[IMPORT] Mass show import already in progress, skipping...")
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	defer importShowsMutex.Unlock()

	user, err := GetCurrentUser(r)
	if err != nil || user == nil || !user.IsAdmin {
		http.Error(w, "Unauthorized: Admin only", http.StatusUnauthorized)
		return
	}

	cfg := config.Load()
	allShows, err := services.GetShows()
	if err != nil {
		log.Printf("Error getting shows for mass import: %v", err)
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
		log.Printf("No shows found to import")
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	log.Printf("[IMPORT] Starting mass show import for %d shows with 4 workers...", len(showsToImport))

	count := 0
	var mu sync.Mutex
	var wg sync.WaitGroup
	showChan := make(chan models.Show, len(showsToImport))

	// Start 4 workers
	for range 4 {
		wg.Go(func() {
			for s := range showChan {
				if err := services.RenameAndMoveShow(cfg, s.ID); err != nil {
					log.Printf("Error importing show %d (%s): %v", s.ID, s.Title, err)
				} else {
					mu.Lock()
					count++
					mu.Unlock()
				}
			}
		})
	}

	// Dispatch shows
	for _, s := range showsToImport {
		showChan <- s
	}
	close(showChan)
	wg.Wait()

	// Final cleanup pass
	services.CleanupEmptyDirs(cfg.IncomingShowsPath)

	log.Printf("Mass show import complete: %d shows moved", count)
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
		log.Printf("Error renaming movie %d: %v", id, err)
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
		log.Printf("Error renaming show %d: %v", id, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func DownloadSubtitlesHandler(w http.ResponseWriter, r *http.Request) {
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

	cfg := config.Load()

	if mediaType == "movie" {
		m, err := services.GetMovieByID(id)
		if err != nil {
			http.Error(w, "Movie not found", http.StatusNotFound)
			return
		}

		// If IMDB ID is missing, try to re-match it first
		if m.IMDBID == "" {
			log.Printf("[HANDLERS] IMDB ID missing for movie %s, attempting re-match...", m.Title)
			if err := services.MatchMovie(cfg, m.ID); err == nil {
				// Reload movie to get new IMDB ID
				m, _ = services.GetMovieByID(id)
			}
		}

		if m.IMDBID == "" {
			http.Error(w, "IMDB ID missing and re-match failed", http.StatusBadRequest)
			return
		}

		if err := services.DownloadSubtitlesForMovie(cfg, m.ID); err != nil {
			log.Printf("[HANDLERS] Manual subtitle download failed for %s: %v", m.Title, err)
			http.Error(w, "Download failed", http.StatusInternalServerError)
			return
		}
	} else if mediaType == "episode" {
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
			log.Printf("[HANDLERS] Error fetching episode %d for subtitle download: %v", id, err)
			http.Error(w, "Episode not found", http.StatusNotFound)
			return
		}

		// If IMDB ID is missing, try to re-match the parent show first
		if sh.IMDBID == "" {
			log.Printf("[HANDLERS] IMDB ID missing for show %s, attempting re-match...", sh.Title)
			if err := services.MatchShow(cfg, sh.ID); err == nil {
				// Reload IMDB ID
				database.DB.QueryRow("SELECT imdb_id FROM shows WHERE id = $1", sh.ID).Scan(&sh.IMDBID)
			}
		}

		if sh.IMDBID == "" {
			http.Error(w, "IMDB ID missing and re-match failed", http.StatusBadRequest)
			return
		}

		if err := services.DownloadSubtitlesForEpisode(cfg, e.ID); err != nil {
			log.Printf("[HANDLERS] Manual subtitle download failed for %s S%02dE%02d: %v", sh.Title, s.SeasonNumber, e.EpisodeNumber, err)
			http.Error(w, "Download failed", http.StatusInternalServerError)
			return
		}
	} else {
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

	user, err := GetCurrentUser(r)
	if err != nil || user == nil || !user.IsAdmin {
		http.Error(w, "Unauthorized: Admin only", http.StatusUnauthorized)
		return
	}

	log.Printf("[ADMIN] NUKE operation started by user: %s", user.Username)

	// Start a transaction for safety
	tx, err := database.DB.Begin()
	if err != nil {
		log.Printf("[ADMIN] Failed to start nuke transaction: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// Order matters if foreign keys aren't all cascading, but ours are mostly.
	// Deleting shows cascades to seasons and episodes.
	queries := []string{
		"DELETE FROM episodes",
		"DELETE FROM seasons",
		"DELETE FROM shows",
		"DELETE FROM movies",
		"DELETE FROM requests",
	}

	for _, q := range queries {
		if _, err := tx.Exec(q); err != nil {
			tx.Rollback()
			log.Printf("[ADMIN] Failed to execute query '%s': %v", q, err)
			http.Error(w, "Failed to clear table: "+q, http.StatusInternalServerError)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		log.Printf("[ADMIN] Failed to commit nuke transaction: %v", err)
		http.Error(w, "Failed to commit changes", http.StatusInternalServerError)
		return
	}

	log.Printf("[ADMIN] NUKE operation completed successfully")

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}
