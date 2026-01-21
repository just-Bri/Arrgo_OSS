package services

import (
	"Arrgo/config"
	"Arrgo/database"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// SeedingThresholdMinutes is the maximum seeding time in minutes (1440 = 24 hours)
	SeedingThresholdMinutes = 1440
	// SeedingThresholdRatio is the maximum seed ratio (2.0)
	SeedingThresholdRatio = 2.0
)

// CheckAndCleanupSeedingTorrents checks torrents for seeding criteria and cleans them up
// Returns the number of torrents cleaned up
func CheckAndCleanupSeedingTorrents(ctx context.Context, cfg *config.Config, qb *QBittorrentClient) (int, error) {
	if qb == nil {
		return 0, nil
	}

	torrents, err := qb.GetTorrentsDetailed(ctx, "seeding")
	if err != nil {
		return 0, fmt.Errorf("failed to get seeding torrents: %w", err)
	}

	cleanedCount := 0
	for _, torrent := range torrents {
		// Check if torrent meets cleanup criteria
		seedingTimeMinutes := float64(torrent.SeedingTime) / 60.0
		meetsTimeThreshold := seedingTimeMinutes >= SeedingThresholdMinutes
		meetsRatioThreshold := torrent.Ratio >= SeedingThresholdRatio

		if meetsTimeThreshold || meetsRatioThreshold {
			normalizedHash := strings.ToLower(torrent.Hash)
			slog.Info("Torrent meets seeding cleanup criteria",
				"hash", normalizedHash,
				"name", torrent.Name,
				"seeding_time_minutes", seedingTimeMinutes,
				"ratio", torrent.Ratio,
				"meets_time", meetsTimeThreshold,
				"meets_ratio", meetsRatioThreshold)

			// Check if files are in incoming folders and have been imported
			shouldDelete := false
			var incomingPath string

			// Check if this torrent is associated with a movie or episode that's been imported
			var moviePath, episodePath string
			err := database.DB.QueryRow(`
				SELECT m.path FROM movies m 
				WHERE LOWER(m.torrent_hash) = $1 
				AND m.status = 'ready'
				LIMIT 1`, normalizedHash).Scan(&moviePath)
			if err == nil && moviePath != "" {
				// Movie has been imported (status = 'ready' and path not in incoming)
				if !strings.HasPrefix(moviePath, cfg.IncomingMoviesPath) {
					shouldDelete = true
					incomingPath = cfg.IncomingMoviesPath
				}
			} else {
				// Check episodes
				err = database.DB.QueryRow(`
					SELECT e.file_path FROM episodes e
					WHERE LOWER(e.torrent_hash) = $1
					LIMIT 1`, normalizedHash).Scan(&episodePath)
				if err == nil && episodePath != "" {
					// Check if episode has been imported (not in incoming)
					if !strings.HasPrefix(episodePath, cfg.IncomingShowsPath) {
						shouldDelete = true
						incomingPath = cfg.IncomingShowsPath
					}
				} else {
					// No database entry found, check if save path is in incoming
					if strings.HasPrefix(torrent.SavePath, cfg.IncomingMoviesPath) ||
						strings.HasPrefix(torrent.SavePath, cfg.IncomingShowsPath) {
						// Files are in incoming, but not in DB - might be orphaned
						// Only delete if we can verify files exist and are old enough
						shouldDelete = true
						if strings.HasPrefix(torrent.SavePath, cfg.IncomingMoviesPath) {
							incomingPath = cfg.IncomingMoviesPath
						} else {
							incomingPath = cfg.IncomingShowsPath
						}
					}
				}
			}

			if shouldDelete {
				// Delete torrent and files from qBittorrent
				if err := qb.DeleteTorrent(ctx, normalizedHash, true); err != nil {
					slog.Error("Failed to delete torrent from qBittorrent",
						"hash", normalizedHash,
						"error", err)
					continue
				}

				// Clean up files from incoming folder if they still exist
				if incomingPath != "" && torrent.SavePath != "" {
					if strings.HasPrefix(torrent.SavePath, incomingPath) {
						if err := cleanupIncomingFiles(torrent.SavePath, incomingPath); err != nil {
							slog.Error("Failed to cleanup incoming files",
								"save_path", torrent.SavePath,
								"incoming_path", incomingPath,
								"error", err)
						} else {
							slog.Info("Cleaned up incoming files",
								"save_path", torrent.SavePath)
						}
					}
				}

				// Remove torrent_hash from database entries
				database.DB.Exec("UPDATE movies SET torrent_hash = NULL WHERE LOWER(torrent_hash) = $1", normalizedHash)
				database.DB.Exec("UPDATE episodes SET torrent_hash = NULL WHERE LOWER(torrent_hash) = $1", normalizedHash)

				cleanedCount++
			}
		}
	}

	return cleanedCount, nil
}

