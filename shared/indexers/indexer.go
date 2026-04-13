package indexers

import "context"

// SearchResult holds a single torrent search result from any indexer.
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

// Indexer is the common interface implemented by every torrent provider.
type Indexer interface {
	SearchMovies(ctx context.Context, query string) ([]SearchResult, error)
	SearchShows(ctx context.Context, query string, season, episode int) ([]SearchResult, error)
	Name() string
}

// Indexers returns all built-in indexer implementations.
func Indexers() []Indexer {
	return []Indexer{
		&YTSIndexer{},
		NewNyaaIndexer(),
		NewTorrentGalaxyIndexer(),
		NewSolidTorrentsIndexer(),
	}
}
