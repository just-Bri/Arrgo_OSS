package services

import (
	"Arrgo/config"
	"Arrgo/database"
	"Arrgo/models"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
)

func safeRename(src, dst string) error {
	// Try renaming first (efficient if on same device)
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}

	// Fallback for "invalid cross-device link" (EXDEV)
	log.Printf("[RENAMER] Cross-device move detected, falling back to copy+delete: %s -> %s", src, dst)

	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return err
	}

	// Ensure everything is written to disk
	if err := destFile.Sync(); err != nil {
		return err
	}

	// Important to close before removing
	sourceFile.Close()
	destFile.Close()

	return os.Remove(src)
}

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

func RenameAndMoveMovie(cfg *config.Config, movieID int) error {
	var m models.Movie
	query := `SELECT id, title, year, tmdb_id, imdb_id, path, quality, size, poster_path FROM movies WHERE id = $1`
	err := database.DB.QueryRow(query, movieID).Scan(&m.ID, &m.Title, &m.Year, &m.TMDBID, &m.IMDBID, &m.Path, &m.Quality, &m.Size, &m.PosterPath)
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
				CleanupEmptyDirs(cfg.IncomingMoviesPath)
				return nil
			} else if comp == 0 {
				// Qualities are equal, compare size.
				if m.Size <= existingSize {
					// Candidate is smaller or equal size. Delete candidate.
					log.Printf("[RENAMER] Candidate %s has same quality (%s) but smaller/equal size than existing. Deleting candidate.", m.Path, m.Quality)
					oldPath := m.Path
					os.Remove(oldPath)
					database.DB.Exec("DELETE FROM movies WHERE id = $1", m.ID)
					CleanupEmptyDirs(cfg.IncomingMoviesPath)
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
	if err := safeRename(oldPath, destPath); err != nil {
		return err
	}

	// Move the poster if it exists and is in the same directory as the movie
	newPosterPath := ""
	if m.PosterPath != "" {
		if strings.HasPrefix(m.PosterPath, filepath.Dir(oldPath)) {
			posterExt := filepath.Ext(m.PosterPath)
			newPosterPath = filepath.Join(destDirPath, "poster"+posterExt)
			if err := safeRename(m.PosterPath, newPosterPath); err != nil {
				log.Printf("[RENAMER] Failed to move poster: %v", err)
				newPosterPath = "" // Reset if failed
			}
		}
	}

	// Cleanup old directory if it was in incoming
	CleanupEmptyDirs(cfg.IncomingMoviesPath)

	// Update DB with new path and status
	updateQuery := `UPDATE movies SET path = $1, poster_path = $2, status = 'ready', updated_at = CURRENT_TIMESTAMP WHERE id = $3`
	_, err = database.DB.Exec(updateQuery, destPath, newPosterPath, m.ID)
	if err != nil {
		return err
	}

	// Trigger subtitle download
	go func() {
		if err := DownloadSubtitlesForMovie(cfg, m.IMDBID, m.TMDBID, m.Title, m.Year, destDirPath); err != nil {
			log.Printf("[RENAMER] Subtitle download failed for %s: %v", m.Title, err)
		}
	}()

	return nil
}

func RenameAndMoveEpisode(cfg *config.Config, episodeID int) error {
	var e models.Episode
	var s models.Season
	var sh models.Show

	query := `
		SELECT e.id, e.episode_number, e.title, e.file_path, e.quality, e.size, s.season_number, sh.title, sh.year, sh.tmdb_id, sh.imdb_id, sh.poster_path
		FROM episodes e
		JOIN seasons s ON e.season_id = s.id
		JOIN shows sh ON s.show_id = sh.id
		WHERE e.id = $1
	`
	err := database.DB.QueryRow(query, episodeID).Scan(&e.ID, &e.EpisodeNumber, &e.Title, &e.FilePath, &e.Quality, &e.Size, &s.SeasonNumber, &sh.Title, &sh.Year, &sh.TVDBID, &sh.IMDBID, &sh.PosterPath)
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
				CleanupEmptyDirs(cfg.IncomingTVPath)
				return nil
			} else if comp == 0 {
				if e.Size <= existingSize {
					log.Printf("[RENAMER] Candidate episode %s has same quality but smaller/equal size. Deleting candidate.", e.FilePath)
					oldPath := e.FilePath
					os.Remove(oldPath)
					database.DB.Exec("DELETE FROM episodes WHERE id = $1", e.ID)
					CleanupEmptyDirs(cfg.IncomingTVPath)
					return nil
				}
			}

			log.Printf("[RENAMER] Candidate episode %s is better. Replacing existing.", e.FilePath)
			os.Remove(destPath)
			database.DB.Exec("DELETE FROM episodes WHERE file_path = $1", destPath)
		}
	}

	oldPath := e.FilePath
	if err := safeRename(oldPath, destPath); err != nil {
		return err
	}

	// If there's a show poster in the incoming folder, move it to the show root
	newShowPosterPath := sh.PosterPath
	if sh.PosterPath != "" {
		if strings.HasPrefix(sh.PosterPath, cfg.IncomingTVPath) {
			// Show root in library
			showRoot := filepath.Dir(filepath.Dir(destDirPath))
			posterExt := filepath.Ext(sh.PosterPath)
			newShowPosterPath = filepath.Join(showRoot, "poster"+posterExt)
			
			// Only move if destination doesn't exist yet
			if _, err := os.Stat(newShowPosterPath); os.IsNotExist(err) {
				if err := safeRename(sh.PosterPath, newShowPosterPath); err == nil {
					database.DB.Exec("UPDATE shows SET poster_path = $1 WHERE id = (SELECT show_id FROM seasons WHERE id = $2)", newShowPosterPath, e.SeasonID)
				}
			} else {
				// Destination exists, just update DB path to match
				database.DB.Exec("UPDATE shows SET poster_path = $1 WHERE id = (SELECT show_id FROM seasons WHERE id = $2)", newShowPosterPath, e.SeasonID)
			}
		}
	}

	CleanupEmptyDirs(cfg.IncomingTVPath)

	updateQuery := `UPDATE episodes SET file_path = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`
	_, err = database.DB.Exec(updateQuery, destPath, e.ID)
	if err != nil {
		return err
	}

	// Trigger subtitle download for episode
	go func() {
		if err := DownloadSubtitlesForEpisode(cfg, sh.IMDBID, sh.TMDBID, sh.Title, s.SeasonNumber, e.EpisodeNumber, destDirPath); err != nil {
			log.Printf("[RENAMER] Subtitle download failed for %s S%02dE%02d: %v", sh.Title, s.SeasonNumber, e.EpisodeNumber, err)
		}
	}()

	return nil
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
