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

var showsTmpl *template.Template

func init() {
	var err error
	funcMap := template.FuncMap{
		"hasPrefix": strings.HasPrefix,
	}
	showsTmpl, err = template.New("shows").Funcs(funcMap).ParseFiles(
		"templates/layouts/base.html",
		"templates/pages/shows.html",
		"templates/components/navigation.html",
	)
	if err != nil {
		log.Fatal("Failed to parse shows template:", err)
	}
}

type ShowsData struct {
	Username      string
	Shows         []models.Show
	AllGenres     []string
	SelectedGenre string
}

func ShowsHandler(w http.ResponseWriter, r *http.Request) {
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

	allShows, err := services.GetShows()
	if err != nil {
		log.Printf("Error getting shows: %v", err)
		allShows = []models.Show{}
	}

	selectedGenre := r.URL.Query().Get("genre")

	// Extract unique genres
	genreMap := make(map[string]bool)
	for _, s := range allShows {
		if s.Genres != "" {
			gs := strings.Split(s.Genres, ", ")
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

	// Filter shows
	var filteredShows []models.Show
	if selectedGenre != "" {
		for _, s := range allShows {
			if strings.Contains(s.Genres, selectedGenre) {
				filteredShows = append(filteredShows, s)
			}
		}
	} else {
		filteredShows = allShows
	}

	data := ShowsData{
		Username:      user.Username,
		Shows:         filteredShows,
		AllGenres:     allGenres,
		SelectedGenre: selectedGenre,
	}

	if err := showsTmpl.ExecuteTemplate(w, "base", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
