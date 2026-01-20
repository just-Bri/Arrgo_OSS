package services

import (
	"Arrgo/config"
	"Arrgo/database"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
				log.Printf("[SUBTITLES] Extracted quota reset time from text error: %s", utcTime.Format(time.RFC3339))
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

func doRequestWithRetry(cfg *config.Config, client *http.Client, reqFunc func() (*http.Request, error)) (*http.Response, error) {
	var lastResp *http.Response
	var lastErr error

	for i := 0; i < 3; i++ {
		req, err := reqFunc()
		if err != nil {
			return nil, err
		}

		osThrottle()
		resp, err := client.Do(req)
		if err == nil {
			if resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusServiceUnavailable {
				log.Printf("[SUBTITLES] Hit %d error, retrying in 10s (attempt %d/3)...", resp.StatusCode, i+1)
				resp.Body.Close()
				time.Sleep(10 * time.Second)
				lastResp = resp
				continue
			}
			return resp, nil
		}
		lastErr = err
		log.Printf("[SUBTITLES] Request failed: %v, retrying in 10s (attempt %d/3)...", err, i+1)
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

	log.Printf("[SUBTITLES] Authenticating with OpenSubtitles...")
	payload, _ := json.Marshal(map[string]string{
		"username": cfg.OpenSubtitlesUser,
		"password": cfg.OpenSubtitlesPass,
	})

	resp, err := doRequestWithRetry(cfg, sharedhttp.DefaultClient, func() (*http.Request, error) {
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
		log.Printf("[SUBTITLES] OpenSubtitles quota is locked (pre-check), queueing movie %d", movieID)
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
		log.Printf("[SUBTITLES] OpenSubtitles quota was locked while waiting for semaphore, queueing movie %d", movieID)
		return QueueSubtitleDownload("movie", movieID)
	}

	if imdbID == "" {
		log.Printf("[SUBTITLES] No IMDB ID for movie %s, skipping subtitle search", title)
		return nil
	}

	// Remove 'tt' prefix if present
	imdbID = strings.TrimPrefix(imdbID, "tt")

	log.Printf("[SUBTITLES] Searching subtitles for %s (IMDB: %s)...", title, imdbID)

	// 1. Search for English subtitles
	searchURL := sharedhttp.BuildQueryURL("https://api.opensubtitles.com/api/v1/subtitles", map[string]string{
		"imdb_id":          imdbID,
		"languages":        "en",
		"hearing_impaired": "include",
		"order_by":         "votes",
	})
	resp, err := doRequestWithRetry(cfg, sharedhttp.DefaultClient, func() (*http.Request, error) {
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
		log.Printf("[SUBTITLES] No English subtitles found for %s", title)
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

	log.Printf("[SUBTITLES] Found subtitle for %s (SDH: %v), downloading file ID %d...", title, isSDH, fileID)

	token, baseURL, err := getOSToken(cfg)
	if err != nil {
		log.Printf("[SUBTITLES] Warning: Failed to get auth token: %v. Download might fail.", err)
		baseURL = "api.opensubtitles.com"
	}

	downloadPayload := map[string]int{"file_id": fileID}
	payloadBytes, _ := json.Marshal(downloadPayload)

	downloadURL := fmt.Sprintf("https://%s/api/v1/download", baseURL)
	downloadResp, err := doRequestWithRetry(cfg, sharedhttp.DefaultClient, func() (*http.Request, error) {
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
	fileResp, err := doRequestWithRetry(cfg, sharedhttp.DefaultClient, func() (*http.Request, error) {
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

	log.Printf("[SUBTITLES] Successfully downloaded subtitle for %s to %s", title, destPath)
	return nil
}

func DownloadSubtitlesForEpisode(cfg *config.Config, episodeID int) error {
	if cfg.OpenSubtitlesAPIKey == "" {
		return nil
	}

	if IsQuotaLocked() {
		log.Printf("[SUBTITLES] OpenSubtitles quota is locked (pre-check), queueing episode %d", episodeID)
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
		log.Printf("[SUBTITLES] OpenSubtitles quota was locked while waiting for semaphore, queueing episode %d", episodeID)
		return QueueSubtitleDownload("episode", episodeID)
	}

	if imdbID == "" {
		log.Printf("[SUBTITLES] No parent IMDB ID for show %s, skipping subtitle search", showTitle)
		return nil
	}

	imdbID = strings.TrimPrefix(imdbID, "tt")

	log.Printf("[SUBTITLES] Searching subtitles for %s S%02dE%02d (Parent IMDB: %s)...", showTitle, season, episode, imdbID)

	searchURL := sharedhttp.BuildQueryURL("https://api.opensubtitles.com/api/v1/subtitles", map[string]string{
		"parent_imdb_id":   imdbID,
		"season_number":    fmt.Sprintf("%d", season),
		"episode_number":   fmt.Sprintf("%d", episode),
		"languages":        "en",
		"hearing_impaired": "include",
		"order_by":         "votes",
	})

	resp, err := doRequestWithRetry(cfg, sharedhttp.DefaultClient, func() (*http.Request, error) {
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
		log.Printf("[SUBTITLES] No English subtitles found for %s S%02dE%02d", showTitle, season, episode)
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
		log.Printf("[SUBTITLES] Warning: Failed to get auth token: %v. Download might fail.", err)
		baseURL = "api.opensubtitles.com"
	}

	downloadPayload := map[string]int{"file_id": fileID}
	payloadBytes, _ := json.Marshal(downloadPayload)

	downloadURL := fmt.Sprintf("https://%s/api/v1/download", baseURL)
	downloadResp, err := doRequestWithRetry(cfg, sharedhttp.DefaultClient, func() (*http.Request, error) {
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
	fileResp, err := doRequestWithRetry(cfg, sharedhttp.DefaultClient, func() (*http.Request, error) {
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

	log.Printf("[SUBTITLES] Successfully downloaded subtitle for %s S%02dE%02d to %s", showTitle, season, episode, destPath)
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
