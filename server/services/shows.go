package services

import (
	"Arrgo/config"
	"Arrgo/database"
	"Arrgo/models"
	"database/sql"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

func ScanShows(cfg *config.Config, onlyIncoming bool) error {
	log.Printf("[SCANNER] Starting TV show scan with 4 workers...")

	type showTask struct {
		root string
		name string
	}

	taskChan := make(chan showTask, 100)
	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range taskChan {
				processShowDir(cfg, task.root, task.name)
			}
		}()
	}

	// Scan paths based on preference
	var paths []string
	if onlyIncoming {
		paths = []string{cfg.IncomingPath}
	} else {
		paths = []string{cfg.TVShowsPath}
	}

	for _, p := range paths {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); os.IsNotExist(err) {
			log.Printf("[SCANNER] Path does not exist, skipping: %s", p)
			continue
		}

		entries, err := os.ReadDir(p)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if entry.IsDir() {
				taskChan <- showTask{root: p, name: entry.Name()}
			}
		}
	}

	close(taskChan)
	wg.Wait()

	log.Printf("[SCANNER] TV show scan complete.")

	return nil
}

func processShowDir(cfg *config.Config, root string, name string) {
	showPath := filepath.Join(root, name)
	title, year := parseMovieName(name) // Reuse parseMovieName for "Title (Year)"

	// Look for local poster
	posterPath := ""
	posterExtensions := []string{".jpg", ".jpeg", ".png", ".webp"}
	posterNames := []string{"poster", "folder", "cover", "show"}

	for _, n := range posterNames {
		for _, ext := range posterExtensions {
			p := filepath.Join(showPath, n+ext)
			if _, err := os.Stat(p); err == nil {
				posterPath = p
				break
			}
		}
		if posterPath != "" {
			break
		}
	}

	log.Printf("[SCANNER] Processing show: %s (%d) at %s", title, year, showPath)
	showID, err := upsertShow(models.Show{
		Title:      title,
		Year:       year,
		Path:       showPath,
		PosterPath: posterPath,
		Status:     "discovered",
	})
	if err != nil {
		log.Printf("[SCANNER] Error upserting show %s: %v", title, err)
		return
	}

	// Fetch metadata immediately
	MatchShow(cfg, showID)

	scanSeasons(showID, showPath)
}

func upsertShow(show models.Show) (int, error) {
	var id int
	query := `
		INSERT INTO shows (title, year, path, poster_path, status, updated_at)
		VALUES ($1, $2, $3, $4, $5, CURRENT_TIMESTAMP)
		ON CONFLICT (path) DO UPDATE SET
			title = EXCLUDED.title,
			year = EXCLUDED.year,
			poster_path = COALESCE(NULLIF(EXCLUDED.poster_path, ''), shows.poster_path),
			updated_at = CURRENT_TIMESTAMP
		RETURNING id
	`
	err := database.DB.QueryRow(query, show.Title, show.Year, show.Path, show.PosterPath, show.Status).Scan(&id)
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

		info, _ := entry.Info()
		size := info.Size()
		quality := DetectQuality(episodePath)

		upsertEpisode(seasonID, episodeNum, entry.Name(), episodePath, quality, size)
	}
}

func upsertEpisode(seasonID int, episodeNum int, title string, path string, quality string, size int64) {
	query := `
		INSERT INTO episodes (season_id, episode_number, title, file_path, quality, size, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, CURRENT_TIMESTAMP)
		ON CONFLICT (file_path) DO UPDATE SET
			episode_number = EXCLUDED.episode_number,
			title = EXCLUDED.title,
			quality = EXCLUDED.quality,
			size = EXCLUDED.size,
			updated_at = CURRENT_TIMESTAMP
	`
	database.DB.Exec(query, seasonID, episodeNum, title, path, quality, size)
}

func GetShowCount(excludeIncomingPath string) (int, error) {
	var count int
	var err error
	if excludeIncomingPath != "" {
		err = database.DB.QueryRow("SELECT COUNT(*) FROM shows WHERE path NOT LIKE $1 || '%'", excludeIncomingPath).Scan(&count)
	} else {
		err = database.DB.QueryRow("SELECT COUNT(*) FROM shows").Scan(&count)
	}
	return count, err
}