// cleanupIncomingFiles removes files from the incoming folder
func cleanupIncomingFiles(savePath, incomingPath string) error {
	// Check if path exists
	if _, err := os.Stat(savePath); os.IsNotExist(err) {
		return nil // Already deleted
	}

	// If it's a directory, remove the entire directory
	if info, err := os.Stat(savePath); err == nil && info.IsDir() {
		return os.RemoveAll(savePath)
	}

	// If it's a file, remove it and try to clean up parent directories
	if err := os.Remove(savePath); err != nil {
		return err
	}

	// Try to clean up empty parent directories up to the incoming root
	dir := filepath.Dir(savePath)
	for strings.HasPrefix(dir, incomingPath) && dir != incomingPath {
		entries, err := os.ReadDir(dir)
		if err != nil {
			break
		}
		if len(entries) == 0 {
			if err := os.Remove(dir); err != nil {
				break
			}
			dir = filepath.Dir(dir)
		} else {
			break
		}
	}

	return nil
}

// CheckSeedingCriteriaOnImport checks if a torrent should be deleted when importing
// Returns true if the torrent should be deleted, along with the torrent hash
func CheckSeedingCriteriaOnImport(ctx context.Context, cfg *config.Config, qb *QBittorrentClient, torrentHash string) (bool, error) {
	if qb == nil || torrentHash == "" {
		return false, nil
	}

	// Get detailed info with ratio and seeding time
	torrents, err := qb.GetTorrentsDetailed(ctx, "")
	if err != nil {
		return false, err
	}

	var detailedTorrent *TorrentStatus
	normalizedHash := strings.ToLower(torrentHash)
	for _, t := range torrents {
		if strings.ToLower(t.Hash) == normalizedHash {
			detailedTorrent = &t
			break
		}
	}

	if detailedTorrent == nil {
		// Torrent not found, might already be deleted
		return false, nil
	}

	seedingTimeMinutes := float64(detailedTorrent.SeedingTime) / 60.0
	meetsTimeThreshold := seedingTimeMinutes >= SeedingThresholdMinutes
	meetsRatioThreshold := detailedTorrent.Ratio >= SeedingThresholdRatio

	return meetsTimeThreshold || meetsRatioThreshold, nil
}

