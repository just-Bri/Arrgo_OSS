package services

import (
	"Arrgo/config"
	"Arrgo/database"
	"Arrgo/models"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

func ScanShows(cfg *config.Config) error {
	paths := []string{cfg.TVShowsPath, cfg.IncomingPath}
	for _, p := range paths {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); os.IsNotExist(err) {
			continue
		}
		if err := scanShowPath(cfg, p); err != nil {
			return err
		}
	}
	return nil
}

func scanShowPath(cfg *config.Config, root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		showPath := filepath.Join(root, entry.Name())
		title, year := parseMovieName(entry.Name()) // Reuse parseMovieName for "Title (Year)"

		showID, err := upsertShow(models.Show{
			Title:  title,
			Year:   year,
			Path:   showPath,
			Status: "discovered",
		})
		if err != nil {
			continue
		}

		scanSeasons(showID, showPath)
	}

	return nil
}

func upsertShow(show models.Show) (int, error) {
	var id int
	query := `
		INSERT INTO shows (title, year, path, status, updated_at)
		VALUES ($1, $2, $3, $4, CURRENT_TIMESTAMP)
		ON CONFLICT (path) DO UPDATE SET
			title = EXCLUDED.title,
			year = EXCLUDED.year,
			updated_at = CURRENT_TIMESTAMP
		RETURNING id
	`
	err := database.DB.QueryRow(query, show.Title, show.Year, show.Path, show.Status).Scan(&id)
	return id, err
}

func scanSeasons(showID int, showPath string) {
	entries, err := os.ReadDir(showPath)
	if err != nil {
		return
	}

	seasonRegex := regexp.MustCompile(`(?i)Season\s+(\d+)`)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		matches := seasonRegex.FindStringSubmatch(entry.Name())
		if len(matches) < 2 {
			continue
		}

		seasonNum, _ := strconv.Atoi(matches[1])
		seasonPath := filepath.Join(showPath, entry.Name())

		seasonID, err := upsertSeason(showID, seasonNum)
		if err != nil {
			continue
		}

		scanEpisodes(seasonID, seasonPath)
	}
}

func upsertSeason(showID int, seasonNum int) (int, error) {
	var id int
	query := `
		INSERT INTO seasons (show_id, season_number, updated_at)
		VALUES ($1, $2, CURRENT_TIMESTAMP)
		ON CONFLICT (show_id, season_number) DO UPDATE SET
			updated_at = CURRENT_TIMESTAMP
		RETURNING id
	`
	err := database.DB.QueryRow(query, showID, seasonNum).Scan(&id)
	return id, err
}

func scanEpisodes(seasonID int, seasonPath string) {
	entries, err := os.ReadDir(seasonPath)
	if err != nil {
		return
	}

	// Match SXXEXX
	episodeRegex := regexp.MustCompile(`(?i)S\d+E(\d+)`)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if !movieExtensions[ext] {
			continue
		}

		matches := episodeRegex.FindStringSubmatch(entry.Name())
		if len(matches) < 2 {
			continue
		}

		episodeNum, _ := strconv.Atoi(matches[1])
		episodePath := filepath.Join(seasonPath, entry.Name())

		upsertEpisode(seasonID, episodeNum, entry.Name(), episodePath)
	}
}

func upsertEpisode(seasonID int, episodeNum int, title string, path string) {
	query := `
		INSERT INTO episodes (season_id, episode_number, title, file_path, updated_at)
		VALUES ($1, $2, $3, $4, CURRENT_TIMESTAMP)
		ON CONFLICT (file_path) DO UPDATE SET
			episode_number = EXCLUDED.episode_number,
			title = EXCLUDED.title,
			updated_at = CURRENT_TIMESTAMP
	`
	database.DB.Exec(query, seasonID, episodeNum, title, path)
}
