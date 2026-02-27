package services

import (
	"Arrgo/config"
	"Arrgo/database"
	"context"
	"database/sql"
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
	// At this ratio, torrents are always removed from qBittorrent
	// Files are only deleted if they've been imported (moved out of incoming folder)
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

			// Always remove torrent from qBittorrent when threshold is met
			// Files are only deleted if BOTH conditions are met:
			// 1. Torrent has been removed from qBittorrent (verified after deletion)
			// 2. Files have been imported (imported_at IS NOT NULL in database)
			var incomingPath string
			var filesImported bool
			var moviePath, episodePath string

			// Check if this torrent is associated with a movie that's been imported
			var movieImportedAt sql.NullTime
			err := database.DB.QueryRow(`
				SELECT m.path, m.imported_at FROM movies m 
				WHERE LOWER(m.torrent_hash) = $1 
				AND m.status = 'ready'
				LIMIT 1`, normalizedHash).Scan(&moviePath, &movieImportedAt)
			if err == nil && moviePath != "" {
				// Movie exists in database
				incomingPath = cfg.IncomingMoviesPath
				// Check BOTH: imported_at is set AND path is not in incoming
				if movieImportedAt.Valid && !strings.HasPrefix(moviePath, cfg.IncomingMoviesPath) {
					// Movie has been imported (imported_at set AND moved out of incoming)
					filesImported = true
					slog.Debug("Movie is imported - will delete incoming files after torrent removal",
						"hash", normalizedHash,
						"movie_path", moviePath,
						"imported_at", movieImportedAt.Time)
				} else {
					// Movie not imported yet (either imported_at is NULL or still in incoming)
					filesImported = false
					slog.Debug("Torrent will be removed but files kept - movie not imported yet",
						"hash", normalizedHash,
						"movie_path", moviePath,
						"has_imported_at", movieImportedAt.Valid,
						"still_in_incoming", strings.HasPrefix(moviePath, cfg.IncomingMoviesPath))
				}
			} else {
				// Check episodes - need to verify ALL episodes are imported
				var episodesInIncoming int
				var episodesImportedCount int
				incomingShowsPathPattern := cfg.IncomingShowsPath + "%"
				err = database.DB.QueryRow(`
					SELECT 
						COUNT(CASE WHEN e.file_path LIKE $2 THEN 1 END) as in_incoming,
						COUNT(CASE WHEN e.imported_at IS NOT NULL AND e.file_path NOT LIKE $2 THEN 1 END) as imported
					FROM episodes e
					JOIN seasons s ON e.season_id = s.id
					WHERE LOWER(e.torrent_hash) = $1`, normalizedHash, incomingShowsPathPattern).Scan(&episodesInIncoming, &episodesImportedCount)

				if err == nil {
					// Get total episode count for this torrent
					var totalEpisodes int
					database.DB.QueryRow(`
						SELECT COUNT(*) FROM episodes e
						WHERE LOWER(e.torrent_hash) = $1`, normalizedHash).Scan(&totalEpisodes)

					if episodesInIncoming > 0 {
						// Some or all episodes are still in incoming - not imported yet
						filesImported = false
						incomingPath = cfg.IncomingShowsPath
						slog.Debug("Torrent will be removed but files kept - episodes still in incoming",
							"hash", normalizedHash,
							"episodes_in_incoming", episodesInIncoming,
							"episodes_imported", episodesImportedCount,
							"total_episodes", totalEpisodes)
					} else if episodesImportedCount > 0 && episodesImportedCount == totalEpisodes {
						// ALL episodes have been imported (imported_at set AND not in incoming)
						filesImported = true
						incomingPath = cfg.IncomingShowsPath
						// Get one episode path for file cleanup check
						database.DB.QueryRow(`
							SELECT e.file_path FROM episodes e
							WHERE LOWER(e.torrent_hash) = $1
							AND e.imported_at IS NOT NULL
							LIMIT 1`, normalizedHash).Scan(&episodePath)
						slog.Debug("All episodes are imported - will delete incoming files after torrent removal",
							"hash", normalizedHash,
							"episodes_imported", episodesImportedCount,
							"total_episodes", totalEpisodes)
					} else {
						// Some episodes imported but not all, or no episodes found
						filesImported = false
						incomingPath = cfg.IncomingShowsPath
						slog.Debug("Torrent will be removed but files kept - not all episodes imported",
							"hash", normalizedHash,
							"episodes_imported", episodesImportedCount,
							"total_episodes", totalEpisodes)
					}
				} else {
					// No database entries found - check if save path is in incoming
					if strings.HasPrefix(torrent.SavePath, cfg.IncomingMoviesPath) ||
						strings.HasPrefix(torrent.SavePath, cfg.IncomingShowsPath) {
						// Files are in incoming but not in DB - haven't been imported yet
						filesImported = false
						if strings.HasPrefix(torrent.SavePath, cfg.IncomingMoviesPath) {
							incomingPath = cfg.IncomingMoviesPath
						} else {
							incomingPath = cfg.IncomingShowsPath
						}
						slog.Debug("Torrent will be removed but files kept - files in incoming but not in DB",
							"hash", normalizedHash,
							"save_path", torrent.SavePath)
					} else {
						// Files are not in incoming and not in DB - might be orphaned
						// Don't delete - we can't verify import status
						filesImported = false
						slog.Debug("Torrent will be removed but files kept - cannot verify import status (not in DB or incoming)",
							"hash", normalizedHash,
							"save_path", torrent.SavePath)
					}
				}
			}

			// Always remove torrent from qBittorrent when threshold is met
			// Files are only deleted if BOTH conditions are met:
			// 1. Torrent has been removed from qBittorrent (verified after deletion)
			// 2. Files have been imported (imported_at IS NOT NULL in database)
			if err := qb.DeleteTorrent(ctx, normalizedHash, false); err != nil {
				slog.Error("Failed to remove torrent from qBittorrent",
					"hash", normalizedHash,
					"error", err)
				continue
			}

			// Verify torrent was actually removed from qBittorrent
			_, verifyErr := qb.GetTorrentByHash(ctx, normalizedHash)
			if verifyErr == nil {
				// Torrent still exists - deletion may have failed silently
				slog.Warn("Torrent still exists in qBittorrent after deletion attempt - skipping file cleanup",
					"hash", normalizedHash)
				// Still remove torrent_hash from database entries
				database.DB.Exec("UPDATE movies SET torrent_hash = NULL WHERE LOWER(torrent_hash) = $1", normalizedHash)
				database.DB.Exec("UPDATE episodes SET torrent_hash = NULL WHERE LOWER(torrent_hash) = $1", normalizedHash)
				continue
			}

			slog.Info("Removed torrent from qBittorrent",
				"hash", normalizedHash,
				"ratio", torrent.Ratio,
				"files_imported", filesImported)

			// Clean up files from incoming folder ONLY if BOTH conditions are met:
			// 1. Torrent has been removed from qBittorrent (verified above)
			// 2. Files have been imported (imported_at IS NOT NULL, verified in database query above)
			if filesImported && incomingPath != "" && torrent.SavePath != "" {
				if strings.HasPrefix(torrent.SavePath, incomingPath) {
					// Verify the files have actually been moved (not just copied)
					// by checking that the final location exists and is different
					fileMoved := false
					if moviePath != "" && !strings.HasPrefix(moviePath, incomingPath) {
						// Movie has been moved to final location
						if _, err := os.Stat(moviePath); err == nil {
							fileMoved = true
						}
					} else if episodePath != "" && !strings.HasPrefix(episodePath, incomingPath) {
						// Episode has been moved to final location
						if _, err := os.Stat(episodePath); err == nil {
							fileMoved = true
						}
					}

					// Only delete incoming files if we're certain they've been moved
					if fileMoved {
						if err := cleanupIncomingFiles(torrent.SavePath, incomingPath); err != nil {
							slog.Error("Failed to cleanup incoming files",
								"save_path", torrent.SavePath,
								"incoming_path", incomingPath,
								"error", err)
						} else {
							slog.Info("Cleaned up incoming files - torrent removed and files imported",
								"save_path", torrent.SavePath,
								"movie_path", moviePath,
								"episode_path", episodePath)
						}
					} else {
						slog.Warn("Skipping incoming file cleanup - files may not have been moved yet",
							"save_path", torrent.SavePath,
							"movie_path", moviePath,
							"episode_path", episodePath)
					}
				}
			} else {
				slog.Info("Keeping incoming files - torrent removed but files not imported yet",
					"hash", normalizedHash,
					"save_path", torrent.SavePath,
					"files_imported", filesImported)
			}

			// Remove torrent_hash from database entries (after cleanup decision is made)
			database.DB.Exec("UPDATE movies SET torrent_hash = NULL WHERE LOWER(torrent_hash) = $1", normalizedHash)
			database.DB.Exec("UPDATE episodes SET torrent_hash = NULL WHERE LOWER(torrent_hash) = $1", normalizedHash)

			cleanedCount++
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

// SeedingStatus represents the seeding status of a torrent
type SeedingStatus struct {
	IsSeeding          bool    // Whether torrent is actively seeding
	Ratio              float64 // Current seed ratio
	SeedingTimeMinutes float64 // Seeding time in minutes
	MeetsCriteria      bool    // Whether seeding criteria is met (1440 min OR 2.0 ratio)
	TorrentExists      bool    // Whether torrent exists in qBittorrent
}

// GetSeedingStatus gets the seeding status for a torrent hash
func GetSeedingStatus(ctx context.Context, cfg *config.Config, qb *QBittorrentClient, torrentHash string) (*SeedingStatus, error) {
	if qb == nil || torrentHash == "" {
		return &SeedingStatus{}, nil
	}

	// Get detailed info with ratio and seeding time
	torrents, err := qb.GetTorrentsDetailed(ctx, "")
	if err != nil {
		return &SeedingStatus{}, err
	}

	return GetSeedingStatusFromList(torrents, torrentHash), nil
}

// GetSeedingStatusFromList gets the seeding status for a torrent hash using a provided list of torrents
func GetSeedingStatusFromList(torrents []TorrentStatus, torrentHash string) *SeedingStatus {
	status := &SeedingStatus{
		IsSeeding:          false,
		Ratio:              0.0,
		SeedingTimeMinutes: 0.0,
		MeetsCriteria:      false,
		TorrentExists:      false,
	}

	if torrentHash == "" {
		return status
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
		return status
	}

	status.TorrentExists = true
	status.Ratio = detailedTorrent.Ratio
	status.SeedingTimeMinutes = float64(detailedTorrent.SeedingTime) / 60.0

	// Check if torrent is seeding
	state := strings.ToLower(detailedTorrent.State)
	seedingStates := []string{"uploading", "stalledup", "queuedup", "pausedup", "seeding"}
	for _, seedState := range seedingStates {
		if state == seedState {
			status.IsSeeding = true
			break
		}
	}

	// Check if seeding criteria is met
	meetsTimeThreshold := status.SeedingTimeMinutes >= SeedingThresholdMinutes
	meetsRatioThreshold := status.Ratio >= SeedingThresholdRatio
	status.MeetsCriteria = meetsTimeThreshold || meetsRatioThreshold

	return status
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

	// IMPORTANT: Only remove torrent from qBittorrent WITHOUT deleting files
	// The files may still be needed for seeding. We'll manually clean up
	// only the incoming folder files after verifying they've been moved.
	if err := qb.DeleteTorrent(ctx, normalizedHash, false); err != nil {
		slog.Error("Failed to remove torrent from qBittorrent on import",
			"hash", normalizedHash,
			"error", err)
		// Continue with file cleanup even if torrent removal fails
	}

	// Clean up files from incoming folder ONLY if:
	// 1. Files are confirmed to be in incoming folder (oldPath is in incoming)
	// 2. Files have been imported (moved to final location, not just copied)
	// 3. Torrent has been removed from qBittorrent (so files aren't actively being used)
	var incomingPath string
	var finalPath string
	var mediaType string

	if strings.HasPrefix(filePath, cfg.IncomingMoviesPath) {
		incomingPath = cfg.IncomingMoviesPath
		mediaType = "movie"
		// Get the final path from database
		err := database.DB.QueryRow(`
			SELECT path FROM movies 
			WHERE LOWER(torrent_hash) = $1 
			AND path NOT LIKE $2 || '%'
			LIMIT 1`, normalizedHash, cfg.IncomingMoviesPath).Scan(&finalPath)
		if err != nil {
			slog.Debug("Could not find final movie path in database", "hash", normalizedHash, "error", err)
		}
	} else if strings.HasPrefix(filePath, cfg.IncomingShowsPath) {
		incomingPath = cfg.IncomingShowsPath
		mediaType = "show"
		// Get the final path from database (any episode from this torrent)
		err := database.DB.QueryRow(`
			SELECT e.file_path FROM episodes e
			WHERE LOWER(e.torrent_hash) = $1 
			AND e.file_path NOT LIKE $2 || '%'
			LIMIT 1`, normalizedHash, cfg.IncomingShowsPath).Scan(&finalPath)
		if err != nil {
			slog.Debug("Could not find final episode path in database", "hash", normalizedHash, "error", err)
		}
	}

	if incomingPath != "" && finalPath != "" {
		// Verify the file has actually been moved to the final location
		if _, err := os.Stat(finalPath); err == nil {
			// File exists at final location - safe to delete incoming files
			fileDir := filepath.Dir(filePath)
			if strings.HasPrefix(fileDir, incomingPath) {
				if err := cleanupIncomingFiles(fileDir, incomingPath); err != nil {
					slog.Error("Failed to cleanup incoming files on import",
						"file_dir", fileDir,
						"incoming_path", incomingPath,
						"final_path", finalPath,
						"error", err)
				} else {
					slog.Info("Cleaned up incoming files on import after verifying move",
						"file_dir", fileDir,
						"final_path", finalPath,
						"media_type", mediaType)
				}
			}
		} else {
			slog.Warn("Skipping incoming file cleanup on import - final file not found",
				"file_path", filePath,
				"final_path", finalPath,
				"error", err)
		}
	} else if incomingPath != "" && finalPath == "" {
		slog.Debug("Skipping incoming file cleanup on import - file not moved yet or not found in database",
			"file_path", filePath,
			"incoming_path", incomingPath)
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

	// Try to find a torrent with matching save path
	ctx := context.Background()
	torrents, err := qb.GetTorrentsDetailed(ctx, "")
	if err != nil {
		slog.Debug("Failed to get torrents for hash linking", "file_path", filePath, "error", err)
		return
	}

	var matchedHash string
	for _, torrent := range torrents {
		normalizedHash := strings.ToLower(torrent.Hash)

		// Check if file is inside this torrent's save path
		if strings.HasPrefix(filePath, torrent.SavePath) {
			relPath, err := filepath.Rel(torrent.SavePath, filePath)
			if err == nil {
				// Fetch files for this torrent to see if our file is part of it
				tFiles, err := qb.GetTorrentFiles(ctx, normalizedHash)
				if err == nil {
					for _, tf := range tFiles {
						if tf.Name == relPath {
							matchedHash = normalizedHash
							slog.Debug("Linked torrent hash to file via exact file match",
								"hash", normalizedHash,
								"file_path", filePath,
								"torrent_file", tf.Name,
								"media_type", mediaType)
							break
						}
					}
				}
			}
		}

		if matchedHash != "" {
			break
		}
	}

	// Link the hash if we found a match
	if matchedHash != "" {
		switch mediaType {
		case "movie":
			result, err := database.DB.Exec(`
				UPDATE movies 
				SET torrent_hash = $1 
				WHERE path = $2 AND (torrent_hash IS NULL OR torrent_hash = '')`,
				matchedHash, filePath)
			if err != nil {
				slog.Debug("Failed to update movie with torrent hash", "file_path", filePath, "error", err)
			} else {
				rowsAffected, _ := result.RowsAffected()
				if rowsAffected > 0 {
					slog.Info("Linked torrent hash to movie",
						"hash", matchedHash,
						"file_path", filePath)
				}
			}
		case "show":
			result, err := database.DB.Exec(`
				UPDATE episodes 
				SET torrent_hash = $1 
				WHERE file_path = $2 AND (torrent_hash IS NULL OR torrent_hash = '')`,
				matchedHash, filePath)
			if err != nil {
				slog.Debug("Failed to update episode with torrent hash", "file_path", filePath, "error", err)
			} else {
				rowsAffected, _ := result.RowsAffected()
				if rowsAffected > 0 {
					slog.Info("Linked torrent hash to episode",
						"hash", matchedHash,
						"file_path", filePath)
				}
			}
		}
	} else {
		slog.Debug("Could not find matching torrent for file",
			"file_path", filePath,
			"media_type", mediaType,
			"torrent_count", len(torrents))
	}
}

// StartSeedingCleanupWorker starts a background worker that periodically checks and cleans up seeding torrents
func StartSeedingCleanupWorker(cfg *config.Config, qb *QBittorrentClient) {
	slog.Info("Starting seeding cleanup background worker")

	go func() {
		ticker := time.NewTicker(1 * time.Hour) // Check every 1 hour
		defer ticker.Stop()

		for range ticker.C {
			slog.Debug("Running seeding cleanup check")
			count, err := CheckAndCleanupSeedingTorrents(context.Background(), cfg, qb)
			if err != nil {
				slog.Error("Error during seeding cleanup", "error", err)
			} else if count > 0 {
				slog.Info("Seeding cleanup completed", "torrents_cleaned", count)
			}
		}
	}()
}