// CleanupTorrentOnImport checks seeding criteria and cleans up torrent/files if criteria met
func CleanupTorrentOnImport(ctx context.Context, cfg *config.Config, qb *QBittorrentClient, torrentHash string, filePath string) error {
	if qb == nil || torrentHash == "" {
		return nil
	}

	shouldDelete, err := CheckSeedingCriteriaOnImport(ctx, cfg, qb, torrentHash)
	if err != nil {
		return err
	}

	if !shouldDelete {
		return nil
	}

	normalizedHash := strings.ToLower(torrentHash)
	slog.Info("Torrent meets seeding criteria, cleaning up on import",
		"hash", normalizedHash,
		"file_path", filePath)

	// Delete torrent and files from qBittorrent
	if err := qb.DeleteTorrent(ctx, normalizedHash, true); err != nil {
		slog.Error("Failed to delete torrent from qBittorrent on import",
			"hash", normalizedHash,
			"error", err)
		// Continue with file cleanup even if torrent deletion fails
	}

	// Clean up files from incoming folder if they still exist
	var incomingPath string
	if strings.HasPrefix(filePath, cfg.IncomingMoviesPath) {
		incomingPath = cfg.IncomingMoviesPath
	} else if strings.HasPrefix(filePath, cfg.IncomingShowsPath) {
		incomingPath = cfg.IncomingShowsPath
	}

	if incomingPath != "" {
		// Get the directory containing the file
		fileDir := filepath.Dir(filePath)
		if strings.HasPrefix(fileDir, incomingPath) {
			if err := cleanupIncomingFiles(fileDir, incomingPath); err != nil {
				slog.Error("Failed to cleanup incoming files on import",
					"file_dir", fileDir,
					"incoming_path", incomingPath,
					"error", err)
			} else {
				slog.Info("Cleaned up incoming files on import",
					"file_dir", fileDir)
			}
		}
	}

	// Remove torrent_hash from database entries
	database.DB.Exec("UPDATE movies SET torrent_hash = NULL WHERE LOWER(torrent_hash) = $1", normalizedHash)
	database.DB.Exec("UPDATE episodes SET torrent_hash = NULL WHERE LOWER(torrent_hash) = $1", normalizedHash)

	return nil
}

// LinkTorrentHashToFile attempts to link a torrent hash to a file based on its path
// This is called when files are scanned in incoming folders
func LinkTorrentHashToFile(cfg *config.Config, qb *QBittorrentClient, filePath string, mediaType string) {
	if qb == nil {
		return
	}

	// Only link if file is in incoming folder
	if mediaType == "movie" && !strings.HasPrefix(filePath, cfg.IncomingMoviesPath) {
		return
	}
	if mediaType == "show" && !strings.HasPrefix(filePath, cfg.IncomingShowsPath) {
		return
	}

	// Get the directory containing the file (torrents usually save to a directory)
	fileDir := filepath.Dir(filePath)
	
	// Try to find a torrent with matching save path
	ctx := context.Background()
	torrents, err := qb.GetTorrentsDetailed(ctx, "")
	if err != nil {
		return
	}

	for _, torrent := range torrents {
		// Check if torrent's save path matches or contains the file directory
		if strings.HasPrefix(fileDir, torrent.SavePath) || strings.HasPrefix(torrent.SavePath, fileDir) {
			normalizedHash := strings.ToLower(torrent.Hash)
			
			// Link to movie or episode
			if mediaType == "movie" {
				database.DB.Exec(`
					UPDATE movies 
					SET torrent_hash = $1 
					WHERE path = $2 AND (torrent_hash IS NULL OR torrent_hash = '')`,
					normalizedHash, filePath)
			} else if mediaType == "show" {
				database.DB.Exec(`
					UPDATE episodes 
					SET torrent_hash = $1 
					WHERE file_path = $2 AND (torrent_hash IS NULL OR torrent_hash = '')`,
					normalizedHash, filePath)
			}
			
			slog.Debug("Linked torrent hash to file",
				"hash", normalizedHash,
				"file_path", filePath,
				"media_type", mediaType)
			break
		}
	}
}

// StartSeedingCleanupWorker starts a background worker that periodically checks and cleans up seeding torrents
func StartSeedingCleanupWorker(cfg *config.Config, qb *QBittorrentClient) {
	slog.Info("Starting seeding cleanup background worker")

	go func() {
		ticker := time.NewTicker(1 * time.Hour) // Check every 1 hour
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				slog.Debug("Running seeding cleanup check")
				count, err := CheckAndCleanupSeedingTorrents(context.Background(), cfg, qb)
				if err != nil {
					slog.Error("Error during seeding cleanup", "error", err)
				} else if count > 0 {
					slog.Info("Seeding cleanup completed", "torrents_cleaned", count)
				}
			}
		}
	}()
}
