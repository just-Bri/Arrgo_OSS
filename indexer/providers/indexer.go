package providers

import (
	"context"
)

type SearchResult struct {
	Title       string `json:"title"`
	Size        string `json:"size"`
	Seeds       int    `json:"seeds"`
	Peers       int    `json:"peers"`
	MagnetLink  string `json:"magnet_link"`
	InfoHash    string `json:"info_hash"`
	Source      string `json:"source"`
	Resolution  string `json:"resolution"`
	Quality     string `json:"quality"`
}

type Indexer interface {
	SearchMovies(ctx context.Context, query string) ([]SearchResult, error)
	SearchShows(ctx context.Context, query string, season, episode int) ([]SearchResult, error)
	GetName() string
}

func GetIndexers() []Indexer {
	return []Indexer{
		&YTSIndexer{},
		&EZTVIndexer{},
	}
}
