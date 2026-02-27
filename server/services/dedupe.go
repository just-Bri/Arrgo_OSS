package services

import (
	"Arrgo/config"
	"Arrgo/database"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// DedupeResult contains statistics about the deduplication process
type DedupeResult struct {
	ItemsScanned   int      `json:"items_scanned"`
	ItemsRemoved   int      `json:"items_removed"`
	FoldersRemoved int      `json:"folders_removed"`
	BytesFreed     int64    `json:"bytes_freed"`
	Messages       []string `json:"messages"`
}

// DeduplicateMovies scans the movies directory for duplicates based on TMDB/IMDB IDs
func DeduplicateMovies(cfg *config.Config) (*DedupeResult, error) {
	result := &DedupeResult{}
	moviesMap := make(map[string][]string) // ID -> list of paths

	// Read the movies directory
	entries, err := os.ReadDir(cfg.MoviesPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read movies directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue // Movies should be in their own folders
		}

		folderPath := filepath.Join(cfg.MoviesPath, entry.Name())
		result.ItemsScanned++

		// Extract ID from folder name using existing parser logic
		_, _, tmdbID, _, imdbID := ParseMediaName(entry.Name())

		idKey := ""
		if tmdbID != "" {
			idKey = "tmdb-" + tmdbID
		} else if imdbID != "" {
			idKey = "imdb-" + imdbID
		}

		if idKey != "" {
			moviesMap[idKey] = append(moviesMap[idKey], folderPath)
		}
	}

	// Process duplicates
	for id, paths := range moviesMap {
		if len(paths) <= 1 {
			continue // No duplicates
		}

		slog.Info("Found duplicate movie folders", "id", id, "count", len(paths))
		result.Messages = append(result.Messages, fmt.Sprintf("Found %d folders for %s", len(paths), id))

		// Find the best video file across all duplicate folders
		type videoFile struct {
			folder string
			path   string
			size   int64
		}

		var allVideos []videoFile

		for _, folder := range paths {
			files, err := os.ReadDir(folder)
			if err != nil {
				slog.Error("Failed to read duplicate movie folder", "folder", folder, "error", err)
				continue
			}

			for _, file := range files {
				if file.IsDir() {
					continue
				}

				ext := strings.ToLower(filepath.Ext(file.Name()))
				if ext == ".mkv" || ext == ".mp4" || ext == ".avi" {
					fullPath := filepath.Join(folder, file.Name())
					info, err := file.Info()
					if err == nil {
						allVideos = append(allVideos, videoFile{
							folder: folder,
							path:   fullPath,
							size:   info.Size(),
						})
					}
				}
			}
		}

		if len(allVideos) <= 1 {
			continue // No duplicate video files to resolve
		}

		// Sort by size descending
		sort.Slice(allVideos, func(i, j int) bool {
			return allVideos[i].size > allVideos[j].size
		})

		bestVideo := allVideos[0]

		// Map of folders to keep vs folders to delete
		foldersToDelete := make(map[string]bool)
		for _, p := range paths {
			if p != bestVideo.folder {
				foldersToDelete[p] = true
			}
		}

		// Delete the duplicate video files and their DB entries
		for _, vid := range allVideos[1:] {
			slog.Info("Deleting duplicate movie file", "path", vid.path, "size", vid.size)
			if err := os.Remove(vid.path); err == nil {
				result.ItemsRemoved++
				result.BytesFreed += vid.size
				result.Messages = append(result.Messages, fmt.Sprintf("Removed duplicate: %s", filepath.Base(vid.path)))

				// Optional: Delete from database if it exists
				database.DB.Exec("DELETE FROM movies WHERE path = $1", vid.path)
			} else {
				slog.Error("Failed to delete duplicate movie file", "path", vid.path, "error", err)
			}
		}

		// Try to delete the empty duplicate folders
		for folder := range foldersToDelete {
			// Only remove if it's empty (or only contains nfos/posters which we can delete)
			if err := os.RemoveAll(folder); err == nil {
				result.FoldersRemoved++
			}
		}
	}

	return result, nil
}

