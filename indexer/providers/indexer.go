package providers

import (
	"context"
)

type SearchResult struct {
	Title      string `json:"title"`
	Size       string `json:"size"`
	Seeds      int    `json:"seeds"`
	Peers      int    `json:"peers"`
	MagnetLink string `json:"magnet_link"`
	InfoHash   string `json:"info_hash"`
	Source     string `json:"source"`
	Resolution string `json:"resolution"`
	Quality    string `json:"quality"`
}

type Indexer interface {
	SearchMovies(ctx context.Context, query string) ([]SearchResult, error)
	SearchShows(ctx context.Context, query string, season, episode int) ([]SearchResult, error)
	Name() string
}

func Indexers() []Indexer {
	return []Indexer{
		&YTSIndexer{},
		NewNyaaIndexer(),
		NewX1337Indexer(),
		NewTorrentGalaxyIndexer(),
		NewSolidTorrentsIndexer(),
	}
}
