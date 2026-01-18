package handlers

import (
	"Arrgo/config"
	"Arrgo/models"
	"Arrgo/services"
	"html/template"
	"log"
	"net/http"
	"strconv"
)

var dashboardTmpl *template.Template

func init() {
	var err error
	dashboardTmpl, err = template.ParseFiles(
		"templates/layouts/base.html",
		"templates/pages/dashboard.html",
		"templates/components/header.html",
		"templates/components/navigation.html",
	)
	if err != nil {
		log.Fatal("Failed to parse dashboard template:", err)
	}
}

type DashboardData struct {
	Username string
	Movies   []models.Movie
}

func DashboardHandler(w http.ResponseWriter, r *http.Request) {
	session, err := services.GetSession(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	userID, ok := session.Values["user_id"]
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	var userIDInt int64
	switch v := userID.(type) {
	case int64:
		userIDInt = v
	case int:
		userIDInt = int64(v)
	case string:
		var err error
		userIDInt, err = strconv.ParseInt(v, 10, 64)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
	default:
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	user, err := services.GetUserByID(userIDInt)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Load config and scan for movies (synchronously for now, but should be background)
	cfg := config.Load()
	if err := services.ScanMovies(cfg); err != nil {
		log.Printf("Warning: Failed to scan movies: %v", err)
	}

	movies, err := services.GetMovies()
	if err != nil {
		log.Printf("Error getting movies from DB: %v", err)
		movies = []models.Movie{}
	}

	data := DashboardData{
		Username: user.Username,
		Movies:   movies,
	}

	if err := dashboardTmpl.ExecuteTemplate(w, "base", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