// DeduplicateShows scans the shows directory, merges duplicate show folders, and dedupes episodes
func DeduplicateShows(cfg *config.Config) (*DedupeResult, error) {
	result := &DedupeResult{}
	showsMap := make(map[string][]string) // ID -> list of paths

	// Read the shows directory
	entries, err := os.ReadDir(cfg.ShowsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read shows directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		folderPath := filepath.Join(cfg.ShowsPath, entry.Name())
		result.ItemsScanned++

		_, _, tmdbID, tvdbID, _ := ParseMediaName(entry.Name())

		idKey := ""
		if tvdbID != "" {
			idKey = "tvdb-" + tvdbID
		} else if tmdbID != "" {
			idKey = "tmdb-" + tmdbID
		}

		if idKey != "" {
			showsMap[idKey] = append(showsMap[idKey], folderPath)
		}
	}

	// 1. Merge duplicate show folders
	for id, paths := range showsMap {
		if len(paths) <= 1 {
			// Single folder, just dedupe episodes inside it
			dedupeEpisodesInShow(paths[0], result)
			continue
		}

		slog.Info("Found duplicate show folders", "id", id, "count", len(paths))
		result.Messages = append(result.Messages, fmt.Sprintf("Merging %d folders for %s", len(paths), id))

		// Assume the first one is the "primary" one. Better: pick the one with clean naming
		primaryFolder := paths[0]
		for _, p := range paths {
			// If it matches exactly the standard naming "Title (Year) {id}", it's the best primary
			// Here we just pick one heuristics might be better
			if len(p) < len(primaryFolder) {
				primaryFolder = p // shorter might be cleaner, arbitrary
			}
		}

		for _, p := range paths {
			if p == primaryFolder {
				continue
			}
			mergeShowFolders(p, primaryFolder, result)
		}

		// Dedupe the newly consolidated primary folder
		dedupeEpisodesInShow(primaryFolder, result)
	}

	return result, nil
}

// dedupeEpisodesInShow finds and removes duplicate SXXEXX video files in a show directory
func dedupeEpisodesInShow(showPath string, result *DedupeResult) {
	// Map of S01E01 -> list of video files
	episodesMap := make(map[string][]struct {
		path string
		size int64
	})

	seasonEpRegex := regexp.MustCompile(`(?i)(S\d{2,}E\d{2,})`)

	err := filepath.Walk(showPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".mkv" || ext == ".mp4" || ext == ".avi" {
			match := seasonEpRegex.FindString(path)
			if match != "" {
				key := strings.ToUpper(match)
				episodesMap[key] = append(episodesMap[key], struct {
					path string
					size int64
				}{path, info.Size()})
			}
		}
		return nil
	})

	if err != nil {
		slog.Error("Failed to walk show directory for dedupe", "path", showPath, "error", err)
		return
	}

	for _, files := range episodesMap {
		if len(files) <= 1 {
			continue
		}

		// Sort by size descending
		sort.Slice(files, func(i, j int) bool {
			return files[i].size > files[j].size
		})

		// Keep files[0], delete the rest
		for _, f := range files[1:] {
			slog.Info("Deleting duplicate episode file", "path", f.path, "size", f.size)
			if err := os.Remove(f.path); err == nil {
				result.ItemsRemoved++
				result.BytesFreed += f.size
				result.Messages = append(result.Messages, fmt.Sprintf("Removed duplicate episode: %s", filepath.Base(f.path)))

				database.DB.Exec("DELETE FROM episodes WHERE file_path = $1", f.path)
			}
		}
	}
}

// mergeShowFolders moves everything from srcShowPath to destShowPath and deletes src
func mergeShowFolders(srcShowPath, destShowPath string, result *DedupeResult) {
	slog.Info("Merging show folders", "from", srcShowPath, "to", destShowPath)

	err := filepath.Walk(srcShowPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		relPath, err := filepath.Rel(srcShowPath, path)
		if err != nil || relPath == "." {
			return nil
		}

		destPath := filepath.Join(destShowPath, relPath)

		if info.IsDir() {
			os.MkdirAll(destPath, 0755)
			return nil
		}

		// It's a file. Let's move it over if it doesn't exist, else leave it to be cleaned up
		if _, err := os.Stat(destPath); os.IsNotExist(err) {
			slog.Info("Moving file to primary show folder", "from", path, "to", destPath)
			if err := os.Rename(path, destPath); err != nil {
				// Fallback to copy if cross-device, though unlikely in a merge
				copyFile(path, destPath)
				os.Remove(path)
			}

			// Update database just in case
			database.DB.Exec("UPDATE episodes SET file_path = $1 WHERE file_path = $2", destPath, path)
		}

		return nil
	})

	if err != nil {
		slog.Error("Failed to merge show folders", "src", srcShowPath, "error", err)
		return
	}

	// Now try to delete the source folder
	if err := os.RemoveAll(srcShowPath); err == nil {
		result.FoldersRemoved++
		result.Messages = append(result.Messages, fmt.Sprintf("Merged duplicate folder: %s", filepath.Base(srcShowPath)))
		database.DB.Exec("DELETE FROM shows WHERE path = $1", srcShowPath)
	}
}
