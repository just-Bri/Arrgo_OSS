package handlers

import (
	"Arrgo/config"
	"Arrgo/services"
	"log"
	"net/http"
	"strconv"
)

func ScanHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cfg := config.Load()
	log.Printf("Manual scan triggered")
	
	// Scan Movies
	if err := services.ScanMovies(cfg); err != nil {
		log.Printf("Error scanning movies: %v", err)
	}
	
	// Scan Shows
	if err := services.ScanShows(cfg); err != nil {
		log.Printf("Error scanning shows: %v", err)
	}

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
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

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func RenameShowHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := r.URL.Query().Get("id")
	_, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	// cfg := config.Load()
	
	// Fetch all episodes for this show and rename them
	// ... implementation pending ...
	
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}
