package handlers

import (
	"Arrgo/config"
	"Arrgo/models"
	"Arrgo/services"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

var moviesTmpl *template.Template
var movieDetailsTmpl *template.Template

func init() {
	var err error
	funcMap := GetFuncMap()
	moviesTmpl, err = template.New("movies").Funcs(funcMap).ParseFiles(
		"templates/layouts/base.html",
		"templates/pages/movies.html",
		"templates/components/navigation.html",
	)
	if err != nil {
		log.Fatal("Failed to parse movies template:", err)
	}

	movieDetailsTmpl, err = template.New("movieDetails").Funcs(funcMap).ParseFiles(
		"templates/layouts/base.html",
		"templates/pages/movie_details.html",
		"templates/components/navigation.html",
	)
	if err != nil {
		log.Fatal("Failed to parse movie details template:", err)
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

func MovieDetailsHandler(w http.ResponseWriter, r *http.Request) {
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

	var movie *models.Movie
	idStr := r.URL.Query().Get("id")
	tmdbID := r.URL.Query().Get("tmdb_id")

	if idStr != "" {
		id, _ := strconv.Atoi(idStr)
		movie, err = services.GetMovieByID(id)
		if err != nil {
			http.Error(w, "Movie not found", http.StatusNotFound)
			return
		}
	} else if tmdbID != "" {
		// External search result
		cfg := config.Load()
		details, err := services.GetTMDBMovieDetails(cfg, tmdbID)
		if err != nil {
			log.Printf("Error getting TMDB movie details: %v", err)
			http.Error(w, "Movie details not found", http.StatusNotFound)
			return
		}

		year := 0
		if len(details.ReleaseDate) >= 4 {
			year, _ = strconv.Atoi(details.ReleaseDate[:4])
		}

		var genres []string
		for _, g := range details.Genres {
			genres = append(genres, g.Name)
		}

		movie = &models.Movie{
			Title:      details.Title,
			Year:       year,
			TMDBID:     fmt.Sprintf("%d", details.ID),
			Overview:   details.Overview,
			PosterPath: details.PosterPath,
			Genres:     strings.Join(genres, ", "),
			Status:     "search_result",
		}
		
		// Check library status for this tmdb_id
		status, _ := services.CheckLibraryStatus("movie", tmdbID)
		if status.Message != "" {
			movie.Status = status.Message
		}
	} else {
		http.Error(w, "Missing movie ID", http.StatusBadRequest)
		return
	}

	data := struct {
		Username string
		Movie    *models.Movie
	}{
		Username: user.Username,
		Movie:    movie,
	}

	if err := movieDetailsTmpl.ExecuteTemplate(w, "base", data); err != nil {
		log.Printf("Error executing movie details template: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
