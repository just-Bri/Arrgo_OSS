package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

type EZTVResponse struct {
	ImdbID     string `json:"imdb_id"`
	TorrentsCount int    `json:"torrents_count"`
	Limit        int    `json:"limit"`
	Page         int    `json:"page"`
	Torrents     []struct {
		ID            int    `json:"id"`
		Hash          string `json:"hash"`
		Filename      string `json:"filename"`
		EpisodeURL    string `json:"episode_url"`
		TorrentURL    string `json:"torrent_url"`
		MagnetURL     string `json:"magnet_url"`
		Title         string `json:"title"`
		Season        string `json:"season"`
		Episode       string `json:"episode"`
		SizeBytes     string `json:"size_bytes"`
		Seeds         int    `json:"seeds"`
		Peers         int    `json:"peers"`
		DateReleased  int64  `json:"date_released_unix"`
	} `json:"torrents"`
}

type EZTVIndexer struct{}

func (e *EZTVIndexer) GetName() string {
	return "EZTV"
}

func (e *EZTVIndexer) SearchMovies(ctx context.Context, query string) ([]SearchResult, error) {
	// EZTV is for TV shows
	return nil, nil
}

func (e *EZTVIndexer) SearchShows(ctx context.Context, query string, season, episode int) ([]SearchResult, error) {
	// EZTV API search
	// https://eztv.re/api/get-torrents?limit=100&search=...
	
	searchQuery := query
	if season > 0 {
		if episode > 0 {
			searchQuery = fmt.Sprintf("%s S%02dE%02d", query, season, episode)
		} else {
			searchQuery = fmt.Sprintf("%s S%02d", query, season)
		}
	}

	apiURL := fmt.Sprintf("https://eztv.re/api/get-torrents?limit=100&search=%s", url.QueryEscape(searchQuery))
	
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var eztvResp EZTVResponse
	if err := json.NewDecoder(resp.Body).Decode(&eztvResp); err != nil {
		return nil, err
	}

	results := []SearchResult{}
	for _, t := range eztvResp.Torrents {
		sizeBytes, _ := strconv.ParseInt(t.SizeBytes, 10, 64)
		sizeStr := formatSize(sizeBytes)

		results = append(results, SearchResult{
			Title:      t.Title,
			Size:       sizeStr,
			Seeds:      t.Seeds,
			Peers:      t.Peers,
			MagnetLink: t.MagnetURL,
			InfoHash:   t.Hash,
			Source:     "EZTV",
			Resolution: "", // EZTV doesn't always provide this cleanly in a separate field
			Quality:    "",
		})
	}

	return results, nil
}

func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
