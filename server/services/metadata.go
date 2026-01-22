package services

import (
	"Arrgo/config"
	"Arrgo/database"
	"Arrgo/models"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	sharedhttp "github.com/justbri/arrgo/shared/http"
)

var (
	tvdbToken       string
	tvdbTokenExpiry time.Time
	tvdbMutex       sync.Mutex

	// Rate limiting for TMDB/TVDB
	lastRequestTime time.Time
	rateLimitMutex  sync.Mutex

	tmdbGenres = map[int]string{
		28:    "Action",
		12:    "Adventure",
		16:    "Animation",
		35:    "Comedy",
		80:    "Crime",
		99:    "Documentary",
		18:    "Drama",
		10751: "Family",
		14:    "Fantasy",
		36:    "History",
		27:    "Horror",
		10402: "Music",
		9648:  "Mystery",
		10749: "Romance",
		878:   "Science Fiction",
		10770: "TV Movie",
		53:    "Thriller",
		10752: "War",
		37:    "Western",
	}
)

type TMDBMovieDetails struct {
	ID          int     `json:"id"`
	IMDBID      string  `json:"imdb_id"`
	Title       string  `json:"title"`
	ReleaseDate string  `json:"release_date"`
	Overview    string  `json:"overview"`
	PosterPath  string  `json:"poster_path"`
	VoteAverage float64 `json:"vote_average"`
	Genres      []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"genres"`
	Runtime int `json:"runtime"`
}

type TVDBShowDetails struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Overview   string `json:"overview"`
	Image      string `json:"image"`
	FirstAired string `json:"firstAired"`
	LastAired  string `json:"lastAired"`
	Status     struct {
		Name string `json:"name"`
	} `json:"status"`
	Genres []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"genres"`
	Seasons []struct {
		ID     int `json:"id"`
		Number int `json:"number"`
		Type   struct {
			Name string `json:"name"`
		} `json:"type"`
	} `json:"seasons"`
	RemoteIDs []struct {
		ID   string `json:"id"`
		Type int    `json:"type"`
	} `json:"remoteIds"`
}

type TVDBSeasonEpisodesResponse struct {
	Data struct {
		Episodes []struct {
			ID           int    `json:"id"`
			Name         string `json:"name"`
			SeasonNumber int    `json:"seasonNumber"`
			Number       int    `json:"number"`
			Overview     string `json:"overview"`
			Aired        string `json:"aired"`
		} `json:"episodes"`
	} `json:"data"`
}

func GetTMDBMovieDetails(cfg *config.Config, tmdbID string) (*TMDBMovieDetails, error) {
	if cfg.TMDBAPIKey == "" {
		return nil, fmt.Errorf("TMDB_API_KEY is not set")
	}

	throttle()
	apiURL := fmt.Sprintf("https://api.themoviedb.org/3/movie/%s?api_key=%s&language=en-US", tmdbID, cfg.TMDBAPIKey)

	resp, err := sharedhttp.MakeRequest(context.Background(), apiURL, sharedhttp.LongTimeoutClient)
	if err != nil {
		return nil, err
	}

	var details TMDBMovieDetails
	if err := sharedhttp.DecodeJSONResponse(resp, &details); err != nil {
		return nil, err
	}

	return &details, nil
}

func GetTVDBShowDetails(cfg *config.Config, tvdbID string) (*TVDBShowDetails, error) {
	if cfg.TVDBAPIKey == "" {
		return nil, fmt.Errorf("TVDB_API_KEY is not set")
	}

	token, err := getTVDBToken(cfg.TVDBAPIKey)
	if err != nil {
		return nil, err
	}

	throttle()
	url := fmt.Sprintf("https://api4.thetvdb.com/v4/series/%s/extended?meta=translations&short=false", tvdbID)

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept-Language", "eng")

	resp, err := sharedhttp.LongTimeoutClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("TVDB returned status %d", resp.StatusCode)
	}

	var result struct {
		Data TVDBShowDetails `json:"data"`
	}
	if err := sharedhttp.DecodeJSONResponse(resp, &result); err != nil {
		return nil, err
	}

	return &result.Data, nil
}

