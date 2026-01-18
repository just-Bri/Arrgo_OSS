package services

import (
	"Arrgo/config"
	"Arrgo/database"
	"Arrgo/models"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func sanitizePath(name string) string {
	// Remove or replace characters that are problematic for filesystems
	// Specifically :, /, \, *, ?, ", <, >, |
	replacer := strings.NewReplacer(
		":", " -",
		"/", "-",
		"\\", "-",
		"*", "",
		"?", "",
		"\"", "",
		"<", "",
		">", "",
		"|", "-",
	)
	sanitized := replacer.Replace(name)
	// Remove trailing dots and spaces
	sanitized = strings.TrimRight(sanitized, ". ")
	return sanitized
}

func cleanupEmptyDirs(startPath string, stopAt string) {
	parent := filepath.Dir(startPath)
	// Don't go above the root scan paths
	if stopAt == "" || parent == stopAt || parent == "." || parent == "/" {
		return
	}

	files, err := os.ReadDir(parent)
	if err != nil {
		return
	}

	if len(files) == 0 {
		log.Printf("[CLEANUP] Removing empty directory: %s", parent)
		os.Remove(parent)
		cleanupEmptyDirs(parent, stopAt)
	}
}

func RenameAndMoveMovie(cfg *config.Config, movieID int) error {
	var m models.Movie
	query := `SELECT id, title, year, tmdb_id, path, quality, size FROM movies WHERE id = $1`
	err := database.DB.QueryRow(query, movieID).Scan(&m.ID, &m.Title, &m.Year, &m.TMDBID, &m.Path, &m.Quality, &m.Size)
	if err != nil {
		return err
	}

	if m.TMDBID == "" {
		return fmt.Errorf("movie must be matched before renaming")
	}

	ext := filepath.Ext(m.Path)
	sanitizedTitle := sanitizePath(m.Title)
	newName := fmt.Sprintf("%s (%d) {tmdb-%s}%s", sanitizedTitle, m.Year, m.TMDBID, ext)

	// Create destination directory: Movies/Title (Year) {tmdb-id}/Title (Year) {tmdb-id}.ext
	destDirName := fmt.Sprintf("%s (%d) {tmdb-%s}", sanitizedTitle, m.Year, m.TMDBID)
	destDirPath := filepath.Join(cfg.MoviesPath, destDirName)
	destPath := filepath.Join(destDirPath, newName)

	if err := os.MkdirAll(destDirPath, 0755); err != nil {
		return err
	}

	if m.Path == destPath {
		return nil // Already in correct place
	}

	// SMART RENAMING LOGIC: Quality Check
	if _, err := os.Stat(destPath); err == nil {
		// File already exists at destination. Let's compare quality.
		var existingQuality string
		var existingSize int64
		err = database.DB.QueryRow("SELECT quality, size FROM movies WHERE path = $1", destPath).Scan(&existingQuality, &existingSize)
		if err == nil {
			// Compare quality
			comp := CompareQuality(m.Quality, existingQuality)
			if comp < 0 {
				// Candidate is LOWER quality than existing.
				// Keep the existing file, delete the candidate.
				log.Printf("[RENAMER] Candidate %s is lower quality (%s) than existing (%s). Deleting candidate.", m.Path, m.Quality, existingQuality)
				oldPath := m.Path
				os.Remove(oldPath)
				database.DB.Exec("DELETE FROM movies WHERE id = $1", m.ID)
				cleanupEmptyDirs(oldPath, cfg.IncomingPath)
				return nil
			} else if comp == 0 {
				// Qualities are equal, compare size.
				if m.Size <= existingSize {
					// Candidate is smaller or equal size. Delete candidate.
					log.Printf("[RENAMER] Candidate %s has same quality (%s) but smaller/equal size than existing. Deleting candidate.", m.Path, m.Quality)
					oldPath := m.Path
					os.Remove(oldPath)
					database.DB.Exec("DELETE FROM movies WHERE id = $1", m.ID)
					cleanupEmptyDirs(oldPath, cfg.IncomingPath)
					return nil
				}
				// Candidate is larger, proceed to replace.
				log.Printf("[RENAMER] Candidate %s has same quality (%s) but larger size. Replacing existing.", m.Path, m.Quality)
			} else {
				// Candidate is HIGHER quality. Proceed to replace.
				log.Printf("[RENAMER] Candidate %s is higher quality (%s) than existing (%s). Replacing existing.", m.Path, m.Quality, existingQuality)
			}
			
			// If we are replacing, remove the existing file and its DB entry
			os.Remove(destPath)
			database.DB.Exec("DELETE FROM movies WHERE path = $1", destPath)
		}
	}

	// Move the file
	oldPath := m.Path
	if err := os.Rename(oldPath, destPath); err != nil {
		return err
	}

	// Cleanup old directory if it was in incoming
	cleanupEmptyDirs(oldPath, cfg.IncomingPath)

	// Update DB with new path and status
	updateQuery := `UPDATE movies SET path = $1, status = 'ready', updated_at = CURRENT_TIMESTAMP WHERE id = $2`
	_, err = database.DB.Exec(updateQuery, destPath, m.ID)

	return err
}

func RenameAndMoveEpisode(cfg *config.Config, episodeID int) error {
	var e models.Episode
	var s models.Season
	var sh models.Show

	query := `
		SELECT e.id, e.episode_number, e.title, e.file_path, e.quality, e.size, s.season_number, sh.title, sh.year, sh.tvdb_id
		FROM episodes e
		JOIN seasons s ON e.season_id = s.id
		JOIN shows sh ON s.show_id = sh.id
		WHERE e.id = $1
	`
	err := database.DB.QueryRow(query, episodeID).Scan(&e.ID, &e.EpisodeNumber, &e.Title, &e.FilePath, &e.Quality, &e.Size, &s.SeasonNumber, &sh.Title, &sh.Year, &sh.TVDBID)
	if err != nil {
		return err
	}

	// TV Shows: Title (Year) {tvdb-ID}/Season XX/Title - SXXEXX - Episode Title.ext
	ext := filepath.Ext(e.FilePath)

	sanitizedShowTitle := sanitizePath(sh.Title)
	sanitizedEpTitle := sanitizePath(e.Title)

	showDirName := fmt.Sprintf("%s (%d)", sanitizedShowTitle, sh.Year)
	if sh.TVDBID != "" {
		showDirName = fmt.Sprintf("%s (%d) {tvdb-%s}", sanitizedShowTitle, sh.Year, sh.TVDBID)
	}
	seasonDirName := fmt.Sprintf("Season %02d", s.SeasonNumber)

	newFileName := fmt.Sprintf("%s - S%02dE%02d - %s%s", sanitizedShowTitle, s.SeasonNumber, e.EpisodeNumber, sanitizedEpTitle, ext)

	destDirPath := filepath.Join(cfg.TVShowsPath, showDirName, seasonDirName)
	destPath := filepath.Join(destDirPath, newFileName)

	if err := os.MkdirAll(destDirPath, 0755); err != nil {
		return err
	}

	if e.FilePath == destPath {
		return nil
	}

	// SMART RENAMING LOGIC: Quality Check for Episodes
	if _, err := os.Stat(destPath); err == nil {
		var existingQuality string
		var existingSize int64
		err = database.DB.QueryRow("SELECT quality, size FROM episodes WHERE file_path = $1", destPath).Scan(&existingQuality, &existingSize)
		if err == nil {
			comp := CompareQuality(e.Quality, existingQuality)
			if comp < 0 {
				log.Printf("[RENAMER] Candidate episode %s is lower quality (%s) than existing (%s). Deleting candidate.", e.FilePath, e.Quality, existingQuality)
				oldPath := e.FilePath
				os.Remove(oldPath)
				database.DB.Exec("DELETE FROM episodes WHERE id = $1", e.ID)
				cleanupEmptyDirs(oldPath, cfg.IncomingPath)
				return nil
			} else if comp == 0 {
				if e.Size <= existingSize {
					log.Printf("[RENAMER] Candidate episode %s has same quality but smaller/equal size. Deleting candidate.", e.FilePath)
					oldPath := e.FilePath
					os.Remove(oldPath)
					database.DB.Exec("DELETE FROM episodes WHERE id = $1", e.ID)
					cleanupEmptyDirs(oldPath, cfg.IncomingPath)
					return nil
				}
			}
			
			log.Printf("[RENAMER] Candidate episode %s is better. Replacing existing.", e.FilePath)
			os.Remove(destPath)
			database.DB.Exec("DELETE FROM episodes WHERE file_path = $1", destPath)
		}
	}

	oldPath := e.FilePath
	if err := os.Rename(oldPath, destPath); err != nil {
		return err
	}

	cleanupEmptyDirs(oldPath, cfg.IncomingPath)

	updateQuery := `UPDATE episodes SET file_path = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`
	_, err = database.DB.Exec(updateQuery, destPath, e.ID)
	return err
}

func RenameAndMoveShow(cfg *config.Config, showID int) error {
	// Fetch all episodes for this show
	query := `
		SELECT e.id
		FROM episodes e
		JOIN seasons s ON e.season_id = s.id
		WHERE s.show_id = $1
	`
	rows, err := database.DB.Query(query, showID)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var epID int
		if err := rows.Scan(&epID); err != nil {
			continue
		}
		if err := RenameAndMoveEpisode(cfg, epID); err != nil {
			log.Printf("[RENAMER] Error renaming episode %d: %v", epID, err)
		}
	}

	return nil
}
