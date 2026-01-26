package indexers

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/justbri/arrgo/shared/format"
)

type TorrentGalaxyResponse struct {
	Status  string `json:"status"`
	Results []struct {
		Title    string `json:"title"`
		Size     string `json:"size"`
		Seeders  int    `json:"seeders"`
		Leechers int    `json:"leechers"`
		Magnet   string `json:"magnet"`
		InfoHash string `json:"infohash"`
		Category string `json:"category"`
	} `json:"results"`
}

type TorrentGalaxyIndexer struct{}

func NewTorrentGalaxyIndexer() *TorrentGalaxyIndexer {
	return &TorrentGalaxyIndexer{}
}

func (tg *TorrentGalaxyIndexer) GetName() string {
	return "TorrentGalaxy"
}

func (tg *TorrentGalaxyIndexer) SearchMovies(ctx context.Context, query string) ([]SearchResult, error) {
	return tg.search(ctx, query, "Movies")
}

func (tg *TorrentGalaxyIndexer) SearchShows(ctx context.Context, query string, season, episode int) ([]SearchResult, error) {
	// Enhance query with season info if provided
	searchQuery := query
	if season > 0 {
		// Try multiple formats: "Show Name S02" and "Show Name Season 2"
		searchQuery = fmt.Sprintf("%s S%02d", query, season)
	}
	return tg.search(ctx, searchQuery, "TV")
}

func (tg *TorrentGalaxyIndexer) search(ctx context.Context, query string, category string) ([]SearchResult, error) {
	// TorrentGalaxy doesn't have an official public API
	// Options:
	// 1. Use a proxy API service (if available)
	// 2. Use Jackett/Prowlarr which provides Torznab API for TorrentGalaxy
	// 3. Implement HTML scraping (requires more maintenance)

	// Try using a proxy API endpoint if available
	// Note: These endpoints may not be stable - consider using Jackett/Prowlarr instead
	apiURL := BuildQueryURL("https://torrentgalaxy.to/api/search", map[string]string{
		"q":        query,
		"category": category,
	})

	slog.Info("Fetching from TorrentGalaxy", "query", query, "category", category)
	resp, err := MakeHTTPRequest(ctx, apiURL, DefaultHTTPClient)
	if err != nil {
		slog.Warn("TorrentGalaxy request failed", "query", query, "category", category, "error", err)
		// Graceful degradation - return empty results instead of error
		// This allows other indexers to still work
		return []SearchResult{}, nil
	}

	var apiResp TorrentGalaxyResponse
	if err := DecodeJSONResponse(resp, &apiResp); err != nil {
		slog.Warn("TorrentGalaxy decode failed", "query", query, "category", category, "error", err)
		// If JSON decode fails, return empty results (graceful degradation)
		return []SearchResult{}, nil
	}

	if apiResp.Status != "success" {
		slog.Warn("TorrentGalaxy returned non-success status", "query", query, "category", category, "status", apiResp.Status)
		return []SearchResult{}, nil
	}
	
	slog.Info("TorrentGalaxy request successful", "query", query, "category", category, "results", len(apiResp.Results))

	var results []SearchResult
	for _, r := range apiResp.Results {
		// Parse size string to bytes
		sizeBytes := parseSize(r.Size)

		// Extract quality/resolution from title
		quality, resolution := extractQualityInfo(r.Title)

		results = append(results, SearchResult{
			Title:      r.Title,
			Size:       format.Bytes(sizeBytes),
			Seeds:      r.Seeders,
			Peers:      r.Leechers,
			MagnetLink: r.Magnet,
			InfoHash:   r.InfoHash,
			Source:     "TorrentGalaxy",
			Resolution: resolution,
			Quality:    quality,
		})
	}

	return results, nil
}
