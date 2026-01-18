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
	Username            string
	IsAdmin             bool
	SearchQuery         string
	MovieCount          int
	IncomingMovieCount  int
	ShowCount           int
	IncomingShowCount   int
}

func DashboardHandler(w http.ResponseWriter, r *http.Request) {
	user, err := GetCurrentUser(r)
	if err != nil || user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	log.Printf("[DASHBOARD] Loading dashboard for user: %s", user.Username)

	cfg := config.Load()
	
	// Library counts (excluding incoming)
	movieCount, err := services.GetMovieCount(cfg.IncomingPath)
	if err != nil {
		log.Printf("Error getting movie count: %v", err)
	}

	showCount, err := services.GetShowCount(cfg.IncomingPath)
	if err != nil {
		log.Printf("Error getting show count: %v", err)
	}

	incomingMovieCount := 0
	incomingShowCount := 0

	if user.IsAdmin {
		// Total counts (including incoming)
		totalMovies, _ := services.GetMovieCount("")
		totalShows, _ := services.GetShowCount("")
		
		incomingMovieCount = totalMovies - movieCount
		incomingShowCount = totalShows - showCount
	}

	data := DashboardData{
		Username:            user.Username,
		IsAdmin:             user.IsAdmin,
		SearchQuery:         "",
		MovieCount:          movieCount,
		IncomingMovieCount:  incomingMovieCount,
		ShowCount:           showCount,
		IncomingShowCount:   incomingShowCount,
	}

	if err := dashboardTmpl.ExecuteTemplate(w, "base", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
