package database

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

func SeedAdminUser() error {
	// Check if admin user already exists
	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM users WHERE username = $1", "admin").Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check for existing admin user: %w", err)
	}

	if count > 0 {
		// Admin user already exists, skip seeding
		return nil
	}

	// Hash the password "admin"
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	// Insert admin user
	_, err = DB.Exec(
		"INSERT INTO users (username, email, password_hash, is_admin) VALUES ($1, $2, $3, $4)",
		"admin",
		"admin@arrgo.local",
		string(hashedPassword),
		true,
	)
	if err != nil {
		return fmt.Errorf("failed to seed admin user: %w", err)
	}

	return nil
}
