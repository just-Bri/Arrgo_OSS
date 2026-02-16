package database

import (
	"fmt"
)

// InitSchema creates all database tables with their final schema structure.
// This replaces the previous migration system now that the schema is stable.
func InitSchema() error {
	schemaSQL := `
	-- Users table
	CREATE TABLE IF NOT EXISTS users (
		id SERIAL PRIMARY KEY,
		username VARCHAR(255) UNIQUE NOT NULL,
		email VARCHAR(255) UNIQUE NOT NULL,
		password_hash VARCHAR(255) NOT NULL,
		is_admin BOOLEAN DEFAULT FALSE,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- Movies table
	CREATE TABLE IF NOT EXISTS movies (
		id SERIAL PRIMARY KEY,
		title VARCHAR(255) NOT NULL,
		year INTEGER,
		tmdb_id VARCHAR(50),
		imdb_id VARCHAR(50),
		path TEXT UNIQUE NOT NULL,
		quality VARCHAR(50),
		size BIGINT DEFAULT 0,
		overview TEXT,
		poster_path VARCHAR(255),
		genres TEXT,
		status VARCHAR(50) DEFAULT 'discovered',
		raw_metadata JSONB,
		torrent_hash VARCHAR(255),
		imported_at TIMESTAMP,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- Shows table
	CREATE TABLE IF NOT EXISTS shows (
		id SERIAL PRIMARY KEY,
		title VARCHAR(255) NOT NULL,
		year INTEGER,
		tvdb_id VARCHAR(50),
		tmdb_id VARCHAR(50),
		imdb_id VARCHAR(50),
		path TEXT UNIQUE NOT NULL,
		overview TEXT,
		poster_path VARCHAR(255),
		genres TEXT,
		status VARCHAR(50) DEFAULT 'discovered',
		raw_metadata JSONB,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- Seasons table
	CREATE TABLE IF NOT EXISTS seasons (
		id SERIAL PRIMARY KEY,
		show_id INTEGER REFERENCES shows(id) ON DELETE CASCADE,
		season_number INTEGER NOT NULL,
		overview TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(show_id, season_number)
	);

	-- Episodes table
	CREATE TABLE IF NOT EXISTS episodes (
		id SERIAL PRIMARY KEY,
		season_id INTEGER REFERENCES seasons(id) ON DELETE CASCADE,
		episode_number INTEGER NOT NULL,
		title VARCHAR(255),
		file_path TEXT UNIQUE NOT NULL,
		quality VARCHAR(50),
		size BIGINT DEFAULT 0,
		torrent_hash VARCHAR(255),
		imported_at TIMESTAMP,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(file_path)
	);

	-- Requests table
	CREATE TABLE IF NOT EXISTS requests (
		id SERIAL PRIMARY KEY,
		user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
		title VARCHAR(255) NOT NULL,
		original_title VARCHAR(255),
		media_type VARCHAR(20) NOT NULL,
		tmdb_id VARCHAR(50),
		tvdb_id VARCHAR(50),
		imdb_id VARCHAR(50),
		year INTEGER,
		poster_path VARCHAR(255),
		overview TEXT,
		seasons TEXT,
		status VARCHAR(50) DEFAULT 'pending',
		retry_count INTEGER DEFAULT 0,
		last_search_at TIMESTAMP,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- Downloads table
	CREATE TABLE IF NOT EXISTS downloads (
		id SERIAL PRIMARY KEY,
		request_id INTEGER REFERENCES requests(id) ON DELETE CASCADE,
		torrent_hash VARCHAR(255) UNIQUE NOT NULL,
		title VARCHAR(255) NOT NULL,
		size BIGINT DEFAULT 0,
		status VARCHAR(50) DEFAULT 'downloading',
		progress FLOAT DEFAULT 0,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- Settings table
	CREATE TABLE IF NOT EXISTS settings (
		key VARCHAR(255) PRIMARY KEY,
		value TEXT,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- Subtitle queue table
	CREATE TABLE IF NOT EXISTS subtitle_queue (
		id SERIAL PRIMARY KEY,
		media_type VARCHAR(20) NOT NULL,
		media_id INTEGER NOT NULL,
		retry_count INTEGER DEFAULT 0,
		next_retry TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(media_type, media_id)
	);

	-- Indexers table
	CREATE TABLE IF NOT EXISTS indexers (
		id SERIAL PRIMARY KEY,
		name VARCHAR(255) NOT NULL,
		type VARCHAR(50) NOT NULL,
		enabled BOOLEAN DEFAULT TRUE,
		url VARCHAR(500),
		api_key VARCHAR(255),
		priority INTEGER DEFAULT 0,
		config JSONB,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(name, type)
	);

	-- Insert default built-in indexers
	INSERT INTO indexers (name, type, enabled, priority) VALUES
		('YTS', 'builtin', TRUE, 1),
		('Nyaa', 'builtin', TRUE, 2),
		('1337x', 'builtin', TRUE, 3),
		('TorrentGalaxy', 'builtin', TRUE, 4),
		('SolidTorrents', 'builtin', TRUE, 5)
	ON CONFLICT (name, type) DO NOTHING;
	`

	if _, err := DB.Exec(schemaSQL); err != nil {
		return fmt.Errorf("failed to initialize database schema: %w", err)
	}

	// Drop the old constraint if it exists to allow for duplicate episode numbers (mislabeled files)
	// This ensures existing installations are updated automatically.
	dropConstraintSQL := "ALTER TABLE episodes DROP CONSTRAINT IF EXISTS episodes_season_id_episode_number_key;"
	if _, err := DB.Exec(dropConstraintSQL); err != nil {
		fmt.Printf("Warning: failed to drop episodes constraint: %v\n", err)
	}

	// Add original_title column to requests table if it doesn't exist
	// This is needed for dual-title torrent searching (English + localized titles)
	addOriginalTitleSQL := "ALTER TABLE requests ADD COLUMN IF NOT EXISTS original_title VARCHAR(255);"
	if _, err := DB.Exec(addOriginalTitleSQL); err != nil {
		fmt.Printf("Warning: failed to add original_title column: %v\n", err)
	}

	return nil
}
