package handlers

import (
	"Arrgo/config"
	"Arrgo/services"
	"html/template"
	"log"
	"net/http"
	"strings"
)

var dashboardTmpl *template.Template

func init() {
	var err error
	funcMap := template.FuncMap{
		"hasPrefix": strings.HasPrefix,
	}
	dashboardTmpl, err = template.New("dashboard").Funcs(funcMap).ParseFiles(
		"templates/layouts/base.html",
		"templates/pages/dashboard.html",
		"templates/components/navigation.html",
	)
	if err != nil {
		log.Fatal("Failed to parse dashboard template:", err)
	}
}

type DashboardData struct {
	Username           string
	IsAdmin            bool
	CurrentPage        string
	SearchQuery        string
	MovieCount         int
	IncomingMovieCount int
	MovieRequestCount  int
	ShowCount          int
	IncomingShowCount  int
	ShowRequestCount   int
}

func DashboardHandler(w http.ResponseWriter, r *http.Request) {
	user, err := GetCurrentUser(r)
	if err != nil || user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	log.Printf("[DASHBOARD] Loading dashboard for user: %s", user.Username)

	cfg := config.Load()

	// Library counts (excluding incoming subfolders)
	movieCount, err := services.GetMovieCount(cfg.IncomingMoviesPath)
	if err != nil {
		log.Printf("Error getting movie count: %v", err)
	}

	showCount, err := services.GetShowCount(cfg.IncomingTVPath)
	if err != nil {
		log.Printf("Error getting show count: %v", err)
	}

	incomingMovieCount := 0
	incomingShowCount := 0
	movieRequestCount := 0
	showRequestCount := 0

	if user.IsAdmin {
		// Total counts (including incoming)
		totalMovies, _ := services.GetMovieCount("")
		totalShows, _ := services.GetShowCount("")

		incomingMovieCount = totalMovies - movieCount
		incomingShowCount = totalShows - showCount

		// Pending requests
		movieRequestCount, showRequestCount, _ = services.GetPendingRequestCounts()
	}

	data := DashboardData{
		Username:           user.Username,
		IsAdmin:            user.IsAdmin,
		CurrentPage:        "/dashboard",
		SearchQuery:        "",
		MovieCount:         movieCount,
		IncomingMovieCount: incomingMovieCount,
		MovieRequestCount:  movieRequestCount,
		ShowCount:          showCount,
		IncomingShowCount:  incomingShowCount,
		ShowRequestCount:   showRequestCount,
	}

	if err := dashboardTmpl.ExecuteTemplate(w, "base", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
