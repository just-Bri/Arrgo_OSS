package database

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
	"github.com/justbri/arrgo/shared/config"
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
