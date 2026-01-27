package database

import (
	"fmt"

	"github.com/justbri/arrgo/shared/config"
	"golang.org/x/crypto/bcrypt"
)

func SeedAdminUser() error {
	// Get admin credentials from environment variables
	adminUsername := config.GetEnv("ADMIN_USERNAME", "admin")
	adminPassword := config.GetEnv("ADMIN_PASSWORD", "")
	adminEmail := config.GetEnv("ADMIN_EMAIL", "admin@arrgo.local")

	// If no password is set, skip seeding (user should set ADMIN_PASSWORD)
	if adminPassword == "" {
		return nil
	}

	// Check if admin user already exists
	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM users WHERE username = $1", adminUsername).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check for existing admin user: %w", err)
	}

	if count > 0 {
		// Admin user already exists, skip seeding
		return nil
	}

	// Hash the password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(adminPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	// Insert admin user
	_, err = DB.Exec(
		"INSERT INTO users (username, email, password_hash, is_admin) VALUES ($1, $2, $3, $4)",
		adminUsername,
		adminEmail,
		string(hashedPassword),
		true,
	)
	if err != nil {
		return fmt.Errorf("failed to seed admin user: %w", err)
	}

	return nil
}

// SeedDefaultIndexers ensures default built-in indexers exist in the database
func SeedDefaultIndexers() error {
	defaultIndexers := []struct {
		name        string
		indexerType string
		priority    int
	}{
		{"YTS", "builtin", 1},
		{"Nyaa", "builtin", 2},
		{"1337x", "builtin", 3},
		{"TorrentGalaxy", "builtin", 4},
		{"SolidTorrents", "builtin", 5},
	}

	for _, idx := range defaultIndexers {
		// Use INSERT ... ON CONFLICT to avoid duplicates
		_, err := DB.Exec(
			`INSERT INTO indexers (name, type, enabled, priority) 
			 VALUES ($1, $2, TRUE, $3)
			 ON CONFLICT (name, type) DO UPDATE SET priority = EXCLUDED.priority`,
			idx.name, idx.indexerType, idx.priority,
		)
		if err != nil {
			return fmt.Errorf("failed to seed indexer %s: %w", idx.name, err)
		}
	}

	return nil
}