type TVDBEpisode struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	SeasonNumber int    `json:"seasonNumber"`
	Number       int    `json:"number"`
	Overview     string `json:"overview"`
	Aired        string `json:"aired"`
}

func GetTVDBShowEpisodes(cfg *config.Config, tvdbID string) ([]TVDBEpisode, error) {
	if cfg.TVDBAPIKey == "" {
		return nil, fmt.Errorf("TVDB_API_KEY is not set")
	}

	token, err := getTVDBToken(cfg.TVDBAPIKey)
	if err != nil {
		return nil, err
	}

	throttle()
	// Using default translation to get episode names in English
	url := fmt.Sprintf("https://api4.thetvdb.com/v4/series/%s/episodes/default/eng", tvdbID)

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := sharedhttp.LongTimeoutClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("TVDB returned status %d", resp.StatusCode)
	}

	var result struct {
		Data struct {
			Episodes []TVDBEpisode `json:"episodes"`
		} `json:"data"`
	}
	if err := sharedhttp.DecodeJSONResponse(resp, &result); err != nil {
		return nil, err
	}

	return result.Data.Episodes, nil
}

func throttle() {
	rateLimitMutex.Lock()
	defer rateLimitMutex.Unlock()

	elapsed := time.Since(lastRequestTime)
	if elapsed < 200*time.Millisecond {
		time.Sleep(200*time.Millisecond - elapsed)
	}
	lastRequestTime = time.Now()
}

type TMDBMovieSearchResponse struct {
	Results []struct {
		ID          int     `json:"id"`
		Title       string  `json:"title"`
		ReleaseDate string  `json:"release_date"`
		Overview    string  `json:"overview"`
		PosterPath  string  `json:"poster_path"`
		VoteAverage float64 `json:"vote_average"`
		GenreIDs    []int   `json:"genre_ids"`
	} `json:"results"`
}

type TVDBAuthResponse struct {
	Data struct {
		Token string `json:"token"`
	} `json:"data"`
	Status string `json:"status"`
}

type TVDBSearchResponse struct {
	Data []struct {
		TVDBID          string   `json:"tvdb_id"`
		Name            string   `json:"name"`
		Overview        string   `json:"overview"`
		ImageURL        string   `json:"image_url"`
		Year            string   `json:"year"`
		PrimaryLanguage string   `json:"primary_language"`
		Genres          []string `json:"genres"`
	} `json:"data"`
	Status string `json:"status"`
}

type SearchResult struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Year       int      `json:"year"`
	MediaType  string   `json:"media_type"`
	PosterPath string   `json:"poster_path"`
	Overview   string   `json:"overview"`
	Genres     []string `json:"genres"`
}

func SearchTMDB(cfg *config.Config, query string) ([]SearchResult, error) {
	if cfg.TMDBAPIKey == "" {
		return nil, fmt.Errorf("TMDB_API_KEY is not set")
	}

	throttle()
	searchURL := sharedhttp.BuildQueryURL("https://api.themoviedb.org/3/search/movie", map[string]string{
		"api_key":  cfg.TMDBAPIKey,
		"query":    query,
		"language": "en-US",
	})

	resp, err := sharedhttp.MakeRequest(context.Background(), searchURL, sharedhttp.LongTimeoutClient)
	if err != nil {
		return nil, err
	}

	var searchResults TMDBMovieSearchResponse
	if err := sharedhttp.DecodeJSONResponse(resp, &searchResults); err != nil {
		return nil, err
	}

	results := make([]SearchResult, 0, len(searchResults.Results))
	for _, r := range searchResults.Results {
		year := 0
		if len(r.ReleaseDate) >= 4 {
			year, _ = strconv.Atoi(r.ReleaseDate[:4])
		}

		var genres []string
		for _, id := range r.GenreIDs {
			if name, ok := tmdbGenres[id]; ok {
				genres = append(genres, name)
			}
		}

		results = append(results, SearchResult{
			ID:         fmt.Sprintf("%d", r.ID),
			Title:      r.Title,
			Year:       year,
			MediaType:  "movie",
			PosterPath: r.PosterPath,
			Overview:   r.Overview,
			Genres:     genres,
		})
	}

	return results, nil
}

