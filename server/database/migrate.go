package database

import (
	"embed"
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// RunMigrations executes all pending migrations in order.
func RunMigrations() error {
	// Create the schema_migrations table if it doesn't exist
	_, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version VARCHAR(255) PRIMARY KEY,
			applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create schema_migrations table: %w", err)
	}

	// Read all migration files
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("failed to read migrations directory: %w", err)
	}

	// Sort by name (lexicographic, so 001_ < 002_ etc.)
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	// Run each migration that hasn't been applied
	for _, name := range names {
		var count int
		err := DB.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = $1", name).Scan(&count)
		if err != nil {
			return fmt.Errorf("failed to check migration status for %s: %w", name, err)
		}
		if count > 0 {
			continue // already applied
		}

		// Read and execute the migration
		content, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("failed to read migration %s: %w", name, err)
		}

		slog.Info("Applying migration", "migration", name)

		tx, err := DB.Begin()
		if err != nil {
			return fmt.Errorf("failed to begin transaction for %s: %w", name, err)
		}

		if _, err := tx.Exec(string(content)); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to execute migration %s: %w", name, err)
		}

		if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES ($1)", name); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to record migration %s: %w", name, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit migration %s: %w", name, err)
		}

		slog.Info("Migration applied successfully", "migration", name)
	}

	return nil
}