func SearchShowsLocal(query string) ([]models.Show, error) {
	dbQuery := `
		SELECT id, title, year, tvdb_id, path, overview, poster_path, genres, status, created_at, updated_at 
		FROM shows 
		WHERE title ILIKE $1 OR overview ILIKE $1 OR genres ILIKE $1
		ORDER BY title ASC
	`
	rows, err := database.DB.Query(dbQuery, "%"+query+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var shows []models.Show
	for rows.Next() {
		var s models.Show
		var tvdbID, overview, posterPath, genres sql.NullString
		err := rows.Scan(&s.ID, &s.Title, &s.Year, &tvdbID, &s.Path, &overview, &posterPath, &genres, &s.Status, &s.CreatedAt, &s.UpdatedAt)
		if err != nil {
			return nil, err
		}
		s.TVDBID = tvdbID.String
		s.Overview = overview.String
		s.PosterPath = posterPath.String
		s.Genres = genres.String
		shows = append(shows, s)
	}
	return shows, nil
}

func GetShows() ([]models.Show, error) {
	query := `SELECT id, title, year, tvdb_id, path, overview, poster_path, genres, status, created_at, updated_at FROM shows ORDER BY title ASC`
	rows, err := database.DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	shows := []models.Show{}
	for rows.Next() {
		var s models.Show
		var tvdbID, overview, posterPath, genres sql.NullString
		err := rows.Scan(&s.ID, &s.Title, &s.Year, &tvdbID, &s.Path, &overview, &posterPath, &genres, &s.Status, &s.CreatedAt, &s.UpdatedAt)
		if err != nil {
			return nil, err
		}
		s.TVDBID = tvdbID.String
		s.Overview = overview.String
		s.PosterPath = posterPath.String
		s.Genres = genres.String
		shows = append(shows, s)
	}
	return shows, nil
}

func GetShowByID(id int) (*models.Show, error) {
	query := `SELECT id, title, year, tvdb_id, path, overview, poster_path, genres, status, created_at, updated_at FROM shows WHERE id = $1`
	var s models.Show
	var tvdbID, overview, posterPath, genres sql.NullString
	err := database.DB.QueryRow(query, id).Scan(&s.ID, &s.Title, &s.Year, &tvdbID, &s.Path, &overview, &posterPath, &genres, &s.Status, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	s.TVDBID = tvdbID.String
	s.Overview = overview.String
	s.PosterPath = posterPath.String
	s.Genres = genres.String
	return &s, nil
}

type SeasonWithEpisodes struct {
	models.Season
	Episodes []models.Episode
}

func GetShowSeasons(showID int) ([]SeasonWithEpisodes, error) {
	query := `SELECT id, show_id, season_number, overview FROM seasons WHERE show_id = $1 ORDER BY season_number ASC`
	rows, err := database.DB.Query(query, showID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var seasons []SeasonWithEpisodes
	for rows.Next() {
		var s SeasonWithEpisodes
		var overview sql.NullString
		err := rows.Scan(&s.ID, &s.ShowID, &s.SeasonNumber, &overview)
		if err != nil {
			return nil, err
		}
		s.Overview = overview.String

		// Fetch episodes for this season
		episodes, err := GetSeasonEpisodes(s.ID)
		if err != nil {
			log.Printf("Error getting episodes for season %d: %v", s.ID, err)
		} else {
			s.Episodes = episodes
		}

		seasons = append(seasons, s)
	}
	return seasons, nil
}

func GetSeasonEpisodes(seasonID int) ([]models.Episode, error) {
	query := `SELECT id, season_id, episode_number, title, file_path, quality, size FROM episodes WHERE season_id = $1 ORDER BY episode_number ASC`
	rows, err := database.DB.Query(query, seasonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var episodes []models.Episode
	for rows.Next() {
		var e models.Episode
		var title, quality sql.NullString
		err := rows.Scan(&e.ID, &e.SeasonID, &e.EpisodeNumber, &title, &e.FilePath, &quality, &e.Size)
		if err != nil {
			return nil, err
		}
		e.Title = title.String
		e.Quality = quality.String
		episodes = append(episodes, e)
	}
	return episodes, nil
}