func SearchTVDB(cfg *config.Config, query string) ([]SearchResult, error) {
	if cfg.TVDBAPIKey == "" {
		return nil, fmt.Errorf("TVDB_API_KEY is not set")
	}

	token, err := getTVDBToken(cfg.TVDBAPIKey)
	if err != nil {
		return nil, err
	}

	throttle()
	searchURL := sharedhttp.BuildQueryURL("https://api4.thetvdb.com/v4/search", map[string]string{
		"query": query,
		"type":  "series",
		"lang":  "eng",
	})

	req, _ := http.NewRequest("GET", searchURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := sharedhttp.LongTimeoutClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("TVDB returned status %d", resp.StatusCode)
	}

	var searchResults TVDBSearchResponse
	if err := sharedhttp.DecodeJSONResponse(resp, &searchResults); err != nil {
		return nil, err
	}

	results := make([]SearchResult, 0, len(searchResults.Data))
	for _, r := range searchResults.Data {
		year, _ := strconv.Atoi(r.Year)
		results = append(results, SearchResult{
			ID:         r.TVDBID,
			Title:      r.Name,
			Year:       year,
			MediaType:  "show",
			PosterPath: r.ImageURL,
			Overview:   r.Overview,
			Genres:     r.Genres,
		})
	}

	return results, nil
}

func getTVDBToken(apiKey string) (string, error) {
	tvdbMutex.Lock()
	defer tvdbMutex.Unlock()

	if tvdbToken != "" && time.Now().Before(tvdbTokenExpiry) {
		return tvdbToken, nil
	}

	slog.Info("Authenticating with TVDB")
	payload, _ := json.Marshal(map[string]string{"apikey": apiKey})
	req, _ := http.NewRequest("POST", "https://api4.thetvdb.com/v4/login", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	resp, err := sharedhttp.LongTimeoutClient.Do(req)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return "", fmt.Errorf("TVDB authentication returned status %d", resp.StatusCode)
	}

	var auth TVDBAuthResponse
	if err := sharedhttp.DecodeJSONResponse(resp, &auth); err != nil {
		return "", err
	}

	if auth.Data.Token == "" {
		return "", fmt.Errorf("failed to get token from TVDB: %s", auth.Status)
	}

	tvdbToken = auth.Data.Token
	tvdbTokenExpiry = time.Now().Add(23 * time.Hour) // Tokens usually last 24h
	return tvdbToken, nil
}

