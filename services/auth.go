package services

import (
	"Arrgo/database"
	"Arrgo/models"
	"database/sql"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

func AuthenticateUser(username, password string) (*models.User, error) {
	var user models.User
	err := database.DB.QueryRow(
		"SELECT id, username, email, password_hash, created_at, updated_at FROM users WHERE username = $1",
		username,
	).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.PasswordHash,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("invalid credentials")
		}
		return nil, fmt.Errorf("database error: %w", err)
	}

	// Verify password
	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
	if err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}

	return &user, nil
}

func GetUserByID(userID int64) (*models.User, error) {
	var user models.User
	err := database.DB.QueryRow(
		"SELECT id, username, email, password_hash, created_at, updated_at FROM users WHERE id = $1",
		userID,
	).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.PasswordHash,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user not found")
		}
		return nil, fmt.Errorf("database error: %w", err)
	}

	return &user, nil
}

