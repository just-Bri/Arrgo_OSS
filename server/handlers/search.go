package handlers

import (
	"Arrgo/config"
	"Arrgo/services"
	"html/template"
	"log"
	"log/slog"
	"net/http"
	"strings"
)

var searchTmpl *template.Template

func init() {
	var err error
	funcMap := GetFuncMap()
	searchTmpl, err = template.New("search").Funcs(funcMap).ParseFiles(
		"templates/layouts/base.html",
		"templates/pages/search.html",
		"templates/components/navigation.html",
	)
	if err != nil {
		log.Fatal("Failed to parse search template:", err)
	}
}

type UnifiedSearchResult struct {
	ID            string                 `json:"id"`
	Title         string                 `json:"title"`
	Year          int                    `json:"year"`
	MediaType     string                 `json:"media_type"`
	PosterPath    string                 `json:"poster_path"`
	Overview      string                 `json:"overview"`
	Source        string                 `json:"source"` // "local" or "external"
	LibraryStatus services.LibraryStatus `json:"library_status"`
	LocalID       int                    `json:"local_id,omitempty"`
}

type SearchPageData struct {
	Username    string
	IsAdmin     bool
	CurrentPage string
	SearchQuery string
	Movies      []UnifiedSearchResult
	Shows       []UnifiedSearchResult
}

func SearchHandler(w http.ResponseWriter, r *http.Request) {
	user, err := GetCurrentUser(r)
	if err != nil || user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	query := r.URL.Query().Get("q")
	var movies []UnifiedSearchResult
	var shows []UnifiedSearchResult

	if query != "" {
		cfg := config.Load()

		// 1. Search Local Movies
		localMovies, _ := services.SearchMoviesLocal(query)
		for _, m := range localMovies {
			// Skip items still in the incoming folder
			if strings.HasPrefix(m.Path, cfg.IncomingMoviesPath) {
				continue
			}

			movies = append(movies, UnifiedSearchResult{
				ID:         m.TMDBID,
				Title:      m.Title,
				Year:       m.Year,
				MediaType:  "movie",
				PosterPath: m.PosterPath,
				Overview:   m.Overview,
				Source:     "local",
				LocalID:    m.ID,
				LibraryStatus: services.LibraryStatus{
					Exists:  true,
					Message: "In Library",
				},
			})
		}

		// 2. Search Local Shows
		localShows, _ := services.SearchShowsLocal(query)
		for _, s := range localShows {
			// Skip items still in the incoming folder
			if strings.HasPrefix(s.Path, cfg.IncomingShowsPath) {
				continue
			}

			shows = append(shows, UnifiedSearchResult{
				ID:         s.TVDBID,
				Title:      s.Title,
				Year:       s.Year,
				MediaType:  "show",
				PosterPath: s.PosterPath,
				Overview:   s.Overview,
				Source:     "local",
				LocalID:    s.ID,
				LibraryStatus: services.LibraryStatus{
					Exists:  true,
					Message: "In Library",
				},
			})
		}

		// 3. Search External (TMDB/TVDB)
		movieResults, _ := services.SearchTMDB(cfg, query)
		for _, res := range movieResults {
			// Skip if already in movies from local search
			existsLocally := false
			for _, lr := range movies {
				if lr.ID == res.ID {
					existsLocally = true
					break
				}
			}
			if existsLocally {
				continue
			}

			status, _ := services.CheckLibraryStatus("movie", res.ID)
			source := "external"
			localID := 0
			if status.Exists {
				source = "local"
				localID = status.LocalID
			}

			movies = append(movies, UnifiedSearchResult{
				ID:            res.ID,
				Title:         res.Title,
				Year:          res.Year,
				MediaType:     "movie",
				PosterPath:    res.PosterPath,
				Overview:      res.Overview,
				Source:        source,
				LocalID:       localID,
				LibraryStatus: status,
			})
		}

		showResults, _ := services.SearchTVDB(cfg, query)
		for _, res := range showResults {
			// Skip if already in shows from local search
			existsLocally := false
			for _, lr := range shows {
				if lr.ID == res.ID {
					existsLocally = true
					break
				}
			}
			if existsLocally {
				continue
			}

			status, _ := services.CheckLibraryStatus("show", res.ID)
			source := "external"
			localID := 0
			if status.Exists {
				source = "local"
				localID = status.LocalID
			}

			shows = append(shows, UnifiedSearchResult{
				ID:            res.ID,
				Title:         res.Title,
				Year:          res.Year,
				MediaType:     "show",
				PosterPath:    res.PosterPath,
				Overview:      res.Overview,
				Source:        source,
				LocalID:       localID,
				LibraryStatus: status,
			})
		}
	}

	data := SearchPageData{
		Username:    user.Username,
		IsAdmin:     user.IsAdmin,
		CurrentPage: "/search",
		SearchQuery: query,
		Movies:      movies,
		Shows:       shows,
	}

	if err := searchTmpl.ExecuteTemplate(w, "base", data); err != nil {
		slog.Error("Error rendering search template", "error", err, "query", query)
		return
	}
}
