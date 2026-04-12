package indexers

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/justbri/arrgo/shared/format"
	sharedhttp "github.com/justbri/arrgo/shared/http"
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

func (tg *TorrentGalaxyIndexer) Name() string {
	return "TorrentGalaxy"
}

func (tg *TorrentGalaxyIndexer) SearchMovies(ctx context.Context, query string) ([]SearchResult, error) {
	return tg.search(ctx, query, "Movies")
}

func (tg *TorrentGalaxyIndexer) SearchShows(ctx context.Context, query string, season, episode int) ([]SearchResult, error) {
	searchQuery := query
	if season > 0 && episode > 0 {
		searchQuery = fmt.Sprintf("%s S%02dE%02d", query, season, episode)
	} else if season > 0 {
		searchQuery = fmt.Sprintf("%s S%02d", query, season)
	}
	return tg.search(ctx, searchQuery, "TV")
}

func (tg *TorrentGalaxyIndexer) search(ctx context.Context, query string, category string) ([]SearchResult, error) {
	apiURL := sharedhttp.BuildQueryURL("https://torrentgalaxy.to/api/search", map[string]string{
		"q":        query,
		"category": category,
	})

	slog.Info("Fetching from TorrentGalaxy", "query", query, "category", category)
	resp, err := sharedhttp.MakeRequest(ctx, apiURL, sharedhttp.DefaultClient)
	if err != nil {
		slog.Warn("TorrentGalaxy request failed", "query", query, "category", category, "error", err)
		return []SearchResult{}, nil
	}

	var apiResp TorrentGalaxyResponse
	if err := sharedhttp.DecodeJSONResponse(resp, &apiResp); err != nil {
		slog.Warn("TorrentGalaxy decode failed", "query", query, "category", category, "error", err)
		return []SearchResult{}, nil
	}

	if apiResp.Status != "success" {
		slog.Warn("TorrentGalaxy returned non-success status", "query", query, "category", category, "status", apiResp.Status)
		return []SearchResult{}, nil
	}

	slog.Info("TorrentGalaxy request successful", "query", query, "category", category, "results", len(apiResp.Results))

	var results []SearchResult
	for _, r := range apiResp.Results {
		sizeBytes := parseSize(r.Size)
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