func MatchMovie(cfg *config.Config, movieID int) error {
	// 1. Fetch movie from DB
	var m models.Movie
	var tmdbID, imdbID sql.NullString
	query := `SELECT id, title, year, tmdb_id, imdb_id FROM movies WHERE id = $1`
	err := database.DB.QueryRow(query, movieID).Scan(&m.ID, &m.Title, &m.Year, &tmdbID, &imdbID)
	if err != nil {
		slog.Error("Error fetching movie from DB", "movie_id", movieID, "error", err)
		return err
	}
	m.TMDBID = tmdbID.String
	m.IMDBID = imdbID.String

	// 1.5 Check if we already have metadata for this movie title/year in the DB
	var existingTMDBID, existingIMDBID, existingOverview, existingPosterPath, existingGenres sql.NullString
	var existingRawMetadata []byte
	checkQuery := `SELECT tmdb_id, imdb_id, overview, poster_path, genres, raw_metadata FROM movies WHERE title = $1 AND year = $2 AND status = 'matched' AND tmdb_id IS NOT NULL AND tmdb_id != '' LIMIT 1`
	err = database.DB.QueryRow(checkQuery, m.Title, m.Year).Scan(&existingTMDBID, &existingIMDBID, &existingOverview, &existingPosterPath, &existingGenres, &existingRawMetadata)
	if err == nil {
		slog.Info("Found existing metadata in DB, reusing", "title", m.Title, "year", m.Year)
		updateQuery := `
			UPDATE movies
			SET tmdb_id = $1, imdb_id = $2, overview = $3, poster_path = $4, genres = $5, status = 'matched', raw_metadata = $6, updated_at = CURRENT_TIMESTAMP
			WHERE id = $7
		`
		_, err = database.DB.Exec(updateQuery, existingTMDBID, existingIMDBID, existingOverview, existingPosterPath, existingGenres, existingRawMetadata, m.ID)
		return err
	}

	if cfg.TMDBAPIKey == "" {
		return fmt.Errorf("TMDB_API_KEY is not set")
	}

	var matchedTMDBID = m.TMDBID

	// 2. Search TMDB if ID not provided
	if matchedTMDBID == "" {
		// Clean the title before searching to remove quality tags and other metadata
		cleanedTitle := cleanTitleTags(m.Title)
		slog.Info("Searching TMDB for movie", "title", cleanedTitle, "original_title", m.Title, "year", m.Year)
		throttle()
		params := map[string]string{
			"api_key":  cfg.TMDBAPIKey,
			"query":    cleanedTitle,
			"language": "en-US",
		}
		if m.Year > 0 {
			params["year"] = fmt.Sprintf("%d", m.Year)
		}
		searchURL := sharedhttp.BuildQueryURL("https://api.themoviedb.org/3/search/movie", params)

		resp, err := sharedhttp.MakeRequest(context.Background(), searchURL, sharedhttp.LongTimeoutClient)
		if err != nil {
			slog.Error("TMDB API request failed", "title", m.Title, "error", err)
			return err
		}

		var searchResults TMDBMovieSearchResponse
		if err := sharedhttp.DecodeJSONResponse(resp, &searchResults); err != nil {
			slog.Error("Error decoding TMDB response", "title", m.Title, "error", err)
			return err
		}

		if len(searchResults.Results) == 0 {
			slog.Info("No TMDB results found for movie", "title", m.Title)
			return fmt.Errorf("no matches found on TMDB for %s", m.Title)
		}

		// Select the best matching result
		// Prefer exact title matches, then matches that contain the search title
		cleanedTitleLower := strings.ToLower(cleanedTitle)
		bestMatch := searchResults.Results[0]
		bestScore := 0
		
		for _, result := range searchResults.Results {
			resultTitleLower := strings.ToLower(result.Title)
			score := 0
			
			// Exact match gets highest score
			if resultTitleLower == cleanedTitleLower {
				score = 100
			} else if strings.Contains(resultTitleLower, cleanedTitleLower) {
				// Contains match gets medium score
				score = 50
			} else if strings.Contains(cleanedTitleLower, resultTitleLower) {
				// Search title contains result title (partial match)
				score = 25
			}
			
			// Bonus points if year matches (when year is provided)
			if m.Year > 0 && len(result.ReleaseDate) >= 4 {
				if year, err := strconv.Atoi(result.ReleaseDate[:4]); err == nil && year == m.Year {
					score += 10
				}
			}
			
			if score > bestScore {
				bestScore = score
				bestMatch = result
			}
		}
		
		matchedTMDBID = fmt.Sprintf("%d", bestMatch.ID)
		slog.Debug("Selected TMDB result", "tmdb_id", matchedTMDBID, "title", bestMatch.Title, "score", bestScore)
	}

	slog.Info("Using TMDB ID, fetching full details", "tmdb_id", matchedTMDBID)

	// 3. Fetch full details
	details, err := GetTMDBMovieDetails(cfg, matchedTMDBID)
	if err != nil {
		slog.Error("Error fetching full details for TMDB ID", "tmdb_id", matchedTMDBID, "error", err)
		return err
	}

	// Get genre names
	var genres []string
	if len(details.Genres) > 0 {
		for _, g := range details.Genres {
			genres = append(genres, g.Name)
		}
	}
	genreString := strings.Join(genres, ", ")

	// Let's store the raw JSON from TMDB too
	rawMetadata, _ := json.Marshal(details)

	// 4. Update DB with official metadata
	updateQuery := `
		UPDATE movies
		SET title = $1, year = $2, tmdb_id = $3, imdb_id = $4, overview = $5, poster_path = $6, genres = $7, status = 'matched', raw_metadata = $8, updated_at = CURRENT_TIMESTAMP
		WHERE id = $9
	`
	// Use details.ReleaseDate to update year if possible
	matchedYear := m.Year
	if len(details.ReleaseDate) >= 4 {
		if year, err := strconv.Atoi(details.ReleaseDate[:4]); err == nil {
			matchedYear = year
		}
	}

	_, err = database.DB.Exec(updateQuery, details.Title, matchedYear, fmt.Sprintf("%d", details.ID), details.IMDBID, details.Overview, details.PosterPath, genreString, rawMetadata, m.ID)
	if err != nil {
		slog.Error("Error updating DB for movie", "title", m.Title, "error", err)
	}
	return err
}

