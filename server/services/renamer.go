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
	"regexp"
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
	
	// Create destination directory if it doesn't exist (should already be created by caller, but safe check)
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

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

func copyFile(src, dst string) error {
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

	return destFile.Sync()
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
	log.Printf("[RENAMER] Successfully moved movie: %s -> %s", oldPath, destPath)

	// Copy the poster if it exists and is in the same directory as the movie
	newPosterPath := ""
	if m.PosterPath != "" {
		if strings.HasPrefix(m.PosterPath, filepath.Dir(oldPath)) {
			posterExt := filepath.Ext(m.PosterPath)
			newPosterPath = filepath.Join(destDirPath, "poster"+posterExt)
			if err := copyFile(m.PosterPath, newPosterPath); err != nil {
				log.Printf("[RENAMER] Failed to copy poster: %v", err)
				newPosterPath = "" // Reset if failed
			} else {
				log.Printf("[RENAMER] Copied movie poster: %s -> %s", m.PosterPath, newPosterPath)
			}
		} else {
			// If it's a TMDB URL or already in library, keep it as is
			newPosterPath = m.PosterPath
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
		if err := DownloadSubtitlesForMovie(cfg, m.ID); err != nil {
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
		SELECT e.id, e.episode_number, e.title, e.file_path, e.quality, e.size, s.season_number, sh.title, sh.year, sh.tvdb_id, sh.imdb_id, sh.poster_path
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
	
	// Better episode title cleaning
	epTitle := e.Title
	// If title contains the extension or looks like a scene filename, deep clean it
	if strings.Contains(epTitle, ".") || strings.Contains(epTitle, "-") || strings.Contains(strings.ToLower(epTitle), "s0") {
		// Strip extension if present in title
		if filepath.Ext(epTitle) != "" {
			epTitle = strings.TrimSuffix(epTitle, filepath.Ext(epTitle))
		}
		
		// Remove common uploader/junk patterns
		// 1. Try to find the title before SXXEXX
		junkRegex := regexp.MustCompile(`(?i)(.*?)(S\d+E\d+).*`)
		if matches := junkRegex.FindStringSubmatch(epTitle); len(matches) > 1 {
			// If it contains SXXEXX, usually the stuff AFTER it is junk, 
			// and if the stuff BEFORE it is just the show title, we want to be careful.
			// However, if we already have official titles synced, this logic shouldn't even hit.
			prefix := strings.TrimSpace(matches[1])
			if prefix != "" && !strings.EqualFold(prefix, sh.Title) {
				epTitle = prefix
			} else {
				// If prefix is empty or just the show title, try to find something AFTER SXXEXX
				afterRegex := regexp.MustCompile(`(?i)S\d+E\d+\s*[-_.]?\s*(.*)`)
				if afterMatches := afterRegex.FindStringSubmatch(epTitle); len(afterMatches) > 1 {
					epTitle = afterMatches[1]
				}
			}
		}
		
		// Final strip of common scene tags
		cleanRegex := regexp.MustCompile(`(?i)\s*(- IMPORTED|RARBG|YTS|YIFY|Eztv|1337x|GalaxyRG|TGX|PSA|VXT|EVO|MeGusta|AVS|SNEAKY|BRRip|WEB-DL|BluRay|1080p|720p|2160p|x264|x265|HEVC|H264|H265).*$`)
		epTitle = cleanRegex.ReplaceAllString(epTitle, "")
		epTitle = strings.Trim(epTitle, " -._")
	}
	
	if epTitle == "" || strings.EqualFold(epTitle, sh.Title) {
		epTitle = fmt.Sprintf("Episode %d", e.EpisodeNumber)
	}
	
	sanitizedEpTitle := sanitizePath(epTitle)

	showDirName := fmt.Sprintf("%s (%d)", sanitizedShowTitle, sh.Year)
	if sh.TVDBID != "" {
		showDirName = fmt.Sprintf("%s (%d) {tvdb-%s}", sanitizedShowTitle, sh.Year, sh.TVDBID)
	}
	seasonDirName := fmt.Sprintf("Season %02d", s.SeasonNumber)

	newFileName := fmt.Sprintf("%s - S%02dE%02d - %s%s", sanitizedShowTitle, s.SeasonNumber, e.EpisodeNumber, sanitizedEpTitle, ext)

	destDirPath := filepath.Join(cfg.ShowsPath, showDirName, seasonDirName)
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
				CleanupEmptyDirs(cfg.IncomingShowsPath)
				return nil
			} else if comp == 0 {
				if e.Size <= existingSize {
					log.Printf("[RENAMER] Candidate episode %s has same quality but smaller/equal size. Deleting candidate.", e.FilePath)
					oldPath := e.FilePath
					os.Remove(oldPath)
					database.DB.Exec("DELETE FROM episodes WHERE id = $1", e.ID)
					CleanupEmptyDirs(cfg.IncomingShowsPath)
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
	log.Printf("[RENAMER] Successfully moved episode: %s -> %s", oldPath, destPath)

	updateQuery := `UPDATE episodes SET file_path = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`
	_, err = database.DB.Exec(updateQuery, destPath, e.ID)
	if err != nil {
		return err
	}

	// Cleanup old directory if it was in incoming
	CleanupEmptyDirs(cfg.IncomingShowsPath)

	// Trigger subtitle download for episode
	go func() {
		if err := DownloadSubtitlesForEpisode(cfg, e.ID); err != nil {
			log.Printf("[RENAMER] Subtitle download failed for %s S%02dE%02d: %v", sh.Title, s.SeasonNumber, e.EpisodeNumber, err)
		}
	}()

	return nil
}

func RenameAndMoveShow(cfg *config.Config, showID int) error {
	var sh models.Show
	err := database.DB.QueryRow("SELECT id, title, year, tvdb_id, path, poster_path FROM shows WHERE id = $1", showID).Scan(&sh.ID, &sh.Title, &sh.Year, &sh.TVDBID, &sh.Path, &sh.PosterPath)
	if err != nil {
		return err
	}

	sanitizedShowTitle := sanitizePath(sh.Title)
	showDirName := fmt.Sprintf("%s (%d)", sanitizedShowTitle, sh.Year)
	if sh.TVDBID != "" {
		showDirName = fmt.Sprintf("%s (%d) {tvdb-%s}", sanitizedShowTitle, sh.Year, sh.TVDBID)
	}
	destShowPath := filepath.Join(cfg.ShowsPath, showDirName)

	if err := os.MkdirAll(destShowPath, 0755); err != nil {
		return err
	}

	// Move show poster if it exists and is in the incoming folder
	newPosterPath := sh.PosterPath
	if sh.PosterPath != "" && strings.HasPrefix(sh.PosterPath, cfg.IncomingShowsPath) {
		ext := filepath.Ext(sh.PosterPath)
		newPosterPath = filepath.Join(destShowPath, "poster"+ext)
		if _, err := os.Stat(newPosterPath); os.IsNotExist(err) {
			// We'll copy the poster instead of moving it, so the original can be nuked by cleanup
			if err := copyFile(sh.PosterPath, newPosterPath); err != nil {
				log.Printf("[RENAMER] Failed to copy show poster: %v", err)
				newPosterPath = sh.PosterPath 
			} else {
				log.Printf("[RENAMER] Copied show poster: %s -> %s", sh.PosterPath, newPosterPath)
			}
		}
	}

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

	// Update show status and path in DB
	_, err = database.DB.Exec("UPDATE shows SET path = $1, poster_path = $2, status = 'ready', updated_at = CURRENT_TIMESTAMP WHERE id = $3", destShowPath, newPosterPath, showID)
	if err != nil {
		log.Printf("[RENAMER] Error updating show %d in DB: %v", showID, err)
	}

	CleanupEmptyDirs(cfg.IncomingShowsPath)

	return nil
}
