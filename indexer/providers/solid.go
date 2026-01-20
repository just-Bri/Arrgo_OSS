package providers

import (
	"context"

	"github.com/justbri/arrgo/shared/format"
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
	return s.search(ctx, query, "Video")
}

func (s *SolidTorrentsIndexer) search(ctx context.Context, query string, category string) ([]SearchResult, error) {
	// API: https://solidtorrents.to/api/v1/search?q=...&category=Video&sort=seeders
	apiURL := BuildQueryURL("https://solidtorrents.to/api/v1/search", map[string]string{
		"q":        query,
		"category": category,
		"sort":     "seeders",
	})

	resp, err := MakeHTTPRequest(ctx, apiURL, DefaultHTTPClient)
	if err != nil {
		return nil, err
	}

	var apiResp SolidTorrentsResponse
	if err := DecodeJSONResponse(resp, &apiResp); err != nil {
		return nil, err
	}

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
