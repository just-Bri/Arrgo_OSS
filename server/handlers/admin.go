package handlers

import (
	"Arrgo/config"
	"Arrgo/models"
	"Arrgo/services"
	"encoding/json"
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

type AdminPageData struct {
	Username       string
	IsAdmin        bool
	CurrentPage    string
	SearchQuery    string
	IncomingMovies []models.Movie
	IncomingShows  []models.Show

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

	// Get incoming movies and shows using shared helpers
	allMovies, err := services.GetMovies()
	if err != nil {
		slog.Error("Error getting movies for admin", "error", err)
		allMovies = []models.Movie{}
	}
	_, incomingMovies := SeparateIncomingMovies(allMovies, cfg, true)

	allShows, err := services.GetShows()
	if err != nil {
		slog.Error("Error getting shows for admin", "error", err)
		allShows = []models.Show{}
	}
	_, incomingShows := SeparateIncomingShows(allShows, cfg, true)

	data := AdminPageData{
		Username:       user.Username,
		IsAdmin:        user.IsAdmin,
		CurrentPage:    "/admin",
		SearchQuery:    "",
		IncomingMovies: incomingMovies,
		IncomingShows:  incomingShows,

		ScanningIncomingMovies: services.IsScanning(services.ScanIncomingMovies),
		ScanningIncomingShows:  services.IsScanning(services.ScanIncomingShows),
		ScanningMovieLibrary:   services.IsScanning(services.ScanMovieLibrary),
		ScanningShowLibrary:    services.IsScanning(services.ScanShowLibrary),
	}

	if err := adminTmpl.ExecuteTemplate(w, "base", data); err != nil {
		slog.Error("Error rendering admin template", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