func MatchShow(cfg *config.Config, showID int) error {
	var s models.Show
	var tvdbID, tmdbID, imdbID sql.NullString
	query := `SELECT id, title, year, tvdb_id, tmdb_id, imdb_id FROM shows WHERE id = $1`
	err := database.DB.QueryRow(query, showID).Scan(&s.ID, &s.Title, &s.Year, &tvdbID, &tmdbID, &imdbID)
	if err != nil {
		slog.Error("Error fetching show from DB", "show_id", showID, "error", err)
		return err
	}
	s.TVDBID = tvdbID.String
	s.TMDBID = tmdbID.String
	s.IMDBID = imdbID.String

	// 1. Check if we already have metadata for this Title and Year in the DB
	var existingTVDBID, existingTMDBID, existingIMDBID, existingOverview, existingPosterPath, existingGenres sql.NullString
	var existingRawMetadata []byte
	checkQuery := `SELECT tvdb_id, tmdb_id, imdb_id, overview, poster_path, genres, raw_metadata FROM shows WHERE title = $1 AND year = $2 AND status = 'matched' AND tvdb_id IS NOT NULL AND tvdb_id != '' LIMIT 1`
	err = database.DB.QueryRow(checkQuery, s.Title, s.Year).Scan(&existingTVDBID, &existingTMDBID, &existingIMDBID, &existingOverview, &existingPosterPath, &existingGenres, &existingRawMetadata)
	if err == nil {
		slog.Info("Found existing metadata for show in DB, reusing", "title", s.Title, "year", s.Year)
		updateQuery := `
			UPDATE shows
			SET tvdb_id = $1, tmdb_id = $2, imdb_id = $3, overview = $4, poster_path = $5, genres = $6, status = 'matched', raw_metadata = $7, updated_at = CURRENT_TIMESTAMP
			WHERE id = $8
		`
		_, err = database.DB.Exec(updateQuery, existingTVDBID, existingTMDBID, existingIMDBID, existingOverview, existingPosterPath, existingGenres, existingRawMetadata, s.ID)
		return err
	}

	if cfg.TVDBAPIKey == "" {
		return fmt.Errorf("TVDB_API_KEY is not set")
	}

	var matchedTVDBID string

	// 2. Try to match by IDs first
	if s.TVDBID != "" {
		matchedTVDBID = s.TVDBID
	} else if s.TMDBID != "" || s.IMDBID != "" {
		idToSearch := s.TMDBID
		if idToSearch == "" {
			idToSearch = s.IMDBID
		}
		slog.Info("Searching TVDB by remote ID", "remote_id", idToSearch)
		results, err := SearchTVDBByRemoteID(cfg, idToSearch)
		if err == nil && len(results) > 0 {
			matchedTVDBID = results[0].ID
		}
	}

	// 3. Fallback to title search if IDs didn't work
	if matchedTVDBID == "" {
		// Clean the title before searching to remove quality tags and other metadata
		cleanedTitle := cleanTitleTags(s.Title)
		slog.Info("Searching TVDB for show", "title", cleanedTitle, "original_title", s.Title, "year", s.Year)
		results, err := SearchTVDB(cfg, cleanedTitle)
		if err == nil && len(results) > 0 {
			// If year is provided, try to find a result with matching year
			if s.Year > 0 {
				for _, res := range results {
					if res.Year == s.Year {
						matchedTVDBID = res.ID
						break
					}
				}
			}
			// If still no match and we have results, take the first one
			if matchedTVDBID == "" {
				matchedTVDBID = results[0].ID
			}
		}
	}

	if matchedTVDBID == "" {
		slog.Info("No TVDB results found for show", "title", s.Title)
		return fmt.Errorf("no matches found on TVDB for %s", s.Title)
	}

	slog.Info("Found TVDB match for show, fetching full details", "title", s.Title, "tvdb_id", matchedTVDBID)

	// 4. Fetch full details
	details, err := GetTVDBShowDetails(cfg, matchedTVDBID)
	finalIMDBID := s.IMDBID
	finalTMDBID := s.TMDBID

	if err == nil {
		for _, rid := range details.RemoteIDs {
			if rid.Type == 2 { // IMDB
				finalIMDBID = rid.ID
			} else if rid.Type == 3 { // TMDB
				finalTMDBID = rid.ID
			}
		}
	} else {
		slog.Error("Error fetching full details for TVDB ID", "tvdb_id", matchedTVDBID, "error", err)
		return err
	}

	var genres []string
	for _, g := range details.Genres {
		genres = append(genres, g.Name)
	}
	genreString := strings.Join(genres, ", ")
	rawMetadata, _ := json.Marshal(details)

	// 5. Update DB with official metadata
	updateQuery := `
		UPDATE shows
		SET title = $1, year = $2, tvdb_id = $3, tmdb_id = $4, imdb_id = $5, overview = $6, poster_path = $7, genres = $8, status = 'matched', raw_metadata = $9, updated_at = CURRENT_TIMESTAMP
		WHERE id = $10
	`
	matchedYear := s.Year
	if len(details.FirstAired) >= 4 {
		if year, err := strconv.Atoi(details.FirstAired[:4]); err == nil {
			matchedYear = year
		}
	}

	_, err = database.DB.Exec(updateQuery, details.Name, matchedYear, fmt.Sprintf("%d", details.ID), finalTMDBID, finalIMDBID, details.Overview, details.Image, genreString, rawMetadata, s.ID)
	if err != nil {
		slog.Error("Error updating DB for show", "title", s.Title, "error", err)
		return err
	}

	// 6. Sync episode titles from TVDB
	go SyncShowEpisodes(cfg, s.ID)

	return nil
}

