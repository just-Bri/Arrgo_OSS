package services

import (
	"Arrgo/config"
	"Arrgo/database"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	sharedhttp "github.com/justbri/arrgo/shared/http"
)

type OpenSubtitlesError struct {
	Message      string `json:"message"`
	ResetTimeUTC string `json:"reset_time_utc"`
	Status       int    `json:"-"`
}

func (e OpenSubtitlesError) Error() string {
	return fmt.Sprintf("opensubtitles download request failed (%d): %s", e.Status, e.Message)
}

func parseOpenSubtitlesError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)
	var osErr OpenSubtitlesError

	// Try parsing as JSON first
	if err := json.Unmarshal(body, &osErr); err == nil {
		osErr.Status = resp.StatusCode
		if resp.StatusCode == 406 && osErr.ResetTimeUTC != "" {
			// Store quota reset time
			if t, err := time.Parse(time.RFC3339, osErr.ResetTimeUTC); err == nil {
				SetSetting("opensubtitles_quota_reset", t.Format(time.RFC3339))
			}
		}
		return osErr
	}

	// Fallback: Try to extract reset time from plain text error message
	// Example: "... Your quota will be renewed in 04 hours and 57 minutes (2026-01-20 23:59:59 UTC) ts=1768935729"
	if resp.StatusCode == 406 {
		re := regexp.MustCompile(`\((\d{4}-\d{2}-\d{2}\s\d{2}:\d{2}:\d{2})\sUTC\)`)
		if matches := re.FindStringSubmatch(bodyStr); len(matches) > 1 {
			// Parse "2026-01-20 23:59:59"
			layout := "2006-01-02 15:04:05"
			if t, err := time.Parse(layout, matches[1]); err == nil {
				// Convert to UTC and store
				utcTime := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.UTC)
				SetSetting("opensubtitles_quota_reset", utcTime.Format(time.RFC3339))
				slog.Debug("Extracted quota reset time from text error", "reset_time", utcTime.Format(time.RFC3339))
			}
		}
	}

	return fmt.Errorf("opensubtitles request failed (%d): %s", resp.StatusCode, bodyStr)
}

func SetSetting(key, value string) error {
	_, err := database.DB.Exec(`
		INSERT INTO settings (key, value, updated_at)
		VALUES ($1, $2, CURRENT_TIMESTAMP)
		ON CONFLICT (key) DO UPDATE SET value = $2, updated_at = CURRENT_TIMESTAMP`,
		key, value)
	return err
}

func QueueSubtitleDownload(mediaType string, mediaID int) error {
	// If we have a reset time, use it + 5 minutes. Otherwise use now.
	nextRetry := time.Now()
	var resetStr string
	err := database.DB.QueryRow("SELECT value FROM settings WHERE key = 'opensubtitles_quota_reset'").Scan(&resetStr)
	if err == nil {
		if t, err := time.Parse(time.RFC3339, resetStr); err == nil {
			if t.After(nextRetry) {
				nextRetry = t.Add(5 * time.Minute)
			}
		}
	}

	_, err = database.DB.Exec(`
		INSERT INTO subtitle_queue (media_type, media_id, next_retry)
		VALUES ($1, $2, $3)
		ON CONFLICT (media_type, media_id) DO UPDATE SET next_retry = $3`,
		mediaType, mediaID, nextRetry)
	return err
}

func IsQuotaLocked() bool {
	var resetStr string
	err := database.DB.QueryRow("SELECT value FROM settings WHERE key = 'opensubtitles_quota_reset'").Scan(&resetStr)
	if err != nil {
		return false
	}
	if t, err := time.Parse(time.RFC3339, resetStr); err == nil {
		return time.Now().Before(t.Add(5 * time.Minute))
	}
	return false
}

var (
	osToken       string
	osBaseURL     string
	osTokenExpiry time.Time
	osMutex       sync.Mutex

	// Rate limiting for OpenSubtitles
	lastOSRequestTime time.Time
	osRateLimitMutex  sync.Mutex

	// Rate limiting semaphore to ensure we don't overload OpenSubtitles
	osSemaphore = make(chan struct{}, 1)
)

func osThrottle() {
	osRateLimitMutex.Lock()
	defer osRateLimitMutex.Unlock()

	elapsed := time.Since(lastOSRequestTime)
	if elapsed < 200*time.Millisecond {
		time.Sleep(200*time.Millisecond - elapsed)
	}
	lastOSRequestTime = time.Now()
}

