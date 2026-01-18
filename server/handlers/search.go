package handlers

import (
	"Arrgo/config"
	"Arrgo/services"
	"html/template"
	"log"
	"net/http"
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
	SearchQuery string
	Results     []UnifiedSearchResult
}

func SearchHandler(w http.ResponseWriter, r *http.Request) {
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

	query := r.URL.Query().Get("q")
	var results []UnifiedSearchResult

	if query != "" {
		cfg := config.Load()

		// 1. Search Local Movies
		localMovies, _ := services.SearchMoviesLocal(query)
		for _, m := range localMovies {
			results = append(results, UnifiedSearchResult{
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
			results = append(results, UnifiedSearchResult{
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
			// Skip if already in results from local search
			existsLocally := false
			for _, lr := range results {
				if lr.MediaType == "movie" && lr.ID == res.ID {
					existsLocally = true
					break
				}
			}
			if existsLocally {
				continue
			}

			status, _ := services.CheckLibraryStatus("movie", res.ID)
			results = append(results, UnifiedSearchResult{
				ID:            res.ID,
				Title:         res.Title,
				Year:          res.Year,
				MediaType:     "movie",
				PosterPath:    res.PosterPath,
				Overview:      res.Overview,
				Source:        "external",
				LibraryStatus: status,
			})
		}

		showResults, _ := services.SearchTVDB(cfg, query)
		for _, res := range showResults {
			// Skip if already in results from local search
			existsLocally := false
			for _, lr := range results {
				if lr.MediaType == "show" && lr.ID == res.ID {
					existsLocally = true
					break
				}
			}
			if existsLocally {
				continue
			}

			status, _ := services.CheckLibraryStatus("show", res.ID)
			results = append(results, UnifiedSearchResult{
				ID:            res.ID,
				Title:         res.Title,
				Year:          res.Year,
				MediaType:     "show",
				PosterPath:    res.PosterPath,
				Overview:      res.Overview,
				Source:        "external",
				LibraryStatus: status,
			})
		}
	}

	data := SearchPageData{
		Username:    user.Username,
		IsAdmin:     user.IsAdmin,
		SearchQuery: query,
		Results:     results,
	}

	if err := searchTmpl.ExecuteTemplate(w, "base", data); err != nil {
		log.Printf("Error rendering search template: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