func SearchTVDBByRemoteID(cfg *config.Config, remoteID string) ([]SearchResult, error) {
	if cfg.TVDBAPIKey == "" {
		return nil, fmt.Errorf("TVDB_API_KEY is not set")
	}

	token, err := getTVDBToken(cfg.TVDBAPIKey)
	if err != nil {
		return nil, err
	}

	throttle()
	searchURL := fmt.Sprintf("https://api4.thetvdb.com/v4/search/remoteid/%s", remoteID)

	req, _ := http.NewRequest("GET", searchURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := sharedhttp.LongTimeoutClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("TVDB returned status %d", resp.StatusCode)
	}

	var searchResults TVDBSearchResponse
	if err := sharedhttp.DecodeJSONResponse(resp, &searchResults); err != nil {
		return nil, err
	}

	results := make([]SearchResult, 0, len(searchResults.Data))
	for _, r := range searchResults.Data {
		year, _ := strconv.Atoi(r.Year)
		results = append(results, SearchResult{
			ID:         r.TVDBID,
			Title:      r.Name,
			Year:       year,
			MediaType:  "show",
			PosterPath: r.ImageURL,
			Overview:   r.Overview,
			Genres:     r.Genres,
		})
	}

	return results, nil
}

func SyncShowEpisodes(cfg *config.Config, showID int) error {
	var tvdbID string
	err := database.DB.QueryRow("SELECT tvdb_id FROM shows WHERE id = $1", showID).Scan(&tvdbID)
	if err != nil || tvdbID == "" {
		return fmt.Errorf("show has no TVDB ID")
	}

	episodes, err := GetTVDBShowEpisodes(cfg, tvdbID)
	if err != nil {
		return err
	}

	for _, ep := range episodes {
		// Update existing episodes with official titles
		query := `
			UPDATE episodes
			SET title = $1
			WHERE season_id IN (SELECT id FROM seasons WHERE show_id = $2 AND season_number = $3)
			AND episode_number = $4
		`
		database.DB.Exec(query, ep.Name, showID, ep.SeasonNumber, ep.Number)
	}

	slog.Info("Synced episodes for show", "episode_count", len(episodes), "show_id", showID)
	return nil
}

