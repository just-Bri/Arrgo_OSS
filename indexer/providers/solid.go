package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type SolidTorrentsResponse struct {
	Results []struct {
		Title      string   `json:"title"`
		Size       int64    `json:"size"`
		Seeds      int      `json:"seeds"`
		Leechers   int      `json:"leeches"`
		Magnet     string   `json:"magnet"`
		InfoHash   string   `json:"infohash"`
	} `json:"results"`
}

type SolidTorrentsIndexer struct {
	httpClient *http.Client
}

func NewSolidTorrentsIndexer() *SolidTorrentsIndexer {
	return &SolidTorrentsIndexer{
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (s *SolidTorrentsIndexer) GetName() string {
	return "SolidTorrents"
}

func (s *SolidTorrentsIndexer) SearchMovies(ctx context.Context, query string) ([]SearchResult, error) {
	return s.search(ctx, query, "Video")
}

func (s *SolidTorrentsIndexer) SearchShows(ctx context.Context, query string, season, episode int) ([]SearchResult, error) {
	return s.search(ctx, query, "Video")
}

func (s *SolidTorrentsIndexer) search(ctx context.Context, query string, category string) ([]SearchResult, error) {
	// API: https://solidtorrents.to/api/v1/search?q=...&category=Video&sort=seeders
	apiURL := fmt.Sprintf("https://solidtorrents.to/api/v1/search?q=%s&category=%s&sort=seeders", 
		url.QueryEscape(query), url.QueryEscape(category))

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch results: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("provider returned status %d", resp.StatusCode)
	}

	var apiResp SolidTorrentsResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	var results []SearchResult
	for _, r := range apiResp.Results {
		results = append(results, SearchResult{
			Title:      r.Title,
			Size:       formatBytes(r.Size),
			Seeds:      r.Seeds,
			Peers:      r.Leechers,
			MagnetLink: r.Magnet,
			InfoHash:   r.InfoHash,
			Source:     "Solid",
		})
	}

	return results, nil
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("% d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
