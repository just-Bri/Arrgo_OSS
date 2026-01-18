package handlers

import (
	"Arrgo/models"
	"Arrgo/services"
	"html/template"
	"log"
	"net/http"
	"strings"
)

var moviesTmpl *template.Template

func init() {
	var err error
	funcMap := template.FuncMap{
		"hasPrefix": strings.HasPrefix,
	}
	moviesTmpl, err = template.New("movies").Funcs(funcMap).ParseFiles(
		"templates/layouts/base.html",
		"templates/pages/movies.html",
		"templates/components/navigation.html",
	)
	if err != nil {
		log.Fatal("Failed to parse movies template:", err)
	}
}

type MoviesData struct {
	Username string
	Movies   []models.Movie
}

func MoviesHandler(w http.ResponseWriter, r *http.Request) {
	session, err := services.GetSession(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	userID := session.Values["user_id"]
	if userID == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	user, err := services.GetUserByID(interfaceToInt64(userID))
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	movies, err := services.GetMovies()
	if err != nil {
		log.Printf("Error getting movies: %v", err)
		movies = []models.Movie{}
	}

	data := MoviesData{
		Username: user.Username,
		Movies:   movies,
	}

	if err := moviesTmpl.ExecuteTemplate(w, "base", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func interfaceToInt64(v interface{}) int64 {
	switch val := v.(type) {
	case int64:
		return val
	case int:
		return int64(val)
	default:
		return 0
	}
}
