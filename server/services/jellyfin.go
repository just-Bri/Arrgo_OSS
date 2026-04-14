package services

import (
	"Arrgo/config"
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

var jellyfinClient = &http.Client{Timeout: 10 * time.Second}

// skipJellyfinSync returns true for users that should not get a Jellyfin account.
func skipJellyfinSync(username string) bool {
	lower := strings.ToLower(username)
	return lower == "admin"
}

// CreateJellyfinUser creates a new user in Jellyfin with the given username and password.
// Returns the Jellyfin user ID on success.
func CreateJellyfinUser(cfg *config.Config, username, password string) (string, error) {
	if cfg.JellyfinURL == "" || cfg.JellyfinAPIKey == "" {
		return "", fmt.Errorf("jellyfin integration not configured")
	}

	if skipJellyfinSync(username) {
		slog.Debug("Skipping Jellyfin user creation for excluded user", "username", username)
		return "", nil
	}

	// Create the user
	createURL := fmt.Sprintf("%s/Users/New", cfg.JellyfinURL)
	body, _ := json.Marshal(map[string]string{
		"Name":     username,
		"Password": password,
	})

	req, err := http.NewRequest("POST", createURL, bytes.NewBuffer(body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("MediaBrowser Token=%q", cfg.JellyfinAPIKey))
	req.Header.Set("Content-Type", "application/json")

	resp, err := jellyfinClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("jellyfin request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("jellyfin returned status %d", resp.StatusCode)
	}

	var result struct {
		ID   string `json:"Id"`
		Name string `json:"Name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode jellyfin response: %w", err)
	}

	// Set the password for the new user
	passwordURL := fmt.Sprintf("%s/Users/%s/Password", cfg.JellyfinURL, result.ID)
	pwBody, _ := json.Marshal(map[string]string{
		"NewPw": password,
	})

	pwReq, err := http.NewRequest("POST", passwordURL, bytes.NewBuffer(pwBody))
	if err != nil {
		return result.ID, fmt.Errorf("user created but failed to set password: %w", err)
	}
	pwReq.Header.Set("Authorization", fmt.Sprintf("MediaBrowser Token=%q", cfg.JellyfinAPIKey))
	pwReq.Header.Set("Content-Type", "application/json")

	pwResp, err := jellyfinClient.Do(pwReq)
	if err != nil {
		return result.ID, fmt.Errorf("user created but password request failed: %w", err)
	}
	defer pwResp.Body.Close()

	slog.Info("Created Jellyfin user", "username", username, "jellyfin_id", result.ID)
	return result.ID, nil
}

// RefreshJellyfinLibrary triggers a library scan in Jellyfin.
func RefreshJellyfinLibrary(cfg *config.Config) error {
	if cfg.JellyfinURL == "" || cfg.JellyfinAPIKey == "" {
		return nil
	}

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/Library/Refresh", cfg.JellyfinURL), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", fmt.Sprintf("MediaBrowser Token=%q", cfg.JellyfinAPIKey))

	resp, err := jellyfinClient.Do(req)
	if err != nil {
		return fmt.Errorf("jellyfin library refresh failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jellyfin library refresh returned status %d", resp.StatusCode)
	}

	slog.Info("Triggered Jellyfin library refresh")
	return nil
}

// SyncExistingUsersToJellyfin creates Jellyfin accounts for all existing Arrgo users
// that don't already have one in Jellyfin. Since we can't recover passwords, new
// Jellyfin users are created with a temporary password they'll need to change.
func SyncExistingUsersToJellyfin(cfg *config.Config) error {
	if cfg.JellyfinURL == "" || cfg.JellyfinAPIKey == "" {
		return fmt.Errorf("jellyfin integration not configured")
	}

	// Get existing Jellyfin users
	jellyfinUsers, err := getJellyfinUsers(cfg)
	if err != nil {
		return fmt.Errorf("failed to get jellyfin users: %w", err)
	}

	// Build a set of existing Jellyfin usernames (lowercased)
	existingUsers := make(map[string]bool)
	for _, u := range jellyfinUsers {
		existingUsers[strings.ToLower(u.Name)] = true
	}

	// Get all Arrgo users
	users, err := GetAllUsers()
	if err != nil {
		return fmt.Errorf("failed to get arrgo users: %w", err)
	}

	for _, user := range users {
		if skipJellyfinSync(user.Username) {
			continue
		}

		if existingUsers[strings.ToLower(user.Username)] {
			slog.Debug("Jellyfin user already exists, skipping", "username", user.Username)
			continue
		}

		// Create with a temporary password — user will need to change it
		tempPassword := fmt.Sprintf("changeme-%s", user.Username)
		if _, err := CreateJellyfinUser(cfg, user.Username, tempPassword); err != nil {
			slog.Error("Failed to sync user to Jellyfin", "username", user.Username, "error", err)
		}
	}

	return nil
}

type jellyfinUser struct {
	ID   string `json:"Id"`
	Name string `json:"Name"`
}

func getJellyfinUsers(cfg *config.Config) ([]jellyfinUser, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/Users", cfg.JellyfinURL), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("MediaBrowser Token=%q", cfg.JellyfinAPIKey))

	resp, err := jellyfinClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jellyfin returned status %d", resp.StatusCode)
	}

	var users []jellyfinUser
	if err := json.NewDecoder(resp.Body).Decode(&users); err != nil {
		return nil, err
	}
	return users, nil
}
