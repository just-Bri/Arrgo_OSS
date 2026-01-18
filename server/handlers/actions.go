package handlers

import (
	"Arrgo/config"
	"Arrgo/services"
	"log"
	"net/http"
	"strconv"
	"sync"
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