func interfaceToInt(v interface{}) int {
	switch val := v.(type) {
	case string:
		i, _ := strconv.Atoi(val)
		return i
	case int:
		return val
	case float64:
		return int(val)
	default:
		return 0
	}
}

func FetchMetadataForAllDiscovered(cfg *config.Config) {
	slog.Info("Starting background metadata fetching")
	// Movies
	movieQuery := `SELECT id FROM movies WHERE status = 'discovered'`
	movieRows, err := database.DB.Query(movieQuery)
	if err == nil {
		defer movieRows.Close()
		for movieRows.Next() {
			var id int
			if err := movieRows.Scan(&id); err == nil {
				MatchMovie(cfg, id)
			}
		}
	}

	// Shows
	showQuery := `SELECT id FROM shows WHERE status = 'discovered'`
	showRows, err := database.DB.Query(showQuery)
	if err == nil {
		defer showRows.Close()
		for showRows.Next() {
			var id int
			if err := showRows.Scan(&id); err == nil {
				MatchShow(cfg, id)
			}
		}
	}
	slog.Info("Background metadata fetching complete")
}

// GetMovieAlternatives searches TMDB for alternative matches for a movie
// Returns up to 10 results
func GetMovieAlternatives(cfg *config.Config, movieID int) ([]SearchResult, error) {
	var m models.Movie
	var tmdbID sql.NullString
	query := `SELECT id, title, year, tmdb_id FROM movies WHERE id = $1`
	err := database.DB.QueryRow(query, movieID).Scan(&m.ID, &m.Title, &m.Year, &tmdbID)
	if err != nil {
		return nil, err
	}

	if cfg.TMDBAPIKey == "" {
		return nil, fmt.Errorf("TMDB_API_KEY is not set")
	}

	// Clean the title before searching
	cleanedTitle := cleanTitleTags(m.Title)
	slog.Info("Searching TMDB for alternatives", "title", cleanedTitle, "original_title", m.Title, "year", m.Year)
	
	throttle()
	params := map[string]string{
		"api_key":  cfg.TMDBAPIKey,
		"query":    cleanedTitle,
		"language": "en-US",
	}
	if m.Year > 0 {
		params["year"] = fmt.Sprintf("%d", m.Year)
	}
	searchURL := sharedhttp.BuildQueryURL("https://api.themoviedb.org/3/search/movie", params)

	resp, err := sharedhttp.MakeRequest(context.Background(), searchURL, sharedhttp.LongTimeoutClient)
	if err != nil {
		return nil, err
	}

	var searchResults TMDBMovieSearchResponse
	if err := sharedhttp.DecodeJSONResponse(resp, &searchResults); err != nil {
		return nil, err
	}

	// Limit to 10 results
	maxResults := 10
	if len(searchResults.Results) > maxResults {
		searchResults.Results = searchResults.Results[:maxResults]
	}

	results := make([]SearchResult, 0, len(searchResults.Results))
	for _, r := range searchResults.Results {
		year := 0
		if len(r.ReleaseDate) >= 4 {
			year, _ = strconv.Atoi(r.ReleaseDate[:4])
		}

		var genres []string
		for _, id := range r.GenreIDs {
			if name, ok := tmdbGenres[id]; ok {
				genres = append(genres, name)
			}
		}

		results = append(results, SearchResult{
			ID:         fmt.Sprintf("%d", r.ID),
			Title:      r.Title,
			Year:       year,
			MediaType:  "movie",
			PosterPath: r.PosterPath,
			Overview:   r.Overview,
			Genres:     genres,
		})
	}

	return results, nil
}

