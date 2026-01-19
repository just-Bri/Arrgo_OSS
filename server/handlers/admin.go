package handlers

import (
	"Arrgo/config"
	"Arrgo/models"
	"Arrgo/services"
	"html/template"
	"log"
	"net/http"
	"strings"
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
}

func AdminHandler(w http.ResponseWriter, r *http.Request) {
	user, err := GetCurrentUser(r)
	if err != nil || user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if !user.IsAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	cfg := config.Load()

	// Get incoming movies
	allMovies, err := services.GetMovies()
	if err != nil {
		log.Printf("Error getting movies for admin: %v", err)
	}
	var incomingMovies []models.Movie
	for _, m := range allMovies {
		if strings.HasPrefix(m.Path, cfg.IncomingMoviesPath) {
			incomingMovies = append(incomingMovies, m)
		}
	}

	// Get incoming shows
	allShows, err := services.GetShows()
	if err != nil {
		log.Printf("Error getting shows for admin: %v", err)
	}
	var incomingShows []models.Show
	for _, s := range allShows {
		if strings.HasPrefix(s.Path, cfg.IncomingShowsPath) {
			incomingShows = append(incomingShows, s)
		}
	}

	data := AdminPageData{
		Username:       user.Username,
		IsAdmin:        user.IsAdmin,
		CurrentPage:    "/admin",
		SearchQuery:    "",
		IncomingMovies: incomingMovies,
		IncomingShows:  incomingShows,
	}

	if err := adminTmpl.ExecuteTemplate(w, "base", data); err != nil {
		log.Printf("Error rendering admin template: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
