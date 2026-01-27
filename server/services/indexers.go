package services

import (
	"Arrgo/database"
	"Arrgo/models"
	"database/sql"
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

// AddTorznabIndexer adds a new Torznab indexer
func AddTorznabIndexer(name, url, apiKey string, priority int) (*models.Indexer, error) {
	// Validate URL
	if url == "" {
		return nil, fmt.Errorf("URL is required")
	}

	// Check if indexer with same name/type already exists
	var existingID int
	err := database.DB.QueryRow(
		"SELECT id FROM indexers WHERE name = $1 AND type = 'torznab'",
		name,
	).Scan(&existingID)

	if err == nil {
		// Update existing
		_, err = database.DB.Exec(
			`UPDATE indexers SET url = $1, api_key = $2, priority = $3, 
			 enabled = TRUE, updated_at = CURRENT_TIMESTAMP WHERE id = $4`,
			url, apiKey, priority, existingID,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to update indexer: %w", err)
		}
		return GetIndexerByID(existingID)
	}

	// Insert new
	var id int
	err = database.DB.QueryRow(
		`INSERT INTO indexers (name, type, enabled, url, api_key, priority) 
		 VALUES ($1, 'torznab', TRUE, $2, $3, $4) RETURNING id`,
		name, url, apiKey, priority,
	).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("failed to add indexer: %w", err)
	}

	return GetIndexerByID(id)
}

// DeleteIndexer deletes an indexer (only Torznab indexers can be deleted)
func DeleteIndexer(id int) error {
	result, err := database.DB.Exec(
		"DELETE FROM indexers WHERE id = $1 AND type = 'torznab'",
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
		return fmt.Errorf("indexer not found or cannot be deleted (built-in indexers cannot be deleted)")
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

// TestTorznabIndexer tests if a Torznab indexer is accessible
func TestTorznabIndexer(url, apiKey string) error {
	// This would make a test request to the Torznab API
	// For now, we'll just validate the URL format
	if url == "" {
		return fmt.Errorf("URL is required")
	}
	// TODO: Actually test the connection by calling t=caps
	return nil
}

// GetIndexerStats returns statistics about indexers
func GetIndexerStats() (map[string]interface{}, error) {
	var totalCount, enabledCount, builtinCount, torznabCount int

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

	err = database.DB.QueryRow("SELECT COUNT(*) FROM indexers WHERE type = 'torznab'").Scan(&torznabCount)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"total":   totalCount,
		"enabled": enabledCount,
		"builtin": builtinCount,
		"torznab": torznabCount,
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
