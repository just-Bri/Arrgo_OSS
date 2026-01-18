package handlers

import (
	"Arrgo/models"
	"Arrgo/services"
	"html/template"
	"log"
	"net/http"
	"sort"
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
	Username      string
	Movies        []models.Movie
	AllGenres     []string
	SelectedGenre string
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

	allMovies, err := services.GetMovies()
	if err != nil {
		log.Printf("Error getting movies: %v", err)
		allMovies = []models.Movie{}
	}

	selectedGenre := r.URL.Query().Get("genre")

	// Extract unique genres
	genreMap := make(map[string]bool)
	for _, m := range allMovies {
		if m.Genres != "" {
			gs := strings.Split(m.Genres, ", ")
			for _, g := range gs {
				if g != "" {
					genreMap[g] = true
				}
			}
		}
	}
	var allGenres []string
	for g := range genreMap {
		allGenres = append(allGenres, g)
	}
	sort.Strings(allGenres)

	// Filter movies
	var filteredMovies []models.Movie
	if selectedGenre != "" {
		for _, m := range allMovies {
			if strings.Contains(m.Genres, selectedGenre) {
				filteredMovies = append(filteredMovies, m)
			}
		}
	} else {
		filteredMovies = allMovies
	}

	data := MoviesData{
		Username:      user.Username,
		Movies:        filteredMovies,
		AllGenres:     allGenres,
		SelectedGenre: selectedGenre,
	}

	if err := moviesTmpl.ExecuteTemplate(w, "base", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
