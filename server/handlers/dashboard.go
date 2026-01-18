package handlers

import (
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
	Username    string
	SearchQuery string
	MovieCount  int
	ShowCount   int
}

func DashboardHandler(w http.ResponseWriter, r *http.Request) {
	user, err := GetCurrentUser(r)
	if err != nil || user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	log.Printf("[DASHBOARD] Loading dashboard for user: %s", user.Username)

	movieCount, err := services.GetMovieCount()
	if err != nil {
		log.Printf("Error getting movie count: %v", err)
	}

	showCount, err := services.GetShowCount()
	if err != nil {
		log.Printf("Error getting show count: %v", err)
	}

	data := DashboardData{
		Username:    user.Username,
		SearchQuery: "",
		MovieCount:  movieCount,
		ShowCount:   showCount,
	}

	if err := dashboardTmpl.ExecuteTemplate(w, "base", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
