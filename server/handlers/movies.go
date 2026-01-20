package handlers

import (
	"Arrgo/config"
	"Arrgo/models"
	"Arrgo/services"
	"fmt"
	"html/template"
	"log"
	"log/slog"
	"net/http"
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
	Username        string
	IsAdmin         bool
	CurrentPage     string
	SearchQuery     string
	Movies          []models.Movie
	IncomingMovies  []models.Movie
	AllGenres       []string
	SelectedGenre   string
	AllYears        []int
	SelectedYear    string
	AllQualities    []string
	SelectedQuality string
	AllStatuses     []string
	SelectedStatus  string
	AllSizes        []string
	SelectedSize    string
}

func MoviesHandler(w http.ResponseWriter, r *http.Request) {
	user, err := GetCurrentUser(r)
	if err != nil || user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	allMovies, err := services.GetMovies()
	if err != nil {
		slog.Error("Error getting movies", "error", err)
		allMovies = []models.Movie{}
	}

	cfg := config.Load()
	selectedGenre := r.URL.Query().Get("genre")
	selectedYear := r.URL.Query().Get("year")
	selectedQuality := r.URL.Query().Get("quality")
	selectedStatus := r.URL.Query().Get("status")
	selectedSize := r.URL.Query().Get("size")

	// Separate incoming and library movies
	libraryMovies, incomingMovies := SeparateIncomingMovies(allMovies, cfg, user.IsAdmin)

	// Extract unique values for filters BEFORE filtering (so all options are available)
	allGenres := ExtractGenresFromMovies(libraryMovies)
	allYears := ExtractYearsFromMovies(libraryMovies)
	allQualities := ExtractQualitiesFromMovies(libraryMovies)
	allStatuses := ExtractStatusesFromMovies(libraryMovies)

	// Apply filters to library movies
	var filtered []models.Movie
	for _, m := range libraryMovies {
		// Genre filter
		if selectedGenre != "" && !strings.Contains(m.Genres, selectedGenre) {
			continue
		}
		// Year filter
		if selectedYear != "" {
			year, _ := strconv.Atoi(selectedYear)
			if m.Year != year {
				continue
			}
		}
		// Quality filter
		if selectedQuality != "" {
			if m.Quality != selectedQuality {
				continue
			}
		}
		// Status filter
		if selectedStatus != "" {
			if m.Status != selectedStatus {
				continue
			}
		}
		// Size filter
		if selectedSize != "" {
			var minSize, maxSize int64
			switch selectedSize {
			case "small":
				maxSize = 5 * 1024 * 1024 * 1024 // 5GB
			case "medium":
				minSize = 5 * 1024 * 1024 * 1024
				maxSize = 10 * 1024 * 1024 * 1024 // 10GB
			case "large":
				minSize = 10 * 1024 * 1024 * 1024
			}
			if m.Size < minSize || (maxSize > 0 && m.Size > maxSize) {
				continue
			}
		}
		filtered = append(filtered, m)
	}
	libraryMovies = filtered

	data := MoviesData{
		Username:        user.Username,
		IsAdmin:         user.IsAdmin,
		CurrentPage:     "/movies",
		SearchQuery:     "",
		Movies:          libraryMovies,
		IncomingMovies:  incomingMovies,
		AllGenres:       allGenres,
		SelectedGenre:   selectedGenre,
		AllYears:        allYears,
		SelectedYear:    selectedYear,
		AllQualities:    allQualities,
		SelectedQuality: selectedQuality,
		AllStatuses:     allStatuses,
		SelectedStatus:  selectedStatus,
		AllSizes:        []string{"small", "medium", "large"},
		SelectedSize:    selectedSize,
	}

	if err := moviesTmpl.ExecuteTemplate(w, "base", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func MovieDetailsHandler(w http.ResponseWriter, r *http.Request) {
	user, err := GetCurrentUser(r)
	if err != nil || user == nil {
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
			slog.Error("Error getting TMDB movie details", "error", err, "tmdb_id", tmdbID)
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
			Status:     "External",
		}

		// Check library status for this tmdb_id
		status, _ := services.CheckLibraryStatus("movie", tmdbID)
		if status.Exists {
			movie.ID = status.LocalID
			movie.Status = "In Library"

			// Try to get full movie info if it exists
			if localMovie, err := services.GetMovieByID(status.LocalID); err == nil {
				movie = localMovie
			}
		} else if status.Message != "" {
			movie.Status = status.Message
		}
	} else {
		http.Error(w, "Missing movie ID", http.StatusBadRequest)
		return
	}

	// Check library status
	libStatus, _ := services.CheckLibraryStatus("movie", movie.TMDBID)

	data := struct {
		Username      string
		IsAdmin       bool
		CurrentPage   string
		SearchQuery   string
		Movie         *models.Movie
		HasSubtitles  bool
		LibraryStatus services.LibraryStatus
	}{
		Username:      user.Username,
		IsAdmin:       user.IsAdmin,
		CurrentPage:   "/movies",
		SearchQuery:   "",
		Movie:         movie,
		HasSubtitles:  services.HasSubtitles(movie.Path),
		LibraryStatus: libStatus,
	}

	if err := movieDetailsTmpl.ExecuteTemplate(w, "base", data); err != nil {
		slog.Error("Error executing movie details template", "error", err, "movie_id", movie.ID)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
