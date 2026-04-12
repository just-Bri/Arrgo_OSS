package indexers

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/justbri/arrgo/shared/format"
	sharedhttp "github.com/justbri/arrgo/shared/http"
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

func (s *SolidTorrentsIndexer) Name() string {
	return "SolidTorrents"
}

func (s *SolidTorrentsIndexer) SearchMovies(ctx context.Context, query string) ([]SearchResult, error) {
	return s.search(ctx, query, "Video")
}

func (s *SolidTorrentsIndexer) SearchShows(ctx context.Context, query string, season, episode int) ([]SearchResult, error) {
	searchQuery := query
	if season > 0 && episode > 0 {
		searchQuery = fmt.Sprintf("%s S%02dE%02d", query, season, episode)
	} else if season > 0 {
		searchQuery = fmt.Sprintf("%s S%02d", query, season)
	}
	return s.search(ctx, searchQuery, "Video")
}

func (s *SolidTorrentsIndexer) search(ctx context.Context, query string, category string) ([]SearchResult, error) {
	apiURL := sharedhttp.BuildQueryURL("https://solidtorrents.to/api/v1/search", map[string]string{
		"q":        query,
		"category": category,
		"sort":     "seeders",
	})

	slog.Debug("Fetching from SolidTorrents", "query", query, "category", category)
	resp, err := sharedhttp.MakeRequest(ctx, apiURL, sharedhttp.DefaultClient)
	if err != nil {
		slog.Debug("SolidTorrents request failed", "query", query, "category", category, "error", err)
		return nil, err
	}

	var apiResp SolidTorrentsResponse
	if err := sharedhttp.DecodeJSONResponse(resp, &apiResp); err != nil {
		slog.Debug("SolidTorrents decode failed", "query", query, "category", category, "error", err)
		return nil, err
	}

	slog.Debug("SolidTorrents request successful", "query", query, "category", category, "results", len(apiResp.Results))

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
