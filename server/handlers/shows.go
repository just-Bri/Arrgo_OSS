package handlers

import (
	"Arrgo/config"
	"Arrgo/models"
	"Arrgo/services"
	"context"
	"fmt"
	"html/template"
	"log"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

var showsTmpl *template.Template
var showDetailsTmpl *template.Template

func init() {
	var err error
	funcMap := GetFuncMap()
	showsTmpl, err = template.New("shows").Funcs(funcMap).ParseFiles(
		"templates/layouts/base.html",
		"templates/pages/shows.html",
		"templates/components/navigation.html",
	)
	if err != nil {
		log.Fatal("Failed to parse shows template:", err)
	}

	showDetailsTmpl, err = template.New("showDetails").Funcs(funcMap).ParseFiles(
		"templates/layouts/base.html",
		"templates/pages/show_details.html",
		"templates/components/navigation.html",
	)
	if err != nil {
		log.Fatal("Failed to parse show details template:", err)
	}
}

type ShowsData struct {
	Username       string
	IsAdmin        bool
	CurrentPage    string
	SearchQuery    string
	Shows          []models.Show
	IncomingShows  []models.Show
	AllGenres      []string
	SelectedGenre  string
	AllYears       []int
	SelectedYear   string
	AllStatuses    []string
	SelectedStatus string
}

func ShowsHandler(w http.ResponseWriter, r *http.Request) {
	user, err := GetCurrentUser(r)
	if err != nil || user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	allShows, err := services.GetShows()
	if err != nil {
		slog.Error("Error getting shows", "error", err)
		allShows = []models.Show{}
	}

	cfg := config.Load()
	selectedGenre := r.URL.Query().Get("genre")
	selectedYear := r.URL.Query().Get("year")
	selectedStatus := r.URL.Query().Get("status")

	// Separate incoming and library shows
	var allTorrents []services.TorrentStatus
	if user.IsAdmin {
		qb, err := services.NewQBittorrentClient(cfg)
		if err == nil {
			allTorrents, _ = qb.GetTorrentsDetailed(context.Background(), "")
		}
	}
	libraryShows, incomingShows := SeparateIncomingShows(allShows, cfg, user.IsAdmin, allTorrents)

	// Extract unique values for filters BEFORE filtering (so all options are available)
	allGenres := ExtractGenresFromShows(libraryShows)
	allYears := ExtractYearsFromShows(libraryShows)
	allStatuses := ExtractStatusesFromShows(libraryShows)

	// Apply filters to library shows
	var filtered []models.Show
	for _, s := range libraryShows {
		// Genre filter
		if selectedGenre != "" && !strings.Contains(s.Genres, selectedGenre) {
			continue
		}
		// Year filter
		if selectedYear != "" {
			year, _ := strconv.Atoi(selectedYear)
			if s.Year != year {
				continue
			}
		}
		// Status filter
		if selectedStatus != "" {
			if s.Status != selectedStatus {
				continue
			}
		}
		filtered = append(filtered, s)
	}
	libraryShows = filtered

	data := ShowsData{
		Username:       user.Username,
		IsAdmin:        user.IsAdmin,
		CurrentPage:    "/shows",
		SearchQuery:    "",
		Shows:          libraryShows,
		IncomingShows:  incomingShows,
		AllGenres:      allGenres,
		SelectedGenre:  selectedGenre,
		AllYears:       allYears,
		SelectedYear:   selectedYear,
		AllStatuses:    allStatuses,
		SelectedStatus: selectedStatus,
	}

	if err := showsTmpl.ExecuteTemplate(w, "base", data); err != nil {
		slog.Error("Error rendering shows template", "error", err)
		return
	}
}

func ShowDetailsHandler(w http.ResponseWriter, r *http.Request) {
	user, err := GetCurrentUser(r)
	if err != nil || user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	var show *models.Show
	var seasons []services.SeasonWithEpisodes
	var allEpisodes []services.TVDBEpisode // From TVDB for search results or missing check
	idStr := r.URL.Query().Get("id")
	tvdbID := r.URL.Query().Get("tvdb_id")

	cfg := config.Load()

	if idStr != "" {
		id, _ := strconv.Atoi(idStr)
		show, err = services.GetShowByID(id)
		if err != nil {
			http.Error(w, "Show not found", http.StatusNotFound)
			return
		}
		seasons, _ = services.GetShowSeasons(show.ID)

		// If we have a TVDB ID, fetch all episodes to show what's missing
		if show.TVDBID != "" {
			allEpisodes, _ = services.GetTVDBShowEpisodes(cfg, show.TVDBID)
		}
	} else if tvdbID != "" {
		// External search result
		details, err := services.GetTVDBShowDetails(cfg, tvdbID)
		if err != nil {
			slog.Error("Error getting TVDB show details", "error", err, "tvdb_id", tvdbID)
			http.Error(w, "Show details not found", http.StatusNotFound)
			return
		}

		year := 0
		if len(details.FirstAired) >= 4 {
			year, _ = strconv.Atoi(details.FirstAired[:4])
		}
		var genres []string
		for _, g := range details.Genres {
			genres = append(genres, g.Name)
		}

		show = &models.Show{
			Title:      details.Name,
			Year:       year,
			TVDBID:     fmt.Sprintf("%d", details.ID),
			Overview:   details.Overview,
			PosterPath: details.Image,
			Genres:     strings.Join(genres, ", "),
			Status:     "External",
		}

		// Fetch all episodes from TVDB
		allEpisodes, _ = services.GetTVDBShowEpisodes(cfg, tvdbID)

		// Check library status
		status, _ := services.CheckLibraryStatus("show", tvdbID)
		if status.Exists {
			show.ID = status.LocalID
			show.Status = "In Library"

			// Try to get full show info if it exists
			if localShow, err := services.GetShowByID(status.LocalID); err == nil {
				show = localShow
			}
			seasons, _ = services.GetShowSeasons(show.ID)
		} else if status.Message != "" {
			show.Status = status.Message
		}
	} else {
		http.Error(w, "Missing show ID", http.StatusBadRequest)
		return
	}

	// Check library status
	libStatus, _ := services.CheckLibraryStatus("show", show.TVDBID)

	// Prepare data for template
	type EnhancedSeason struct {
		SeasonNumber int
		Episodes     []struct {
			ID           int
			Number       int
			Title        string
			InLibrary    bool
			Quality      string
			Size         int64
			HasSubtitles bool
		}
	}

	var enhancedSeasons []EnhancedSeason
	if len(allEpisodes) > 0 {
		// Group by season
		seasonMap := make(map[int]*EnhancedSeason)
		for _, te := range allEpisodes {
			if te.SeasonNumber == 0 { // Skip specials for now or handle them?
				continue
			}
			if _, ok := seasonMap[te.SeasonNumber]; !ok {
				seasonMap[te.SeasonNumber] = &EnhancedSeason{SeasonNumber: te.SeasonNumber}
			}

			inLibrary := false
			quality := ""
			var size int64 = 0
			hasSubtitles := false
			localID := 0

			// Check if in local library
			for _, ls := range seasons {
				if ls.SeasonNumber == te.SeasonNumber {
					for _, le := range ls.Episodes {
						if le.EpisodeNumber == te.Number {
							inLibrary = true
							quality = le.Quality
							size = le.Size
							localID = le.ID
							if le.FilePath != "" {
								hasSubtitles = services.HasSubtitles(le.FilePath)
							}
							break
						}
					}
				}
			}

			seasonMap[te.SeasonNumber].Episodes = append(seasonMap[te.SeasonNumber].Episodes, struct {
				ID           int
				Number       int
				Title        string
				InLibrary    bool
				Quality      string
				Size         int64
				HasSubtitles bool
			}{
				ID:           localID,
				Number:       te.Number,
				Title:        te.Name,
				InLibrary:    inLibrary,
				Quality:      quality,
				Size:         size,
				HasSubtitles: hasSubtitles,
			})
		}

		// Sort seasons
		var keys []int
		for k := range seasonMap {
			keys = append(keys, k)
		}
		sort.Ints(keys)
		for _, k := range keys {
			enhancedSeasons = append(enhancedSeasons, *seasonMap[k])
		}
	} else {
		// Fallback to local only if TVDB fetch failed
		for _, s := range seasons {
			es := EnhancedSeason{SeasonNumber: s.SeasonNumber}
			for _, e := range s.Episodes {
				hasSubtitles := false
				if e.FilePath != "" {
					hasSubtitles = services.HasSubtitles(e.FilePath)
				}
				es.Episodes = append(es.Episodes, struct {
					ID           int
					Number       int
					Title        string
					InLibrary    bool
					Quality      string
					Size         int64
					HasSubtitles bool
				}{
					ID:           e.ID,
					Number:       e.EpisodeNumber,
					Title:        e.Title,
					InLibrary:    true,
					Quality:      e.Quality,
					Size:         e.Size,
					HasSubtitles: hasSubtitles,
				})
			}
			enhancedSeasons = append(enhancedSeasons, es)
		}
	}

	data := struct {
		Username      string
		IsAdmin       bool
		CurrentPage   string
		SearchQuery   string
		Show          *models.Show
		Seasons       []EnhancedSeason
		LibraryStatus services.LibraryStatus
	}{
		Username:      user.Username,
		IsAdmin:       user.IsAdmin,
		CurrentPage:   "/shows",
		SearchQuery:   "",
		Show:          show,
		Seasons:       enhancedSeasons,
		LibraryStatus: libStatus,
	}

	if err := showDetailsTmpl.ExecuteTemplate(w, "base", data); err != nil {
		slog.Error("Error executing show details template", "error", err, "show_id", show.ID)
		return
	}
}
