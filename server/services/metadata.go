package services

import (
	"Arrgo/config"
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

// MetadataService encapsulates all metadata-fetching state (TVDB token, rate limiter, etc.)
type MetadataService struct {
	cfg             *config.Config
	db              *sql.DB
	tvdbToken       string
	tvdbTokenExpiry time.Time
	tvdbMutex       sync.Mutex
	lastRequestTime time.Time
	rateLimitMutex  sync.Mutex
}

// NewMetadataService creates a new MetadataService instance.
func NewMetadataService(cfg *config.Config, db *sql.DB) *MetadataService {
	return &MetadataService{cfg: cfg, db: db}
}

var globalMetadata *MetadataService

// SetGlobalMetadataService sets the package-level MetadataService instance.
func SetGlobalMetadataService(m *MetadataService) {
	globalMetadata = m
}

// GetGlobalMetadataService returns the package-level MetadataService instance.
func GetGlobalMetadataService() *MetadataService {
	return globalMetadata
}

var (
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
	Translations struct {
		NameTranslations []struct {
			Name     string `json:"name"`
			Language string `json:"language"`
		} `json:"nameTranslations"`
	} `json:"translations"`
	Aliases []struct {
		Name     string `json:"name"`
		Language string `json:"language"`
	} `json:"aliases"`
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

func (s *MetadataService) GetTMDBMovieDetails(tmdbID string) (*TMDBMovieDetails, error) {
	if s.cfg.TMDBAPIKey == "" {
		return nil, fmt.Errorf("TMDB_API_KEY is not set")
	}

	s.throttle()
	apiURL := fmt.Sprintf("https://api.themoviedb.org/3/movie/%s?api_key=%s&language=en-US", tmdbID, s.cfg.TMDBAPIKey)

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

func (s *MetadataService) GetTVDBShowDetails(tvdbID string) (*TVDBShowDetails, error) {
	if s.cfg.TVDBAPIKey == "" {
		return nil, fmt.Errorf("TVDB_API_KEY is not set")
	}

	token, err := s.getTVDBToken()
	if err != nil {
		return nil, err
	}

	s.throttle()
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

func (s *MetadataService) GetTVDBShowEpisodes(tvdbID string) ([]TVDBEpisode, error) {
	if s.cfg.TVDBAPIKey == "" {
		return nil, fmt.Errorf("TVDB_API_KEY is not set")
	}

	token, err := s.getTVDBToken()
	if err != nil {
		return nil, err
	}

	s.throttle()
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

func (s *MetadataService) throttle() {
	s.rateLimitMutex.Lock()
	defer s.rateLimitMutex.Unlock()

	elapsed := time.Since(s.lastRequestTime)
	if elapsed < 200*time.Millisecond {
		time.Sleep(200*time.Millisecond - elapsed)
	}
	s.lastRequestTime = time.Now()
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

func (s *MetadataService) SearchTMDB(query string) ([]SearchResult, error) {
	if s.cfg.TMDBAPIKey == "" {
		return nil, fmt.Errorf("TMDB_API_KEY is not set")
	}

	// Get search variants (e.g., "In & Out" -> ["In & Out", "In and Out"])
	variants := ExpandSearchQuery(query)

	// Track seen IDs to avoid duplicates
	seenIDs := make(map[string]bool)
	allResults := make([]SearchResult, 0)

	// Search each variant and merge results
	for _, variant := range variants {
		s.throttle()
		searchURL := sharedhttp.BuildQueryURL("https://api.themoviedb.org/3/search/movie", map[string]string{
			"api_key":  s.cfg.TMDBAPIKey,
			"query":    variant,
			"language": "en-US",
		})

		resp, err := sharedhttp.MakeRequest(context.Background(), searchURL, sharedhttp.LongTimeoutClient)
		if err != nil {
			// Continue with next variant if one fails
			continue
		}

		var searchResults TMDBMovieSearchResponse
		if err := sharedhttp.DecodeJSONResponse(resp, &searchResults); err != nil {
			// Continue with next variant if decoding fails
			continue
		}

		for _, r := range searchResults.Results {
			id := fmt.Sprintf("%d", r.ID)
			// Skip if we've already seen this result
			if seenIDs[id] {
				continue
			}
			seenIDs[id] = true

			year := 0
			if len(r.ReleaseDate) >= 4 {
				year, _ = strconv.Atoi(r.ReleaseDate[:4])
			}

			var genres []string
			for _, genreID := range r.GenreIDs {
				if name, ok := tmdbGenres[genreID]; ok {
					genres = append(genres, name)
				}
			}

			allResults = append(allResults, SearchResult{
				ID:         id,
				Title:      r.Title,
				Year:       year,
				MediaType:  "movie",
				PosterPath: r.PosterPath,
				Overview:   r.Overview,
				Genres:     genres,
			})
		}
	}

	return allResults, nil
}

func (s *MetadataService) SearchTVDB(query string) ([]SearchResult, error) {
	if s.cfg.TVDBAPIKey == "" {
		return nil, fmt.Errorf("TVDB_API_KEY is not set")
	}

	token, err := s.getTVDBToken()
	if err != nil {
		return nil, err
	}

	// Get search variants (e.g., "In & Out" -> ["In & Out", "In and Out"])
	variants := ExpandSearchQuery(query)

	// Track seen IDs to avoid duplicates
	seenIDs := make(map[string]bool)
	allResults := make([]SearchResult, 0)

	// Search each variant and merge results
	for _, variant := range variants {
		s.throttle()
		searchURL := sharedhttp.BuildQueryURL("https://api4.thetvdb.com/v4/search", map[string]string{
			"query": variant,
			"type":  "series",
			"lang":  "eng",
		})

		req, _ := http.NewRequest("GET", searchURL, nil)
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := sharedhttp.LongTimeoutClient.Do(req)
		if err != nil {
			// Continue with next variant if one fails
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			// Continue with next variant if status is not OK
			continue
		}

		var searchResults TVDBSearchResponse
		if err := sharedhttp.DecodeJSONResponse(resp, &searchResults); err != nil {
			// Continue with next variant if decoding fails
			continue
		}

		for _, r := range searchResults.Data {
			// Skip if we've already seen this result
			if seenIDs[r.TVDBID] {
				continue
			}
			seenIDs[r.TVDBID] = true

			year, _ := strconv.Atoi(r.Year)
			allResults = append(allResults, SearchResult{
				ID:         r.TVDBID,
				Title:      r.Name,
				Year:       year,
				MediaType:  "show",
				PosterPath: r.ImageURL,
				Overview:   r.Overview,
				Genres:     r.Genres,
			})
		}
	}

	return allResults, nil
}

func (s *MetadataService) getTVDBToken() (string, error) {
	s.tvdbMutex.Lock()
	defer s.tvdbMutex.Unlock()

	if s.tvdbToken != "" && time.Now().Before(s.tvdbTokenExpiry) {
		return s.tvdbToken, nil
	}

	slog.Info("Authenticating with TVDB")
	payload, _ := json.Marshal(map[string]string{"apikey": s.cfg.TVDBAPIKey})
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

	s.tvdbToken = auth.Data.Token
	s.tvdbTokenExpiry = time.Now().Add(23 * time.Hour) // Tokens usually last 24h
	return s.tvdbToken, nil
}

func (s *MetadataService) MatchMovie(movieID int) error {
	// 1. Fetch movie from DB
	var m models.Movie
	var tmdbID, imdbID sql.NullString
	query := `SELECT id, title, year, tmdb_id, imdb_id FROM movies WHERE id = $1`
	err := s.db.QueryRow(query, movieID).Scan(&m.ID, &m.Title, &m.Year, &tmdbID, &imdbID)
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
	err = s.db.QueryRow(checkQuery, m.Title, m.Year).Scan(&existingTMDBID, &existingIMDBID, &existingOverview, &existingPosterPath, &existingGenres, &existingRawMetadata)
	if err == nil {
		slog.Info("Found existing metadata in DB, reusing", "title", m.Title, "year", m.Year)
		updateQuery := `
			UPDATE movies
			SET tmdb_id = $1, imdb_id = $2, overview = $3, poster_path = $4, genres = $5, status = 'matched', raw_metadata = $6, updated_at = CURRENT_TIMESTAMP
			WHERE id = $7
		`
		_, err = s.db.Exec(updateQuery, existingTMDBID, existingIMDBID, existingOverview, existingPosterPath, existingGenres, existingRawMetadata, m.ID)
		if err != nil {
			return err
		}
		// Update the request with proper metadata if this movie is linked to a request
		s.updateRequestFromMatchedMovie(m.ID, m.Title, m.Year, existingTMDBID.String, existingIMDBID.String, existingOverview.String, existingPosterPath.String)
		return nil
	}

	if s.cfg.TMDBAPIKey == "" {
		return fmt.Errorf("TMDB_API_KEY is not set")
	}

	var matchedTMDBID = m.TMDBID

	// 2. Check if movie is linked to a request with TMDB ID via torrent hash
	if matchedTMDBID == "" {
		var torrentHash sql.NullString
		err = s.db.QueryRow("SELECT torrent_hash FROM movies WHERE id = $1", movieID).Scan(&torrentHash)
		if err == nil && torrentHash.Valid && torrentHash.String != "" {
			var requestTMDBID sql.NullString
			err = s.db.QueryRow(`
				SELECT r.tmdb_id
				FROM requests r
				JOIN downloads d ON r.id = d.request_id
				WHERE LOWER(d.torrent_hash) = LOWER($1)
				AND r.media_type = 'movie'
				AND r.tmdb_id IS NOT NULL
				AND r.tmdb_id != ''
				LIMIT 1`, torrentHash.String).Scan(&requestTMDBID)
			if err == nil && requestTMDBID.Valid && requestTMDBID.String != "" {
				matchedTMDBID = requestTMDBID.String
				slog.Info("Found request TMDB ID from torrent hash", "movie_id", movieID, "tmdb_id", matchedTMDBID, "torrent_hash", torrentHash.String)
			}
		}
	}

	// 3. Search TMDB if ID still not provided
	if matchedTMDBID == "" {
		// Clean the title before searching to remove quality tags and other metadata
		cleanedTitle := cleanTitleTags(m.Title)
		slog.Info("Searching TMDB for movie", "title", cleanedTitle, "original_title", m.Title, "year", m.Year)
		s.throttle()
		params := map[string]string{
			"api_key":  s.cfg.TMDBAPIKey,
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
	details, err := s.GetTMDBMovieDetails(matchedTMDBID)
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

	_, err = s.db.Exec(updateQuery, details.Title, matchedYear, fmt.Sprintf("%d", details.ID), details.IMDBID, details.Overview, details.PosterPath, genreString, rawMetadata, m.ID)
	if err != nil {
		slog.Error("Error updating DB for movie", "title", m.Title, "error", err)
		return err
	}

	// Update the request with proper metadata if this movie is linked to a request via torrent hash
	s.updateRequestFromMatchedMovie(m.ID, details.Title, matchedYear, fmt.Sprintf("%d", details.ID), details.IMDBID, details.Overview, details.PosterPath)

	return nil
}

func (s *MetadataService) MatchShow(showID int) error {
	var sh models.Show
	var tvdbID, tmdbID, imdbID sql.NullString
	query := `SELECT id, title, year, tvdb_id, tmdb_id, imdb_id FROM shows WHERE id = $1`
	err := s.db.QueryRow(query, showID).Scan(&sh.ID, &sh.Title, &sh.Year, &tvdbID, &tmdbID, &imdbID)
	if err != nil {
		slog.Error("Error fetching show from DB", "show_id", showID, "error", err)
		return err
	}
	sh.TVDBID = tvdbID.String
	sh.TMDBID = tmdbID.String
	sh.IMDBID = imdbID.String

	// 1. Check if we already have metadata for this Title and Year in the DB
	var existingTVDBID, existingTMDBID, existingIMDBID, existingOverview, existingPosterPath, existingGenres sql.NullString
	var existingRawMetadata []byte
	checkQuery := `SELECT tvdb_id, tmdb_id, imdb_id, overview, poster_path, genres, raw_metadata FROM shows WHERE title = $1 AND year = $2 AND status = 'matched' AND tvdb_id IS NOT NULL AND tvdb_id != '' LIMIT 1`
	err = s.db.QueryRow(checkQuery, sh.Title, sh.Year).Scan(&existingTVDBID, &existingTMDBID, &existingIMDBID, &existingOverview, &existingPosterPath, &existingGenres, &existingRawMetadata)
	if err == nil {
		slog.Info("Found existing metadata for show in DB, reusing", "title", sh.Title, "year", sh.Year)
		updateQuery := `
			UPDATE shows
			SET tvdb_id = $1, tmdb_id = $2, imdb_id = $3, overview = $4, poster_path = $5, genres = $6, status = 'matched', raw_metadata = $7, updated_at = CURRENT_TIMESTAMP
			WHERE id = $8
		`
		_, err = s.db.Exec(updateQuery, existingTVDBID, existingTMDBID, existingIMDBID, existingOverview, existingPosterPath, existingGenres, existingRawMetadata, sh.ID)
		return err
	}

	if s.cfg.TVDBAPIKey == "" {
		return fmt.Errorf("TVDB_API_KEY is not set")
	}

	var matchedTVDBID string

	// 2. Try to match by IDs first
	if sh.TVDBID != "" {
		matchedTVDBID = sh.TVDBID
	} else if sh.TMDBID != "" || sh.IMDBID != "" {
		idToSearch := sh.TMDBID
		if idToSearch == "" {
			idToSearch = sh.IMDBID
		}
		slog.Info("Searching TVDB by remote ID", "remote_id", idToSearch)
		results, err := s.SearchTVDBByRemoteID(idToSearch)
		if err == nil && len(results) > 0 {
			matchedTVDBID = results[0].ID
		}
	}

	// 3. Fallback to title search if IDs didn't work
	if matchedTVDBID == "" {
		// Clean the title before searching to remove quality tags and other metadata
		cleanedTitle := cleanTitleTags(sh.Title)
		slog.Info("Searching TVDB for show", "title", cleanedTitle, "original_title", sh.Title, "year", sh.Year)
		results, err := s.SearchTVDB(cleanedTitle)
		if err == nil && len(results) > 0 {
			// Select the best matching result
			// Prefer exact title matches, then matches that contain the search title
			cleanedTitleLower := strings.ToLower(cleanedTitle)
			bestMatch := results[0]
			bestScore := -1

			for _, res := range results {
				resTitleLower := strings.ToLower(res.Title)
				score := 0

				// Exact match gets highest score
				if resTitleLower == cleanedTitleLower {
					score = 100
				} else if strings.Contains(resTitleLower, cleanedTitleLower) {
					// Contains match gets medium score
					score = 50
				} else if strings.Contains(cleanedTitleLower, resTitleLower) {
					// Search title contains result title (partial match)
					score = 25
				}

				// Bonus points if year matches
				if sh.Year > 0 && res.Year > 0 {
					if res.Year == sh.Year {
						score += 10
					} else if res.Year >= sh.Year-1 && res.Year <= sh.Year+1 {
						// Small bonus for being close (sometimes metadata has slight variant years)
						score += 5
					}
				}

				if score > bestScore {
					bestScore = score
					bestMatch = res
				}
			}

			if bestScore >= 0 {
				matchedTVDBID = bestMatch.ID
				slog.Debug("Selected TVDB result", "tvdb_id", matchedTVDBID, "title", bestMatch.Title, "score", bestScore)
			}
		}
	}

	if matchedTVDBID == "" {
		slog.Info("No TVDB results found for show", "title", sh.Title)
		return fmt.Errorf("no matches found on TVDB for %s", sh.Title)
	}

	slog.Info("Found TVDB match for show, fetching full details", "title", sh.Title, "tvdb_id", matchedTVDBID)

	// 4. Fetch full details
	details, err := s.GetTVDBShowDetails(matchedTVDBID)
	finalIMDBID := sh.IMDBID
	finalTMDBID := sh.TMDBID

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

	// 5. Extract English title from translations
	// This is crucial for non-English shows where the primary title is in another language
	englishTitle := ""
	for _, trans := range details.Translations.NameTranslations {
		// Look for English translation (language code "eng")
		if trans.Language == "eng" && trans.Name != "" {
			englishTitle = trans.Name
			break
		}
	}

	// Fallback to aliases if translation is missing
	if englishTitle == "" {
		for _, alias := range details.Aliases {
			if alias.Language == "eng" && alias.Name != "" {
				englishTitle = alias.Name
				break
			}
		}
	}

	// Use the English title as the primary name if found
	displayName := details.Name
	if englishTitle != "" {
		displayName = englishTitle
	}

	// 5.5 Update DB with official metadata
	updateQuery := `
		UPDATE shows
		SET title = $1, year = $2, tvdb_id = $3, tmdb_id = $4, imdb_id = $5, overview = $6, poster_path = $7, genres = $8, status = 'matched', raw_metadata = $9, updated_at = CURRENT_TIMESTAMP
		WHERE id = $10
	`
	matchedYear := sh.Year
	if len(details.FirstAired) >= 4 {
		if year, err := strconv.Atoi(details.FirstAired[:4]); err == nil {
			matchedYear = year
		}
	}

	_, err = s.db.Exec(updateQuery, displayName, matchedYear, fmt.Sprintf("%d", details.ID), finalTMDBID, finalIMDBID, details.Overview, details.Image, genreString, rawMetadata, sh.ID)
	if err != nil {
		slog.Error("Error updating DB for show", "title", sh.Title, "error", err)
		return err
	}

	// Update any pending/downloading requests for this show with the English title
	if englishTitle != "" && englishTitle != details.Name {
		slog.Info("Updating related requests with English title",
			"tvdb_id", matchedTVDBID,
			"primary_title", details.Name,
			"english_title", englishTitle)

		_, err = s.db.Exec(`
			UPDATE requests
			SET original_title = $1, updated_at = CURRENT_TIMESTAMP
			WHERE tvdb_id = $2
			AND media_type = 'show'
			AND status IN ('pending', 'downloading')
			AND (original_title IS NULL OR original_title = '')`,
			englishTitle, matchedTVDBID)
		if err != nil {
			slog.Warn("Failed to update requests with English title", "tvdb_id", matchedTVDBID, "error", err)
		} else {
			slog.Info("Successfully updated requests with English title",
				"tvdb_id", matchedTVDBID,
				"english_title", englishTitle)
		}
	}

	// Update any pending/downloading requests for this show with the English title
	if englishTitle != "" && englishTitle != details.Name {
		slog.Info("Updating related requests with English title",
			"tvdb_id", matchedTVDBID,
			"primary_title", details.Name,
			"english_title", englishTitle)

		_, err = s.db.Exec(`
			UPDATE requests
			SET original_title = $1, updated_at = CURRENT_TIMESTAMP
			WHERE tvdb_id = $2
			AND media_type = 'show'
			AND status IN ('pending', 'downloading')
			AND (original_title IS NULL OR original_title = '')`,
			englishTitle, matchedTVDBID)
		if err != nil {
			slog.Warn("Failed to update requests with English title", "tvdb_id", matchedTVDBID, "error", err)
		} else {
			slog.Info("Successfully updated requests with English title",
				"tvdb_id", matchedTVDBID,
				"english_title", englishTitle)
		}
	}

	// 6. Sync episode titles from TVDB
	s.SyncShowEpisodes(sh.ID)

	return nil
}

func (s *MetadataService) SearchTVDBByRemoteID(remoteID string) ([]SearchResult, error) {
	if s.cfg.TVDBAPIKey == "" {
		return nil, fmt.Errorf("TVDB_API_KEY is not set")
	}

	token, err := s.getTVDBToken()
	if err != nil {
		return nil, err
	}

	s.throttle()
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

func (s *MetadataService) SyncShowEpisodes(showID int) error {
	var tvdbID string
	err := s.db.QueryRow("SELECT tvdb_id FROM shows WHERE id = $1", showID).Scan(&tvdbID)
	if err != nil || tvdbID == "" {
		return fmt.Errorf("show has no TVDB ID")
	}

	episodes, err := s.GetTVDBShowEpisodes(tvdbID)
	if err != nil {
		return err
	}

	// 1. First, populate the tvdb_episodes cache table for quick lookup during scans/renames
	for _, ep := range episodes {
		query := `
			INSERT INTO tvdb_episodes (show_id, season_number, episode_number, name, overview, aired)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (show_id, season_number, episode_number) DO UPDATE SET
				name = EXCLUDED.name,
				overview = EXCLUDED.overview,
				aired = EXCLUDED.aired
		`
		s.db.Exec(query, showID, ep.SeasonNumber, ep.Number, ep.Name, ep.Overview, ep.Aired)
	}

	// 2. Then, update existing episodes in the episodes table with official titles
	for _, ep := range episodes {
		query := `
			UPDATE episodes
			SET title = $1
			WHERE season_id IN (SELECT id FROM seasons WHERE show_id = $2 AND season_number = $3)
			AND episode_number = $4
		`
		s.db.Exec(query, ep.Name, showID, ep.SeasonNumber, ep.Number)
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

func (s *MetadataService) FetchMetadataForAllDiscovered() {
	slog.Info("Starting background metadata fetching")
	// Movies
	movieQuery := `SELECT id FROM movies WHERE status = 'discovered'`
	movieRows, err := s.db.Query(movieQuery)
	if err == nil {
		defer movieRows.Close()
		for movieRows.Next() {
			var id int
			if err := movieRows.Scan(&id); err == nil {
				s.MatchMovie(id)
			}
		}
	}

	// Shows
	showQuery := `SELECT id FROM shows WHERE status = 'discovered'`
	showRows, err := s.db.Query(showQuery)
	if err == nil {
		defer showRows.Close()
		for showRows.Next() {
			var id int
			if err := showRows.Scan(&id); err == nil {
				s.MatchShow(id)
			}
		}
	}
	slog.Info("Background metadata fetching complete")
}

// GetMovieAlternatives searches TMDB for alternative matches for a movie
// Returns up to 10 results
func (s *MetadataService) GetMovieAlternatives(movieID int) ([]SearchResult, error) {
	var m models.Movie
	var tmdbID sql.NullString
	query := `SELECT id, title, year, tmdb_id FROM movies WHERE id = $1`
	err := s.db.QueryRow(query, movieID).Scan(&m.ID, &m.Title, &m.Year, &tmdbID)
	if err != nil {
		return nil, err
	}

	if s.cfg.TMDBAPIKey == "" {
		return nil, fmt.Errorf("TMDB_API_KEY is not set")
	}

	// Clean the title before searching
	cleanedTitle := cleanTitleTags(m.Title)
	slog.Info("Searching TMDB for alternatives", "title", cleanedTitle, "original_title", m.Title, "year", m.Year)

	s.throttle()
	params := map[string]string{
		"api_key":  s.cfg.TMDBAPIKey,
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
func (s *MetadataService) GetShowAlternatives(showID int) ([]SearchResult, error) {
	var sh models.Show
	var tvdbID sql.NullString
	query := `SELECT id, title, year, tvdb_id FROM shows WHERE id = $1`
	err := s.db.QueryRow(query, showID).Scan(&sh.ID, &sh.Title, &sh.Year, &tvdbID)
	if err != nil {
		return nil, err
	}

	if s.cfg.TVDBAPIKey == "" {
		return nil, fmt.Errorf("TVDB_API_KEY is not set")
	}

	// Clean the title before searching
	cleanedTitle := cleanTitleTags(sh.Title)
	slog.Info("Searching TVDB for alternatives", "title", cleanedTitle, "original_title", sh.Title, "year", sh.Year)

	results, err := s.SearchTVDB(cleanedTitle)
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

// updateRequestFromMatchedMovie updates the request with proper metadata from a matched movie
// This ensures requests show the correct title, poster, and other metadata instead of the raw filename
func (s *MetadataService) updateRequestFromMatchedMovie(movieID int, title string, year int, tmdbID string, imdbID string, overview string, posterPath string) {
	// Get the torrent hash from the movie
	var torrentHash sql.NullString
	err := s.db.QueryRow("SELECT torrent_hash FROM movies WHERE id = $1", movieID).Scan(&torrentHash)
	if err != nil || !torrentHash.Valid || torrentHash.String == "" {
		// No torrent hash linked, so no request to update
		return
	}

	// Find the request_id via the downloads table
	var requestID int
	err = s.db.QueryRow(`
		SELECT request_id
		FROM downloads
		WHERE LOWER(torrent_hash) = LOWER($1)
		AND request_id IS NOT NULL
		LIMIT 1
	`, torrentHash.String).Scan(&requestID)
	if err != nil {
		// No request found for this torrent hash
		return
	}

	// Update the request with proper metadata
	updateQuery := `
		UPDATE requests
		SET title = $1, year = $2, tmdb_id = $3, imdb_id = $4, overview = $5, poster_path = $6, updated_at = CURRENT_TIMESTAMP
		WHERE id = $7 AND media_type = 'movie'
	`
	_, err = s.db.Exec(updateQuery, title, year, tmdbID, imdbID, overview, posterPath, requestID)
	if err != nil {
		slog.Error("Error updating request with matched movie metadata", "request_id", requestID, "movie_id", movieID, "error", err)
		return
	}

	slog.Info("Updated request with matched movie metadata", "request_id", requestID, "movie_id", movieID, "title", title)
}

func (s *MetadataService) RematchMovie(movieID int, newTMDBID string) error {
	// Fetch full details for the new TMDB ID
	details, err := s.GetTMDBMovieDetails(newTMDBID)
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

	_, err = s.db.Exec(updateQuery, details.Title, matchedYear, fmt.Sprintf("%d", details.ID), details.IMDBID, details.Overview, details.PosterPath, genreString, rawMetadata, movieID)
	if err != nil {
		slog.Error("Error updating DB for movie rematch", "movie_id", movieID, "error", err)
		return err
	}

	// Update the request with proper metadata if this movie is linked to a request via torrent hash
	s.updateRequestFromMatchedMovie(movieID, details.Title, matchedYear, fmt.Sprintf("%d", details.ID), details.IMDBID, details.Overview, details.PosterPath)

	slog.Info("Movie rematched successfully", "movie_id", movieID, "new_tmdb_id", newTMDBID)
	return nil
}

// RematchShow updates a show with a new TVDB ID and fetches full metadata
func (s *MetadataService) RematchShow(showID int, newTVDBID string) error {
	// Fetch full details for the new TVDB ID
	details, err := s.GetTVDBShowDetails(newTVDBID)
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

	// Extract English title from translations
	englishTitle := ""
	for _, trans := range details.Translations.NameTranslations {
		if trans.Language == "eng" && trans.Name != "" {
			englishTitle = trans.Name
			break
		}
	}

	// Fallback to aliases
	if englishTitle == "" {
		for _, alias := range details.Aliases {
			if alias.Language == "eng" && alias.Name != "" {
				englishTitle = alias.Name
				break
			}
		}
	}

	// Use English title as primary if found
	displayName := details.Name
	if englishTitle != "" {
		displayName = englishTitle
	}

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

	_, err = s.db.Exec(updateQuery, displayName, matchedYear, fmt.Sprintf("%d", details.ID), finalTMDBID, finalIMDBID, details.Overview, details.Image, genreString, rawMetadata, showID)
	if err != nil {
		slog.Error("Error updating DB for show rematch", "show_id", showID, "error", err)
		return err
	}

	// Sync episode titles from TVDB
	s.SyncShowEpisodes(showID)

	slog.Info("Show rematched successfully", "show_id", showID, "new_tvdb_id", newTVDBID)
	return nil
}
