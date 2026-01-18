package services

import (
	"Arrgo/config"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	osToken       string
	osBaseURL     string
	osTokenExpiry time.Time
	osMutex       sync.Mutex
)

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

	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest("POST", "https://api.opensubtitles.com/api/v1/login", bytes.NewBuffer(payload))
	req.Header.Set("Api-Key", cfg.OpenSubtitlesAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Arrgo v1.0")

	resp, err := client.Do(req)
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

func DownloadSubtitlesForMovie(cfg *config.Config, imdbID, tmdbID, title string, year int, videoPath string) error {
	if cfg.OpenSubtitlesAPIKey == "" {
		return nil
	}

	if imdbID == "" {
		log.Printf("[SUBTITLES] No IMDB ID for movie %s, skipping subtitle search", title)
		return nil
	}

	// Remove 'tt' prefix if present
	imdbID = strings.TrimPrefix(imdbID, "tt")

	log.Printf("[SUBTITLES] Searching subtitles for %s (IMDB: %s)...", title, imdbID)

	client := &http.Client{Timeout: 10 * time.Second}

	// 1. Search for English subtitles, preferring hearing impaired (SDH)
	searchURL := fmt.Sprintf("https://api.opensubtitles.com/api/v1/subtitles?imdb_id=%s&languages=en&hearing_impaired=include&order_by=votes", imdbID)
	req, _ := http.NewRequest("GET", searchURL, nil)
	req.Header.Set("Api-Key", cfg.OpenSubtitlesAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Arrgo v1.0")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to search subtitles: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("opensubtitles search returned status %d", resp.StatusCode)
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
	downloadReq, _ := http.NewRequest("POST", downloadURL, bytes.NewBuffer(payloadBytes))
	downloadReq.Header.Set("Api-Key", cfg.OpenSubtitlesAPIKey)
	downloadReq.Header.Set("Content-Type", "application/json")
	downloadReq.Header.Set("Accept", "application/json")
	downloadReq.Header.Set("User-Agent", "Arrgo v1.0")
	if token != "" {
		downloadReq.Header.Set("Authorization", "Bearer "+token)
	}

	downloadResp, err := client.Do(downloadReq)
	if err != nil {
		return fmt.Errorf("failed to request download link: %w", err)
	}
	defer downloadResp.Body.Close()

	if downloadResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(downloadResp.Body)
		return fmt.Errorf("opensubtitles download request failed (%d): %s", downloadResp.StatusCode, string(body))
	}

	var downloadInfo OSDownloadResponse
	if err := json.NewDecoder(downloadResp.Body).Decode(&downloadInfo); err != nil {
		return fmt.Errorf("failed to decode download info: %w", err)
	}

	// 3. Download the actual file
	fileReq, _ := http.NewRequest("GET", downloadInfo.Link, nil)
	fileReq.Header.Set("User-Agent", "Arrgo v1.0")
	fileResp, err := client.Do(fileReq)
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

func DownloadSubtitlesForEpisode(cfg *config.Config, imdbID, tmdbID, showTitle string, season, episode int, videoPath string) error {
	if cfg.OpenSubtitlesAPIKey == "" {
		return nil
	}

	if imdbID == "" {
		log.Printf("[SUBTITLES] No parent IMDB ID for show %s, skipping subtitle search", showTitle)
		return nil
	}

	imdbID = strings.TrimPrefix(imdbID, "tt")

	log.Printf("[SUBTITLES] Searching subtitles for %s S%02dE%02d (Parent IMDB: %s)...", showTitle, season, episode, imdbID)

	client := &http.Client{Timeout: 10 * time.Second}

	searchURL := fmt.Sprintf("https://api.opensubtitles.com/api/v1/subtitles?parent_imdb_id=%s&season_number=%d&episode_number=%d&languages=en&hearing_impaired=include&order_by=votes",
		imdbID, season, episode)

	req, _ := http.NewRequest("GET", searchURL, nil)
	req.Header.Set("Api-Key", cfg.OpenSubtitlesAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Arrgo v1.0")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to search subtitles: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("opensubtitles search returned status %d", resp.StatusCode)
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
	downloadReq, _ := http.NewRequest("POST", downloadURL, bytes.NewBuffer(payloadBytes))
	downloadReq.Header.Set("Api-Key", cfg.OpenSubtitlesAPIKey)
	downloadReq.Header.Set("Content-Type", "application/json")
	downloadReq.Header.Set("Accept", "application/json")
	downloadReq.Header.Set("User-Agent", "Arrgo v1.0")
	if token != "" {
		downloadReq.Header.Set("Authorization", "Bearer "+token)
	}

	downloadResp, err := client.Do(downloadReq)
	if err != nil {
		return fmt.Errorf("failed to request download link: %w", err)
	}
	defer downloadResp.Body.Close()

	if downloadResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(downloadResp.Body)
		return fmt.Errorf("opensubtitles download request failed (%d): %s", downloadResp.StatusCode, string(body))
	}

	var downloadInfo OSDownloadResponse
	if err := json.NewDecoder(downloadResp.Body).Decode(&downloadInfo); err != nil {
		return fmt.Errorf("failed to decode download info: %w", err)
	}

	// 3. Download the actual file
	fileReq, _ := http.NewRequest("GET", downloadInfo.Link, nil)
	fileReq.Header.Set("User-Agent", "Arrgo v1.0")
	fileResp, err := client.Do(fileReq)
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
