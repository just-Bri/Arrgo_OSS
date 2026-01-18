package services

import (
	"Arrgo/config"
	"Arrgo/database"
	"Arrgo/models"
	"fmt"
	"os"
	"path/filepath"
)

func RenameAndMoveMovie(cfg *config.Config, movieID int) error {
	var m models.Movie
	query := `SELECT id, title, year, tmdb_id, path FROM movies WHERE id = $1`
	err := database.DB.QueryRow(query, movieID).Scan(&m.ID, &m.Title, &m.Year, &m.TMDBID, &m.Path)
	if err != nil {
		return err
	}

	if m.TMDBID == "" {
		return fmt.Errorf("movie must be matched before renaming")
	}

	ext := filepath.Ext(m.Path)
	newName := fmt.Sprintf("%s (%d) {tmdb-%s}%s", m.Title, m.Year, m.TMDBID, ext)
	
	// Create destination directory: Movies/Title (Year) {tmdb-id}/Title (Year) {tmdb-id}.ext
	destDirName := fmt.Sprintf("%s (%d) {tmdb-%s}", m.Title, m.Year, m.TMDBID)
	destDirPath := filepath.Join(cfg.MoviesPath, destDirName)
	destPath := filepath.Join(destDirPath, newName)

	if err := os.MkdirAll(destDirPath, 0755); err != nil {
		return err
	}

	if m.Path == destPath {
		return nil // Already in correct place
	}

	if err := os.Rename(m.Path, destPath); err != nil {
		return err
	}

	// Update DB with new path and status
	updateQuery := `UPDATE movies SET path = $1, status = 'ready', updated_at = CURRENT_TIMESTAMP WHERE id = $2`
	_, err = database.DB.Exec(updateQuery, destPath, m.ID)
	
	// TODO: Cleanup old directory if empty
	
	return err
}

func RenameAndMoveEpisode(cfg *config.Config, episodeID int) error {
	var e models.Episode
	var s models.Season
	var sh models.Show
	
	query := `
		SELECT e.id, e.episode_number, e.title, e.file_path, s.season_number, sh.title, sh.year, sh.tvdb_id
		FROM episodes e
		JOIN seasons s ON e.season_id = s.id
		JOIN shows sh ON s.show_id = sh.id
		WHERE e.id = $1
	`
	err := database.DB.QueryRow(query, episodeID).Scan(&e.ID, &e.EpisodeNumber, &e.Title, &e.FilePath, &s.SeasonNumber, &sh.Title, &sh.Year, &sh.TVDBID)
	if err != nil {
		return err
	}

	// TV Shows: Title (Year)/Season XX/Title - SXXEXX - Episode Title.ext
	ext := filepath.Ext(e.FilePath)
	
	showDirName := fmt.Sprintf("%s (%d)", sh.Title, sh.Year)
	seasonDirName := fmt.Sprintf("Season %02d", s.SeasonNumber)
	
	newFileName := fmt.Sprintf("%s - S%02dE%02d - %s%s", sh.Title, s.SeasonNumber, e.EpisodeNumber, e.Title, ext)
	
	destDirPath := filepath.Join(cfg.TVShowsPath, showDirName, seasonDirName)
	destPath := filepath.Join(destDirPath, newFileName)

	if err := os.MkdirAll(destDirPath, 0755); err != nil {
		return err
	}

	if e.FilePath == destPath {
		return nil
	}

	if err := os.Rename(e.FilePath, destPath); err != nil {
		return err
	}

	updateQuery := `UPDATE episodes SET file_path = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`
	_, err = database.DB.Exec(updateQuery, destPath, e.ID)
	return err
}
