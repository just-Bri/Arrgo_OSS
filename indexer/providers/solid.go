package providers

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/justbri/arrgo/shared/format"
)

type SolidTorrentsResponse struct {
	Results []struct {
		Title    string `json:"title"`
		Size     int64  `json:"size"`
		Seeds    int    `json:"seeds"`
		Leechers int    `json:"leeches"`
		Magnet   string `json:"magnet"`
		InfoHash string `json:"infohash"`
	} `json:"results"`
}

type SolidTorrentsIndexer struct{}

func NewSolidTorrentsIndexer() *SolidTorrentsIndexer {
	return &SolidTorrentsIndexer{}
}

func (s *SolidTorrentsIndexer) GetName() string {
	return "SolidTorrents"
}

func (s *SolidTorrentsIndexer) SearchMovies(ctx context.Context, query string) ([]SearchResult, error) {
	return s.search(ctx, query, "Video")
}

func (s *SolidTorrentsIndexer) SearchShows(ctx context.Context, query string, season, episode int) ([]SearchResult, error) {
	// Enhance query with season info if provided
	searchQuery := query
	if season > 0 {
		// Try multiple formats: "Show Name S02" and "Show Name Season 2"
		searchQuery = fmt.Sprintf("%s S%02d", query, season)
	}
	return s.search(ctx, searchQuery, "Video")
}

func (s *SolidTorrentsIndexer) search(ctx context.Context, query string, category string) ([]SearchResult, error) {
	// API: https://solidtorrents.to/api/v1/search?q=...&category=Video&sort=seeders
	apiURL := BuildQueryURL("https://solidtorrents.to/api/v1/search", map[string]string{
		"q":        query,
		"category": category,
		"sort":     "seeders",
	})

	slog.Info("Fetching from SolidTorrents", "query", query, "category", category)
	resp, err := MakeHTTPRequest(ctx, apiURL, DefaultHTTPClient)
	if err != nil {
		slog.Warn("SolidTorrents request failed", "query", query, "category", category, "error", err)
		return nil, err
	}

	var apiResp SolidTorrentsResponse
	if err := DecodeJSONResponse(resp, &apiResp); err != nil {
		slog.Warn("SolidTorrents decode failed", "query", query, "category", category, "error", err)
		return nil, err
	}
	
	slog.Info("SolidTorrents request successful", "query", query, "category", category, "results", len(apiResp.Results))

	var results []SearchResult
	for _, r := range apiResp.Results {
		results = append(results, SearchResult{
			Title:      r.Title,
			Size:       format.Bytes(r.Size),
			Seeds:      r.Seeds,
			Peers:      r.Leechers,
			MagnetLink: r.Magnet,
			InfoHash:   r.InfoHash,
			Source:     "Solid",
		})
	}

	return results, nil
}
