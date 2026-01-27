package indexers

import (
	"context"
	"Arrgo/database"
	"Arrgo/models"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
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

	// Sort by priority
	sort.Slice(dbIndexers, func(i, j int) bool {
		return dbIndexers[i].Priority < dbIndexers[j].Priority
	})

	for _, dbIdx := range dbIndexers {
		if dbIdx.Type == "builtin" {
			// Try to get scraper type from config, fallback to name-based lookup
			scraperType := getScraperTypeFromConfig(dbIdx.Config, dbIdx.Name)
			
			// Check if this is a generic scraper (has full config)
			if scraperType == "generic" && dbIdx.Config != "" {
				idx, err := createGenericIndexerFromConfig(dbIdx.Name, dbIdx.Config)
				if err != nil {
					slog.Warn("Failed to create generic indexer", "name", dbIdx.Name, "error", err)
					continue
				}
				indexers = append(indexers, idx)
			} else {
				// Use specific built-in scraper
				idx := createBuiltinIndexer(scraperType, dbIdx.Name)
				if idx != nil {
					indexers = append(indexers, idx)
				} else {
					slog.Warn("Failed to create built-in indexer", "name", dbIdx.Name, "scraper_type", scraperType)
				}
			}
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

// getScraperTypeFromConfig extracts the scraper type from config JSON, with fallback to name
func getScraperTypeFromConfig(configJSON, name string) string {
	if configJSON == "" {
		// Fallback to name-based mapping for backwards compatibility
		return mapNameToScraperType(name)
	}

	var config map[string]interface{}
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		slog.Warn("Failed to parse indexer config", "error", err, "config", configJSON)
		return mapNameToScraperType(name)
	}

	if scraperType, ok := config["scraper_type"].(string); ok && scraperType != "" {
		return scraperType
	}

	return mapNameToScraperType(name)
}

// mapNameToScraperType maps legacy indexer names to scraper types
func mapNameToScraperType(name string) string {
	nameLower := strings.ToLower(name)
	switch nameLower {
	case "yts":
		return "yts"
	case "nyaa":
		return "nyaa"
	case "1337x", "x1337":
		return "1337x"
	case "torrentgalaxy", "tgx":
		return "torrentgalaxy"
	case "solidtorrents", "solid":
		return "solidtorrents"
	default:
		return nameLower
	}
}

// createBuiltinIndexer creates a built-in indexer instance based on scraper type
func createBuiltinIndexer(scraperType, displayName string) Indexer {
	scraperTypeLower := strings.ToLower(scraperType)
	switch scraperTypeLower {
	case "yts":
		return &YTSIndexer{}
	case "nyaa":
		return NewNyaaIndexer()
	case "1337x", "x1337":
		return NewX1337Indexer()
	case "torrentgalaxy", "tgx":
		return NewTorrentGalaxyIndexer()
	case "solidtorrents", "solid":
		return NewSolidTorrentsIndexer()
	case "generic":
		// Generic scraper requires config - this shouldn't be called directly
		// It should be created via createGenericIndexerFromConfig
		slog.Warn("Generic scraper type requires config", "type", scraperType)
		return nil
	default:
		slog.Warn("Unknown scraper type", "type", scraperType)
		return nil
	}
}

// createGenericIndexerFromConfig creates a generic indexer from database config
func createGenericIndexerFromConfig(name, configJSON string) (Indexer, error) {
	return NewGenericIndexer(name, configJSON)
}

// GetAvailableScraperTypes returns a list of available built-in scraper types
func GetAvailableScraperTypes() []map[string]string {
	return []map[string]string{
		{"value": "yts", "label": "YTS (Movies only)"},
		{"value": "nyaa", "label": "Nyaa (Anime/TV)"},
		{"value": "1337x", "label": "1337x (Movies & TV)"},
		{"value": "torrentgalaxy", "label": "TorrentGalaxy (Movies & TV)"},
		{"value": "solidtorrents", "label": "SolidTorrents (Movies & TV)"},
	}
}