func doRequestWithRetry(client *http.Client, reqFunc func() (*http.Request, error)) (*http.Response, error) {
	var lastResp *http.Response
	var lastErr error

	for i := range 3 {
		req, err := reqFunc()
		if err != nil {
			return nil, err
		}

		osThrottle()
		resp, err := client.Do(req)
		if err == nil {
			if resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusServiceUnavailable {
				slog.Warn("Hit error, retrying", "status_code", resp.StatusCode, "attempt", i+1, "max_attempts", 3)
				resp.Body.Close()
				time.Sleep(10 * time.Second)
				lastResp = resp
				continue
			}
			return resp, nil
		}
		lastErr = err
		slog.Warn("Request failed, retrying", "error", err, "attempt", i+1, "max_attempts", 3)
		time.Sleep(10 * time.Second)
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return lastResp, nil
}

type OSSearchResponse struct {
	TotalCount int `json:"total_count"`
	Data       []struct {
		ID         string `json:"id"`
		Attributes struct {
			SubtitleID      string `json:"subtitle_id"`
			Language        string `json:"language"`
			Release         string `json:"release"`
			HearingImpaired bool   `json:"hearing_impaired"`
			Files           []struct {
				FileID   int    `json:"file_id"`
				FileName string `json:"file_name"`
			} `json:"files"`
		} `json:"attributes"`
	} `json:"data"`
}

type OSDownloadResponse struct {
	Link     string `json:"link"`
	FileName string `json:"file_name"`
}

func getOSToken(cfg *config.Config) (string, string, error) {
	osMutex.Lock()
	defer osMutex.Unlock()

	if osToken != "" && time.Now().Before(osTokenExpiry) {
		return osToken, osBaseURL, nil
	}

	if cfg.OpenSubtitlesUser == "" || cfg.OpenSubtitlesPass == "" {
		return "", "", fmt.Errorf("OpenSubtitles credentials not set")
	}

	slog.Info("Authenticating with OpenSubtitles")
	payload, _ := json.Marshal(map[string]string{
		"username": cfg.OpenSubtitlesUser,
		"password": cfg.OpenSubtitlesPass,
	})

	resp, err := doRequestWithRetry(sharedhttp.DefaultClient, func() (*http.Request, error) {
		req, _ := http.NewRequest("POST", "https://api.opensubtitles.com/api/v1/login", bytes.NewBuffer(payload))
		req.Header.Set("Api-Key", cfg.OpenSubtitlesAPIKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "Arrgo v1.0")
		return req, nil
	})
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("OpenSubtitles login failed (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Token   string `json:"token"`
		BaseURL string `json:"base_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", err
	}

	osToken = result.Token
	osBaseURL = result.BaseURL
	if osBaseURL == "" {
		osBaseURL = "api.opensubtitles.com"
	}
	osTokenExpiry = time.Now().Add(23 * time.Hour) // Tokens usually last 24h
	return osToken, osBaseURL, nil
}

func DownloadSubtitlesForMovie(cfg *config.Config, movieID int) error {
	if cfg.OpenSubtitlesAPIKey == "" {
		return nil
	}

	if IsQuotaLocked() {
		slog.Info("OpenSubtitles quota is locked (pre-check), queueing movie", "movie_id", movieID)
		return QueueSubtitleDownload("movie", movieID)
	}

	var imdbID, tmdbID, title, videoPath string
	var year int
	err := database.DB.QueryRow("SELECT imdb_id, tmdb_id, title, year, path FROM movies WHERE id = $1", movieID).Scan(&imdbID, &tmdbID, &title, &year, &videoPath)
	if err != nil {
		return fmt.Errorf("failed to fetch movie info for subtitle download: %w", err)
	}

	// Wait for our turn
	osSemaphore <- struct{}{}
	defer func() {
		// Small cooldown after each API interaction
		time.Sleep(1 * time.Second)
		<-osSemaphore
	}()

	// Re-check quota after entering semaphore to catch race conditions
	if IsQuotaLocked() {
		slog.Info("OpenSubtitles quota was locked while waiting for semaphore, queueing movie", "movie_id", movieID)
		return QueueSubtitleDownload("movie", movieID)
	}

	if imdbID == "" {
		slog.Info("No IMDB ID for movie, skipping subtitle search", "title", title, "movie_id", movieID)
		return nil
	}

	// Remove 'tt' prefix if present
	imdbID = strings.TrimPrefix(imdbID, "tt")

	slog.Info("Searching subtitles for movie", "title", title, "imdb_id", imdbID)

	// 1. Search for English subtitles
	searchURL := sharedhttp.BuildQueryURL("https://api.opensubtitles.com/api/v1/subtitles", map[string]string{
		"imdb_id":          imdbID,
		"languages":        "en",
		"hearing_impaired": "include",
		"order_by":         "votes",
	})
	resp, err := doRequestWithRetry(sharedhttp.DefaultClient, func() (*http.Request, error) {
		req, _ := http.NewRequest("GET", searchURL, nil)
		req.Header.Set("Api-Key", cfg.OpenSubtitlesAPIKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "Arrgo v1.0")
		return req, nil
	})
	if err != nil {
		return fmt.Errorf("failed to search subtitles: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err := parseOpenSubtitlesError(resp)
		if resp.StatusCode == 406 {
			QueueSubtitleDownload("movie", movieID)
		}
		return err
	}

	var searchResult OSSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResult); err != nil {
		return fmt.Errorf("failed to decode search results: %w", err)
	}

	if len(searchResult.Data) == 0 {
		slog.Info("No English subtitles found for movie", "title", title)
		return nil
	}

	// 2. Find the best match (prioritize hearing impaired if available)
	var bestMatch *struct {
		ID         string `json:"id"`
		Attributes struct {
			SubtitleID      string `json:"subtitle_id"`
			Language        string `json:"language"`
			Release         string `json:"release"`
			HearingImpaired bool   `json:"hearing_impaired"`
			Files           []struct {
				FileID   int    `json:"file_id"`
				FileName string `json:"file_name"`
			} `json:"files"`
		} `json:"attributes"`
	}

	for _, d := range searchResult.Data {
		// If we find one that is hearing impaired, take it immediately if it's the first one or better
		if d.Attributes.HearingImpaired {
			bestMatch = &d
			break
		}
	}

	// Fallback to the first one if no hearing impaired found
	if bestMatch == nil {
		bestMatch = &searchResult.Data[0]
	}

	fileID := bestMatch.Attributes.Files[0].FileID
	isSDH := bestMatch.Attributes.HearingImpaired

	slog.Info("Found subtitle, downloading", "title", title, "sdh", isSDH, "file_id", fileID)

	token, baseURL, err := getOSToken(cfg)
	if err != nil {
		slog.Warn("Failed to get auth token, download might fail", "error", err)
		baseURL = "api.opensubtitles.com"
	}

	downloadPayload := map[string]int{"file_id": fileID}
	payloadBytes, _ := json.Marshal(downloadPayload)

	downloadURL := fmt.Sprintf("https://%s/api/v1/download", baseURL)
	downloadResp, err := doRequestWithRetry(sharedhttp.DefaultClient, func() (*http.Request, error) {
		downloadReq, _ := http.NewRequest("POST", downloadURL, bytes.NewBuffer(payloadBytes))
		downloadReq.Header.Set("Api-Key", cfg.OpenSubtitlesAPIKey)
		downloadReq.Header.Set("Content-Type", "application/json")
		downloadReq.Header.Set("Accept", "application/json")
		downloadReq.Header.Set("User-Agent", "Arrgo v1.0")
		if token != "" {
			downloadReq.Header.Set("Authorization", "Bearer "+token)
		}
		return downloadReq, nil
	})
	if err != nil {
		return fmt.Errorf("failed to request download link: %w", err)
	}
	defer downloadResp.Body.Close()

	if downloadResp.StatusCode != http.StatusOK {
		err := parseOpenSubtitlesError(downloadResp)
		if downloadResp.StatusCode == 406 {
			QueueSubtitleDownload("movie", movieID)
		}
		return err
	}

	var downloadInfo OSDownloadResponse
	if err := json.NewDecoder(downloadResp.Body).Decode(&downloadInfo); err != nil {
		return fmt.Errorf("failed to decode download info: %w", err)
	}

	// 3. Download the actual file
	fileResp, err := doRequestWithRetry(sharedhttp.DefaultClient, func() (*http.Request, error) {
		fileReq, _ := http.NewRequest("GET", downloadInfo.Link, nil)
		fileReq.Header.Set("User-Agent", "Arrgo v1.0")
		return fileReq, nil
	})
	if err != nil {
		return fmt.Errorf("failed to download subtitle file: %w", err)
	}
	defer fileResp.Body.Close()

	// Plex naming convention: Match the video filename but with .en.srt or .en.sdh.srt
	base := strings.TrimSuffix(videoPath, filepath.Ext(videoPath))
	suffix := ".en"
	if isSDH {
		suffix = ".en.sdh"
	}
	destPath := base + suffix + ".srt"

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create subtitle file: %w", err)
	}
	defer out.Close()

	_, err = io.Copy(out, fileResp.Body)
	if err != nil {
		return fmt.Errorf("failed to save subtitle file: %w", err)
	}

	slog.Info("Successfully downloaded subtitle for movie", "title", title, "dest_path", destPath)
	return nil
}

func DownloadSubtitlesForEpisode(cfg *config.Config, episodeID int) error {
	if cfg.OpenSubtitlesAPIKey == "" {
		return nil
	}

	if IsQuotaLocked() {
		slog.Info("OpenSubtitles quota is locked (pre-check), queueing episode", "episode_id", episodeID)
		return QueueSubtitleDownload("episode", episodeID)
	}

	var imdbID, showTitle, videoPath string
	var season, episode int
	query := `
		SELECT sh.imdb_id, sh.title, s.season_number, e.episode_number, e.file_path
		FROM episodes e
		JOIN seasons s ON e.season_id = s.id
		JOIN shows sh ON s.show_id = sh.id
		WHERE e.id = $1
	`
	err := database.DB.QueryRow(query, episodeID).Scan(&imdbID, &showTitle, &season, &episode, &videoPath)
	if err != nil {
		return fmt.Errorf("failed to fetch episode info for subtitle download: %w", err)
	}

	// Wait for our turn
	osSemaphore <- struct{}{}
	defer func() {
		// Small cooldown after each API interaction
		time.Sleep(1 * time.Second)
		<-osSemaphore
	}()

	// Re-check quota after entering semaphore to catch race conditions
	if IsQuotaLocked() {
		slog.Info("OpenSubtitles quota was locked while waiting for semaphore, queueing episode", "episode_id", episodeID)
		return QueueSubtitleDownload("episode", episodeID)
	}

	if imdbID == "" {
		slog.Debug("No parent IMDB ID for show, skipping subtitle search", "show_title", showTitle, "episode_id", episodeID)
		return nil
	}

	imdbID = strings.TrimPrefix(imdbID, "tt")

	slog.Info("Searching subtitles for episode", "show_title", showTitle, "season", season, "episode", episode, "imdb_id", imdbID)

	searchURL := sharedhttp.BuildQueryURL("https://api.opensubtitles.com/api/v1/subtitles", map[string]string{
		"parent_imdb_id":   imdbID,
		"season_number":    fmt.Sprintf("%d", season),
		"episode_number":   fmt.Sprintf("%d", episode),
		"languages":        "en",
		"hearing_impaired": "include",
		"order_by":         "votes",
	})

	resp, err := doRequestWithRetry(sharedhttp.DefaultClient, func() (*http.Request, error) {
		req, _ := http.NewRequest("GET", searchURL, nil)
		req.Header.Set("Api-Key", cfg.OpenSubtitlesAPIKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "Arrgo v1.0")
		return req, nil
	})
	if err != nil {
		return fmt.Errorf("failed to search subtitles: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err := parseOpenSubtitlesError(resp)
		if resp.StatusCode == 406 {
			QueueSubtitleDownload("episode", episodeID)
		}
		return err
	}

	var searchResult OSSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResult); err != nil {
		return fmt.Errorf("failed to decode search results: %w", err)
	}

	if len(searchResult.Data) == 0 {
		slog.Info("No English subtitles found for episode", "show_title", showTitle, "season", season, "episode", episode)
		return nil
	}

	// 2. Find the best match (prioritize hearing impaired if available)
	var bestMatch *struct {
		ID         string `json:"id"`
		Attributes struct {
			SubtitleID      string `json:"subtitle_id"`
			Language        string `json:"language"`
			Release         string `json:"release"`
			HearingImpaired bool   `json:"hearing_impaired"`
			Files           []struct {
				FileID   int    `json:"file_id"`
				FileName string `json:"file_name"`
			} `json:"files"`
		} `json:"attributes"`
	}

	for _, d := range searchResult.Data {
		if d.Attributes.HearingImpaired {
			bestMatch = &d
			break
		}
	}

	if bestMatch == nil {
		bestMatch = &searchResult.Data[0]
	}

	fileID := bestMatch.Attributes.Files[0].FileID
	isSDH := bestMatch.Attributes.HearingImpaired

	token, baseURL, err := getOSToken(cfg)
	if err != nil {
		slog.Warn("Failed to get auth token, download might fail", "error", err)
		baseURL = "api.opensubtitles.com"
	}

	downloadPayload := map[string]int{"file_id": fileID}
	payloadBytes, _ := json.Marshal(downloadPayload)

	downloadURL := fmt.Sprintf("https://%s/api/v1/download", baseURL)
	downloadResp, err := doRequestWithRetry(sharedhttp.DefaultClient, func() (*http.Request, error) {
		downloadReq, _ := http.NewRequest("POST", downloadURL, bytes.NewBuffer(payloadBytes))
		downloadReq.Header.Set("Api-Key", cfg.OpenSubtitlesAPIKey)
		downloadReq.Header.Set("Content-Type", "application/json")
		downloadReq.Header.Set("Accept", "application/json")
		downloadReq.Header.Set("User-Agent", "Arrgo v1.0")
		if token != "" {
			downloadReq.Header.Set("Authorization", "Bearer "+token)
		}
		return downloadReq, nil
	})
	if err != nil {
		return fmt.Errorf("failed to request download link: %w", err)
	}
	defer downloadResp.Body.Close()

	if downloadResp.StatusCode != http.StatusOK {
		err := parseOpenSubtitlesError(downloadResp)
		if downloadResp.StatusCode == 406 {
			QueueSubtitleDownload("episode", episodeID)
		}
		return err
	}

	var downloadInfo OSDownloadResponse
	if err := json.NewDecoder(downloadResp.Body).Decode(&downloadInfo); err != nil {
		return fmt.Errorf("failed to decode download info: %w", err)
	}

	// 3. Download the actual file
	fileResp, err := doRequestWithRetry(sharedhttp.DefaultClient, func() (*http.Request, error) {
		fileReq, _ := http.NewRequest("GET", downloadInfo.Link, nil)
		fileReq.Header.Set("User-Agent", "Arrgo v1.0")
		return fileReq, nil
	})
	if err != nil {
		return fmt.Errorf("failed to download subtitle file: %w", err)
	}
	defer fileResp.Body.Close()

	// Plex naming convention: Match the video filename but with .en.srt or .en.sdh.srt
	base := strings.TrimSuffix(videoPath, filepath.Ext(videoPath))
	suffix := ".en"
	if isSDH {
		suffix = ".en.sdh"
	}
	destPath := base + suffix + ".srt"

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create subtitle file: %w", err)
	}
	defer out.Close()

	_, err = io.Copy(out, fileResp.Body)
	if err != nil {
		return fmt.Errorf("failed to save subtitle file: %w", err)
	}

	slog.Info("Successfully downloaded subtitle for episode", "show_title", showTitle, "season", season, "episode", episode, "dest_path", destPath)
	return nil
}

func HasSubtitles(videoPath string) bool {
	if videoPath == "" {
		return false
	}

	base := strings.TrimSuffix(videoPath, filepath.Ext(videoPath))

	// Check for common Plex/Arrgo subtitle patterns
	checks := []string{
		base + ".en.srt",
		base + ".en.sdh.srt",
		base + ".eng.srt",
		base + ".eng.sdh.srt",
	}

	for _, c := range checks {
		if _, err := os.Stat(c); err == nil {
			return true
		}
	}

	// Also check for a simple .srt if there's only one in the folder (loose check)
	dir := filepath.Dir(videoPath)
	files, err := os.ReadDir(dir)
	if err == nil {
		videoBase := filepath.Base(base)
		for _, f := range files {
			if !f.IsDir() {
				name := strings.ToLower(f.Name())
				if strings.HasSuffix(name, ".srt") && strings.Contains(name, strings.ToLower(videoBase)) {
					if strings.Contains(name, ".en") || strings.Contains(name, ".eng") {
						return true
					}
				}
			}
		}
	}

	return false
}

type SubtitleScanResult struct {
	TotalMovies      int `json:"total_movies"`
	MoviesWithSubs   int `json:"movies_with_subs"`
	MoviesMissing    int `json:"movies_missing"`
	TotalEpisodes    int `json:"total_episodes"`
	EpisodesWithSubs int `json:"episodes_with_subs"`
	EpisodesMissing  int `json:"episodes_missing"`
}

const (
	subtitleScanCacheKey = "subtitle_scan_cache"
	cacheTTL             = 24 * time.Hour
)

// getCachedScanResult retrieves cached scan results if they exist and are still valid
func getCachedScanResult() (*SubtitleScanResult, bool) {
	var value string
	var updatedAt time.Time
	err := database.DB.QueryRow(
		"SELECT value, updated_at FROM settings WHERE key = $1",
		subtitleScanCacheKey,
	).Scan(&value, &updatedAt)
	if err != nil {
		return nil, false
	}

	// Check if cache is still valid (less than 24 hours old)
	if time.Since(updatedAt) > cacheTTL {
		return nil, false
	}

	// Parse JSON result
	var result SubtitleScanResult
	if err := json.Unmarshal([]byte(value), &result); err != nil {
		slog.Warn("Failed to parse cached scan result", "error", err)
		return nil, false
	}

	return &result, true
}

// cacheScanResult stores scan results in the database
func cacheScanResult(result *SubtitleScanResult) error {
	jsonData, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal scan result: %w", err)
	}

	return SetSetting(subtitleScanCacheKey, string(jsonData))
}

// ScanAllMediaForSubtitles scans all movies and episodes to check if they have subtitles
// Results are cached for 24 hours to avoid repeated scans
// Set forceRefresh to true to bypass cache and perform a new scan
func ScanAllMediaForSubtitles(forceRefresh bool) (*SubtitleScanResult, error) {
	// Check cache first (unless forcing refresh)
	if !forceRefresh {
		if cached, ok := getCachedScanResult(); ok {
			slog.Info("Returning cached subtitle scan results")
			return cached, nil
		}
	} else {
		slog.Info("Force refresh requested, bypassing cache")
	}

	slog.Info("Performing new subtitle scan (cache expired or missing)")
	result := &SubtitleScanResult{}

	// Scan movies
	movies, err := GetMovies()
	if err != nil {
		return nil, fmt.Errorf("failed to get movies: %w", err)
	}

	result.TotalMovies = len(movies)
	for _, movie := range movies {
		if movie.Path != "" {
			if HasSubtitles(movie.Path) {
				result.MoviesWithSubs++
			} else {
				result.MoviesMissing++
			}
		}
	}

	// Scan episodes
	episodeQuery := `SELECT id, file_path FROM episodes`
	rows, err := database.DB.Query(episodeQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to get episodes: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var episodeID int
		var filePath string
		if err := rows.Scan(&episodeID, &filePath); err != nil {
			continue
		}

		result.TotalEpisodes++
		if filePath != "" {
			if HasSubtitles(filePath) {
				result.EpisodesWithSubs++
			} else {
				result.EpisodesMissing++
			}
		}
	}

	// Cache the results
	if err := cacheScanResult(result); err != nil {
		slog.Warn("Failed to cache scan results", "error", err)
		// Don't fail the request if caching fails
	}

	return result, nil
}

// QueueMissingSubtitles queues all movies and episodes that are missing subtitles
func QueueMissingSubtitles() (int, error) {
	queuedCount := 0

	// Queue missing movie subtitles
	movies, err := GetMovies()
	if err != nil {
		return 0, fmt.Errorf("failed to get movies: %w", err)
	}

	for _, movie := range movies {
		if movie.Path != "" && !HasSubtitles(movie.Path) {
			if err := QueueSubtitleDownload("movie", movie.ID); err != nil {
				slog.Warn("Failed to queue subtitle for movie", "movie_id", movie.ID, "error", err)
			} else {
				queuedCount++
			}
		}
	}

	// Queue missing episode subtitles
	episodeQuery := `SELECT id, file_path FROM episodes`
	rows, err := database.DB.Query(episodeQuery)
	if err != nil {
		return queuedCount, fmt.Errorf("failed to get episodes: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var episodeID int
		var filePath string
		if err := rows.Scan(&episodeID, &filePath); err != nil {
			continue
		}

		if filePath != "" && !HasSubtitles(filePath) {
			if err := QueueSubtitleDownload("episode", episodeID); err != nil {
				slog.Warn("Failed to queue subtitle for episode", "episode_id", episodeID, "error", err)
			} else {
				queuedCount++
			}
		}
	}

	return queuedCount, nil
}
