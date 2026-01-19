package services

import (
	"Arrgo/config"
	"Arrgo/database"
	"Arrgo/models"
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
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
	url := fmt.Sprintf("https://api.themoviedb.org/3/movie/%s?api_key=%s&language=en-US", tmdbID, cfg.TMDBAPIKey)

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TMDB returned status %d", resp.StatusCode)
	}

	var details TMDBMovieDetails
	if err := json.NewDecoder(resp.Body).Decode(&details); err != nil {
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

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TVDB returned status %d", resp.StatusCode)
	}

	var result struct {
		Data TVDBShowDetails `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
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

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TVDB returned status %d", resp.StatusCode)
	}

	var result struct {
		Data struct {
			Episodes []TVDBEpisode `json:"episodes"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
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
	searchURL := fmt.Sprintf("https://api.themoviedb.org/3/search/movie?api_key=%s&query=%s&language=en-US",
		cfg.TMDBAPIKey, url.QueryEscape(query))

	resp, err := http.Get(searchURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var searchResults TMDBMovieSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResults); err != nil {
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
	searchURL := fmt.Sprintf("https://api4.thetvdb.com/v4/search?query=%s&type=series&lang=eng", url.QueryEscape(query))

	req, _ := http.NewRequest("GET", searchURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var searchResults TVDBSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResults); err != nil {
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

	log.Printf("[METADATA] Authenticating with TVDB...")
	payload, _ := json.Marshal(map[string]string{"apikey": apiKey})
	resp, err := http.Post("https://api4.thetvdb.com/v4/login", "application/json", bytes.NewBuffer(payload))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var auth TVDBAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&auth); err != nil {
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
	query := `SELECT id, title, year FROM movies WHERE id = $1`
	err := database.DB.QueryRow(query, movieID).Scan(&m.ID, &m.Title, &m.Year)
	if err != nil {
		log.Printf("[METADATA] Error fetching movie %d from DB: %v", movieID, err)
		return err
	}

	// 1.5 Check if we already have metadata for this movie title/year in the DB
	var existingTMDBID, existingIMDBID, existingOverview, existingPosterPath, existingGenres sql.NullString
	var existingRawMetadata []byte
	checkQuery := `SELECT tmdb_id, imdb_id, overview, poster_path, genres, raw_metadata FROM movies WHERE title = $1 AND year = $2 AND status = 'matched' AND imdb_id IS NOT NULL AND imdb_id != '' LIMIT 1`
	err = database.DB.QueryRow(checkQuery, m.Title, m.Year).Scan(&existingTMDBID, &existingIMDBID, &existingOverview, &existingPosterPath, &existingGenres, &existingRawMetadata)
	if err == nil {
		log.Printf("[METADATA] Found existing metadata for %s (%d) in DB, reusing...", m.Title, m.Year)
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

	// 2. Search TMDB
	log.Printf("[METADATA] Searching TMDB for movie: %s (%d)", m.Title, m.Year)
	throttle()
	searchURL := fmt.Sprintf("https://api.themoviedb.org/3/search/movie?api_key=%s&query=%s&language=en-US",
		cfg.TMDBAPIKey, url.QueryEscape(m.Title))
	if m.Year > 0 {
		searchURL += fmt.Sprintf("&year=%d", m.Year)
	}

	resp, err := http.Get(searchURL)
	if err != nil {
		log.Printf("[METADATA] TMDB API request failed for %s: %v", m.Title, err)
		return err
	}
	defer resp.Body.Close()

	var searchResults TMDBMovieSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResults); err != nil {
		log.Printf("[METADATA] Error decoding TMDB response for %s: %v", m.Title, err)
		return err
	}

	if len(searchResults.Results) == 0 {
		log.Printf("[METADATA] No TMDB results found for movie: %s", m.Title)
		return fmt.Errorf("no matches found on TMDB for %s", m.Title)
	}

	// 3. Take the first result
	searchResult := searchResults.Results[0]
	log.Printf("[METADATA] Found search match for %s: %s (TMDB ID: %d). Fetching full details...", m.Title, searchResult.Title, searchResult.ID)

	// 3.5 Fetch full details to get IMDB ID
	details, err := GetTMDBMovieDetails(cfg, fmt.Sprintf("%d", searchResult.ID))
	if err != nil {
		log.Printf("[METADATA] Error fetching full details for TMDB ID %d: %v", searchResult.ID, err)
		// Fallback to search results if details fail, though we won't have IMDB ID
		details = &TMDBMovieDetails{
			ID:         searchResult.ID,
			Title:      searchResult.Title,
			Overview:   searchResult.Overview,
			PosterPath: searchResult.PosterPath,
		}
	}

	// Get genre names
	var genres []string
	if len(details.Genres) > 0 {
		for _, g := range details.Genres {
			genres = append(genres, g.Name)
		}
	} else {
		// Fallback to genre IDs from search result
		for _, id := range searchResult.GenreIDs {
			if name, ok := tmdbGenres[id]; ok {
				genres = append(genres, name)
			}
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
		log.Printf("[METADATA] Error updating DB for movie %s: %v", m.Title, err)
	}
	return err
}

func MatchShow(cfg *config.Config, showID int) error {
	var s models.Show
	query := `SELECT id, title, year FROM shows WHERE id = $1`
	err := database.DB.QueryRow(query, showID).Scan(&s.ID, &s.Title, &s.Year)
	if err != nil {
		log.Printf("[METADATA] Error fetching show %d from DB: %v", showID, err)
		return err
	}

	// 1. Check if we already have metadata for this Title and Year in the DB
	var existingTVDBID, existingIMDBID, existingOverview, existingPosterPath, existingGenres sql.NullString
	var existingRawMetadata []byte
	checkQuery := `SELECT tvdb_id, imdb_id, overview, poster_path, genres, raw_metadata FROM shows WHERE title = $1 AND year = $2 AND status = 'matched' AND imdb_id IS NOT NULL AND imdb_id != '' LIMIT 1`
	err = database.DB.QueryRow(checkQuery, s.Title, s.Year).Scan(&existingTVDBID, &existingIMDBID, &existingOverview, &existingPosterPath, &existingGenres, &existingRawMetadata)
	if err == nil {
		log.Printf("[METADATA] Found existing metadata for show %s (%d) in DB, reusing...", s.Title, s.Year)
		updateQuery := `
			UPDATE shows 
			SET tvdb_id = $1, imdb_id = $2, overview = $3, poster_path = $4, genres = $5, status = 'matched', raw_metadata = $6, updated_at = CURRENT_TIMESTAMP
			WHERE id = $7
		`
		_, err = database.DB.Exec(updateQuery, existingTVDBID, existingIMDBID, existingOverview, existingPosterPath, existingGenres, existingRawMetadata, s.ID)
		return err
	}

	if cfg.TVDBAPIKey == "" {
		return fmt.Errorf("TVDB_API_KEY is not set")
	}

	// 2. Search TVDB
	token, err := getTVDBToken(cfg.TVDBAPIKey)
	if err != nil {
		return fmt.Errorf("failed to get TVDB token: %v", err)
	}

	log.Printf("[METADATA] Searching TVDB for show: %s (%d)", s.Title, s.Year)
	throttle()
	searchURL := fmt.Sprintf("https://api4.thetvdb.com/v4/search?query=%s&type=series&lang=eng", url.QueryEscape(s.Title))
	if s.Year > 0 {
		searchURL += fmt.Sprintf("&year=%d", s.Year)
	}

	req, _ := http.NewRequest("GET", searchURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[METADATA] TVDB API request failed for show %s: %v", s.Title, err)
		return err
	}
	defer resp.Body.Close()

	var searchResults TVDBSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResults); err != nil {
		log.Printf("[METADATA] Error decoding TVDB response for show %s: %v", s.Title, err)
		return err
	}

	if len(searchResults.Data) == 0 {
		log.Printf("[METADATA] No TVDB results found for show: %s", s.Title)
		return fmt.Errorf("no matches found on TVDB for %s", s.Title)
	}

	// 3. Take the first result
	searchResult := searchResults.Data[0]
	log.Printf("[METADATA] Found search match for show %s: %s (TVDB ID: %s). Fetching full details...", s.Title, searchResult.Name, searchResult.TVDBID)

	// 3.5 Fetch full details to get IMDB ID
	details, err := GetTVDBShowDetails(cfg, searchResult.TVDBID)
	imdbID := ""
	if err == nil {
		for _, rid := range details.RemoteIDs {
			if rid.Type == 2 { // IMDB
				imdbID = rid.ID
				break
			}
		}
	} else {
		log.Printf("[METADATA] Error fetching full details for TVDB ID %s: %v", searchResult.TVDBID, err)
		// Fallback to search result
		details = &TVDBShowDetails{
			ID:       interfaceToInt(searchResult.TVDBID),
			Name:     searchResult.Name,
			Overview: searchResult.Overview,
			Image:    searchResult.ImageURL,
			Genres: []struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
			}{},
		}
		for _, g := range searchResult.Genres {
			details.Genres = append(details.Genres, struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
			}{Name: g})
		}
	}

	var genres []string
	for _, g := range details.Genres {
		genres = append(genres, g.Name)
	}
	genreString := strings.Join(genres, ", ")
	rawMetadata, _ := json.Marshal(details)

	// 4. Update DB with official metadata
	updateQuery := `
		UPDATE shows 
		SET title = $1, year = $2, tvdb_id = $3, imdb_id = $4, overview = $5, poster_path = $6, genres = $7, status = 'matched', raw_metadata = $8, updated_at = CURRENT_TIMESTAMP
		WHERE id = $9
	`
	matchedYear := s.Year
	if len(details.FirstAired) >= 4 {
		if year, err := strconv.Atoi(details.FirstAired[:4]); err == nil {
			matchedYear = year
		}
	}

	_, err = database.DB.Exec(updateQuery, details.Name, matchedYear, fmt.Sprintf("%d", details.ID), imdbID, details.Overview, details.Image, genreString, rawMetadata, s.ID)
	if err != nil {
		log.Printf("[METADATA] Error updating DB for show %s: %v", s.Title, err)
		return err
	}

	// 5. Sync episode titles from TVDB
	go SyncShowEpisodes(cfg, s.ID)

	return nil
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

	log.Printf("[METADATA] Synced %d episodes for show ID %d", len(episodes), showID)
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
	log.Printf("[METADATA] Starting background metadata fetching...")
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
	log.Printf("[METADATA] Background metadata fetching complete.")
}
