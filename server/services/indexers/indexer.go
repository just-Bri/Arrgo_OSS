package indexers

import (
	"context"
	"Arrgo/database"
	"Arrgo/models"
	"fmt"
	"log/slog"
	"sort"
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
	GetName() string
}

// GetIndexers returns all enabled indexers from the database, sorted by priority
func GetIndexers() ([]Indexer, error) {
	// Get enabled indexers from database
	dbIndexers, err := getEnabledIndexersFromDB()
	if err != nil {
		slog.Warn("Failed to get indexers from database, using defaults", "error", err)
		return getDefaultIndexers(), nil
	}

	indexers := make([]Indexer, 0)
	builtinMap := getBuiltinIndexerMap()

	// Sort by priority
	sort.Slice(dbIndexers, func(i, j int) bool {
		return dbIndexers[i].Priority < dbIndexers[j].Priority
	})

	for _, dbIdx := range dbIndexers {
		if dbIdx.Type == "builtin" {
			// Get built-in indexer
			if idx, ok := builtinMap[dbIdx.Name]; ok {
				indexers = append(indexers, idx)
			}
		} else if dbIdx.Type == "torznab" {
			// Create Torznab indexer
			idx := NewTorznabIndexer(dbIdx.Name, dbIdx.URL, dbIdx.APIKey)
			indexers = append(indexers, idx)
		}
	}

	return indexers, nil
}

// getEnabledIndexersFromDB gets enabled indexers from database
func getEnabledIndexersFromDB() ([]models.Indexer, error) {
	var indexers []models.Indexer
	query := `SELECT id, name, type, enabled, url, api_key, priority, config, created_at, updated_at 
	          FROM indexers WHERE enabled = TRUE ORDER BY priority ASC, name ASC`
	
	rows, err := database.DB.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query indexers: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var idx models.Indexer
		var configJSON *string
		err := rows.Scan(
			&idx.ID, &idx.Name, &idx.Type, &idx.Enabled,
			&idx.URL, &idx.APIKey, &idx.Priority, &configJSON,
			&idx.CreatedAt, &idx.UpdatedAt,
		)
		if err != nil {
			slog.Warn("Failed to scan indexer row", "error", err)
			continue
		}
		if configJSON != nil {
			idx.Config = *configJSON
		}
		indexers = append(indexers, idx)
	}

	return indexers, nil
}

// getDefaultIndexers returns the default hardcoded indexers (fallback)
func getDefaultIndexers() []Indexer {
	return []Indexer{
		&YTSIndexer{},
		NewNyaaIndexer(),
		NewX1337Indexer(),
		NewTorrentGalaxyIndexer(),
		NewSolidTorrentsIndexer(),
	}
}

// getBuiltinIndexerMap returns a map of built-in indexers by name
func getBuiltinIndexerMap() map[string]Indexer {
	return map[string]Indexer{
		"YTS":            &YTSIndexer{},
		"Nyaa":           NewNyaaIndexer(),
		"1337x":          NewX1337Indexer(),
		"TorrentGalaxy": NewTorrentGalaxyIndexer(),
		"SolidTorrents": NewSolidTorrentsIndexer(),
	}
}
