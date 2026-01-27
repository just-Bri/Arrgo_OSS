package services

import (
	"Arrgo/database"
	"Arrgo/models"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
)

// GetIndexers returns all configured indexers
func GetIndexers() ([]models.Indexer, error) {
	var indexers []models.Indexer
	query := `SELECT id, name, type, enabled, url, api_key, priority, config, created_at, updated_at 
	          FROM indexers ORDER BY priority ASC, name ASC`

	rows, err := database.DB.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query indexers: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var idx models.Indexer
		var url, apiKey, configJSON sql.NullString
		err := rows.Scan(
			&idx.ID, &idx.Name, &idx.Type, &idx.Enabled,
			&url, &apiKey, &idx.Priority, &configJSON,
			&idx.CreatedAt, &idx.UpdatedAt,
		)
		if err != nil {
			slog.Warn("Failed to scan indexer row", "error", err)
			continue
		}
		if url.Valid {
			idx.URL = url.String
		}
		if apiKey.Valid {
			idx.APIKey = apiKey.String
		}
		if configJSON.Valid {
			idx.Config = configJSON.String
		}
		indexers = append(indexers, idx)
	}

	return indexers, nil
}

// GetEnabledIndexers returns only enabled indexers
func GetEnabledIndexers() ([]models.Indexer, error) {
	all, err := GetIndexers()
	if err != nil {
		return nil, err
	}

	enabled := make([]models.Indexer, 0)
	for _, idx := range all {
		if idx.Enabled {
			enabled = append(enabled, idx)
		}
	}

	return enabled, nil
}

// GetIndexerByID returns a single indexer by ID
func GetIndexerByID(id int) (*models.Indexer, error) {
	var idx models.Indexer
	var url, apiKey, configJSON sql.NullString

	query := `SELECT id, name, type, enabled, url, api_key, priority, config, created_at, updated_at 
	          FROM indexers WHERE id = $1`

	err := database.DB.QueryRow(query, id).Scan(
		&idx.ID, &idx.Name, &idx.Type, &idx.Enabled,
		&url, &apiKey, &idx.Priority, &configJSON,
		&idx.CreatedAt, &idx.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get indexer: %w", err)
	}

	if url.Valid {
		idx.URL = url.String
	}
	if apiKey.Valid {
		idx.APIKey = apiKey.String
	}
	if configJSON.Valid {
		idx.Config = configJSON.String
	}

	return &idx, nil
}

// ToggleIndexer enables or disables an indexer
func ToggleIndexer(id int, enabled bool) error {
	_, err := database.DB.Exec(
		"UPDATE indexers SET enabled = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2",
		enabled, id,
	)
	if err != nil {
		return fmt.Errorf("failed to toggle indexer: %w", err)
	}
	return nil
}

// AddBuiltinIndexer adds a new built-in indexer
func AddBuiltinIndexer(name, scraperType string, priority int) (*models.Indexer, error) {
	return AddBuiltinIndexerWithConfig(name, scraperType, "", priority)
}

// AddBuiltinIndexerWithConfig adds a new built-in indexer with optional config
func AddBuiltinIndexerWithConfig(name, scraperType, configJSON string, priority int) (*models.Indexer, error) {
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if scraperType == "" {
		return nil, fmt.Errorf("scraper type is required")
	}

	// If config is provided, use it; otherwise create minimal config with scraper type
	if configJSON == "" {
		configJSON = fmt.Sprintf(`{"scraper_type": "%s"}`, scraperType)
	} else {
		// Merge scraper_type into provided config
		var configMap map[string]interface{}
		if err := json.Unmarshal([]byte(configJSON), &configMap); err == nil {
			configMap["scraper_type"] = scraperType
			configBytes, _ := json.Marshal(configMap)
			configJSON = string(configBytes)
		}
	}

	// Check if indexer with same name/type already exists
	var existingID int
	err := database.DB.QueryRow(
		"SELECT id FROM indexers WHERE name = $1 AND type = 'builtin'",
		name,
	).Scan(&existingID)

	if err == nil {
		// Update existing
		_, err = database.DB.Exec(
			`UPDATE indexers SET priority = $1, config = $2, 
			 enabled = TRUE, updated_at = CURRENT_TIMESTAMP WHERE id = $3`,
			priority, configJSON, existingID,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to update indexer: %w", err)
		}
		return GetIndexerByID(existingID)
	}

	// Insert new
	var id int
	err = database.DB.QueryRow(
		`INSERT INTO indexers (name, type, enabled, priority, config) 
		 VALUES ($1, 'builtin', TRUE, $2, $3) RETURNING id`,
		name, priority, configJSON,
	).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("failed to add indexer: %w", err)
	}

	return GetIndexerByID(id)
}

// DeleteIndexer deletes an indexer (custom built-in indexers can be deleted)
func DeleteIndexer(id int) error {
	// First check if it's a default seeded built-in indexer
	var name string
	var indexerType string
	err := database.DB.QueryRow(
		"SELECT name, type FROM indexers WHERE id = $1",
		id,
	).Scan(&name, &indexerType)
	if err != nil {
		return fmt.Errorf("indexer not found: %w", err)
	}

	// Prevent deletion of default seeded built-in indexers
	if indexerType == "builtin" {
		defaultNames := map[string]bool{
			"YTS":            true,
			"Nyaa":           true,
			"1337x":          true,
			"TorrentGalaxy":  true,
			"SolidTorrents":  true,
		}
		if defaultNames[name] {
			return fmt.Errorf("cannot delete default built-in indexer: %s", name)
		}
	}

	// Delete the indexer
	result, err := database.DB.Exec(
		"DELETE FROM indexers WHERE id = $1",
		id,
	)
	if err != nil {
		return fmt.Errorf("failed to delete indexer: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("indexer not found")
	}

	return nil
}

// UpdateIndexerPriority updates the priority of an indexer
func UpdateIndexerPriority(id int, priority int) error {
	_, err := database.DB.Exec(
		"UPDATE indexers SET priority = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2",
		priority, id,
	)
	if err != nil {
		return fmt.Errorf("failed to update priority: %w", err)
	}
	return nil
}

// GetIndexerStats returns statistics about indexers
func GetIndexerStats() (map[string]interface{}, error) {
	var totalCount, enabledCount, builtinCount int

	err := database.DB.QueryRow("SELECT COUNT(*) FROM indexers").Scan(&totalCount)
	if err != nil {
		return nil, err
	}

	err = database.DB.QueryRow("SELECT COUNT(*) FROM indexers WHERE enabled = TRUE").Scan(&enabledCount)
	if err != nil {
		return nil, err
	}

	err = database.DB.QueryRow("SELECT COUNT(*) FROM indexers WHERE type = 'builtin'").Scan(&builtinCount)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"total":   totalCount,
		"enabled": enabledCount,
		"builtin": builtinCount,
	}, nil
}

// ReorderIndexers updates priorities based on a list of IDs
func ReorderIndexers(indexerIDs []int) error {
	tx, err := database.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	for i, id := range indexerIDs {
		_, err = tx.Exec(
			"UPDATE indexers SET priority = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2",
			i+1, id,
		)
		if err != nil {
			return fmt.Errorf("failed to update priority: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
