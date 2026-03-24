package handlers

import (
	"Arrgo/config"
	"Arrgo/services"
	"encoding/json"
	"log/slog"
	"net/http"
)

// DeduplicateMoviesHandler handles the API request to deduplicate movies
func DeduplicateMoviesHandler(w http.ResponseWriter, r *http.Request) {
	user, err := GetCurrentUser(r)
	if err != nil || user == nil || !user.IsAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cfg := config.Load()
	result, err := services.DeduplicateMovies(cfg)
	if err != nil {
		slog.Error("Error deduplicating movies", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// DeduplicateShowsHandler handles the API request to deduplicate shows
func DeduplicateShowsHandler(w http.ResponseWriter, r *http.Request) {
	user, err := GetCurrentUser(r)
	if err != nil || user == nil || !user.IsAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cfg := config.Load()
	result, err := services.DeduplicateShows(cfg)
	if err != nil {
		slog.Error("Error deduplicating shows", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