// GetShowAlternatives searches TVDB for alternative matches for a show
// Returns up to 10 results
func GetShowAlternatives(cfg *config.Config, showID int) ([]SearchResult, error) {
	var s models.Show
	var tvdbID sql.NullString
	query := `SELECT id, title, year, tvdb_id FROM shows WHERE id = $1`
	err := database.DB.QueryRow(query, showID).Scan(&s.ID, &s.Title, &s.Year, &tvdbID)
	if err != nil {
		return nil, err
	}

	if cfg.TVDBAPIKey == "" {
		return nil, fmt.Errorf("TVDB_API_KEY is not set")
	}

	// Clean the title before searching
	cleanedTitle := cleanTitleTags(s.Title)
	slog.Info("Searching TVDB for alternatives", "title", cleanedTitle, "original_title", s.Title, "year", s.Year)
	
	results, err := SearchTVDB(cfg, cleanedTitle)
	if err != nil {
		return nil, err
	}

	// Limit to 10 results
	maxResults := 10
	if len(results) > maxResults {
		results = results[:maxResults]
	}

	return results, nil
}

// RematchMovie updates a movie with a new TMDB ID and fetches full metadata
func RematchMovie(cfg *config.Config, movieID int, newTMDBID string) error {
	// Fetch full details for the new TMDB ID
	details, err := GetTMDBMovieDetails(cfg, newTMDBID)
	if err != nil {
		return err
	}

	// Get genre names
	var genres []string
	if len(details.Genres) > 0 {
		for _, g := range details.Genres {
			genres = append(genres, g.Name)
		}
	}
	genreString := strings.Join(genres, ", ")

	// Store the raw JSON from TMDB
	rawMetadata, _ := json.Marshal(details)

	// Update DB with official metadata
	updateQuery := `
		UPDATE movies
		SET title = $1, year = $2, tmdb_id = $3, imdb_id = $4, overview = $5, poster_path = $6, genres = $7, status = 'matched', raw_metadata = $8, updated_at = CURRENT_TIMESTAMP
		WHERE id = $9
	`
	matchedYear := 0
	if len(details.ReleaseDate) >= 4 {
		if year, err := strconv.Atoi(details.ReleaseDate[:4]); err == nil {
			matchedYear = year
		}
	}

	_, err = database.DB.Exec(updateQuery, details.Title, matchedYear, fmt.Sprintf("%d", details.ID), details.IMDBID, details.Overview, details.PosterPath, genreString, rawMetadata, movieID)
	if err != nil {
		slog.Error("Error updating DB for movie rematch", "movie_id", movieID, "error", err)
		return err
	}

	slog.Info("Movie rematched successfully", "movie_id", movieID, "new_tmdb_id", newTMDBID)
	return nil
}

// RematchShow updates a show with a new TVDB ID and fetches full metadata
func RematchShow(cfg *config.Config, showID int, newTVDBID string) error {
	// Fetch full details for the new TVDB ID
	details, err := GetTVDBShowDetails(cfg, newTVDBID)
	if err != nil {
		return err
	}

	// Extract IMDB and TMDB IDs from remote IDs
	var finalIMDBID, finalTMDBID string
	for _, rid := range details.RemoteIDs {
		if rid.Type == 2 { // IMDB
			finalIMDBID = rid.ID
		} else if rid.Type == 3 { // TMDB
			finalTMDBID = rid.ID
		}
	}

	var genres []string
	for _, g := range details.Genres {
		genres = append(genres, g.Name)
	}
	genreString := strings.Join(genres, ", ")
	rawMetadata, _ := json.Marshal(details)

	// Update DB with official metadata
	updateQuery := `
		UPDATE shows
		SET title = $1, year = $2, tvdb_id = $3, tmdb_id = $4, imdb_id = $5, overview = $6, poster_path = $7, genres = $8, status = 'matched', raw_metadata = $9, updated_at = CURRENT_TIMESTAMP
		WHERE id = $10
	`
	matchedYear := 0
	if len(details.FirstAired) >= 4 {
		if year, err := strconv.Atoi(details.FirstAired[:4]); err == nil {
			matchedYear = year
		}
	}

	_, err = database.DB.Exec(updateQuery, details.Name, matchedYear, fmt.Sprintf("%d", details.ID), finalTMDBID, finalIMDBID, details.Overview, details.Image, genreString, rawMetadata, showID)
	if err != nil {
		slog.Error("Error updating DB for show rematch", "show_id", showID, "error", err)
		return err
	}

	// Sync episode titles from TVDB
	go SyncShowEpisodes(cfg, showID)

	slog.Info("Show rematched successfully", "show_id", showID, "new_tvdb_id", newTVDBID)
	return nil
}
