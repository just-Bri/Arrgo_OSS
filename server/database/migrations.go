package database

import (
	"fmt"
)

func RunMigrations() error {
	migrationSQL := `
	CREATE TABLE IF NOT EXISTS users (
		id SERIAL PRIMARY KEY,
		username VARCHAR(255) UNIQUE NOT NULL,
		email VARCHAR(255) UNIQUE NOT NULL,
		password_hash VARCHAR(255) NOT NULL,
		is_admin BOOLEAN DEFAULT FALSE,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	`

	_, err := DB.Exec(migrationSQL)
	if err != nil {
		return fmt.Errorf("failed to run users migration: %w", err)
	}

	// Migration for existing users table
	DO_USERS := `
	DO $$ 
	BEGIN 
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='users' AND column_name='is_admin') THEN
			ALTER TABLE users ADD COLUMN is_admin BOOLEAN DEFAULT FALSE;
		END IF;
	END $$;
	`
	_, err = DB.Exec(DO_USERS)
	if err != nil {
		return fmt.Errorf("failed to run users column migration: %w", err)
	}

	// Ensure user named 'admin' is actually an admin
	_, err = DB.Exec("UPDATE users SET is_admin = TRUE WHERE username = 'admin'")
	if err != nil {
		return fmt.Errorf("failed to ensure admin user has admin flag: %w", err)
	}

	moviesTableSQL := `
	CREATE TABLE IF NOT EXISTS movies (
		id SERIAL PRIMARY KEY,
		title VARCHAR(255) NOT NULL,
		year INTEGER,
		tmdb_id VARCHAR(50),
		path TEXT UNIQUE NOT NULL,
		quality VARCHAR(50),
		size BIGINT DEFAULT 0,
		overview TEXT,
		poster_path VARCHAR(255),
		genres TEXT,
		imdb_id VARCHAR(50),
		status VARCHAR(50) DEFAULT 'discovered',
		raw_metadata JSONB,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- Migration for existing movies table
	DO $$ 
	BEGIN 
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='movies' AND column_name='quality') THEN
			ALTER TABLE movies ADD COLUMN quality VARCHAR(50);
		END IF;
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='movies' AND column_name='size') THEN
			ALTER TABLE movies ADD COLUMN size BIGINT DEFAULT 0;
		END IF;
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='movies' AND column_name='genres') THEN
			ALTER TABLE movies ADD COLUMN genres TEXT;
		END IF;
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='movies' AND column_name='imdb_id') THEN
			ALTER TABLE movies ADD COLUMN imdb_id VARCHAR(50);
		END IF;
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='movies' AND column_name='torrent_hash') THEN
			ALTER TABLE movies ADD COLUMN torrent_hash VARCHAR(255);
		END IF;
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='movies' AND column_name='imported_at') THEN
			ALTER TABLE movies ADD COLUMN imported_at TIMESTAMP;
		END IF;
	END $$;
	`
	_, err = DB.Exec(moviesTableSQL)
	if err != nil {
		return fmt.Errorf("failed to run movies migration: %w", err)
	}

	showsTableSQL := `
	CREATE TABLE IF NOT EXISTS shows (
		id SERIAL PRIMARY KEY,
		title VARCHAR(255) NOT NULL,
		year INTEGER,
		tvdb_id VARCHAR(50),
		path TEXT UNIQUE NOT NULL,
		overview TEXT,
		poster_path VARCHAR(255),
		genres TEXT,
		imdb_id VARCHAR(50),
		status VARCHAR(50) DEFAULT 'discovered',
		raw_metadata JSONB,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- Migration for existing shows table
	DO $$ 
	BEGIN 
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='shows' AND column_name='tmdb_id') THEN
			ALTER TABLE shows ADD COLUMN tmdb_id VARCHAR(50);
		END IF;
	END $$;

	CREATE TABLE IF NOT EXISTS seasons (
		id SERIAL PRIMARY KEY,
		show_id INTEGER REFERENCES shows(id) ON DELETE CASCADE,
		season_number INTEGER NOT NULL,
		overview TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(show_id, season_number)
	);

	CREATE TABLE IF NOT EXISTS episodes (
		id SERIAL PRIMARY KEY,
		season_id INTEGER REFERENCES seasons(id) ON DELETE CASCADE,
		episode_number INTEGER NOT NULL,
		title VARCHAR(255),
		file_path TEXT UNIQUE NOT NULL,
		quality VARCHAR(50),
		size BIGINT DEFAULT 0,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(season_id, episode_number)
	);

	-- Migration for existing episodes table
	DO $$ 
	BEGIN 
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='episodes' AND column_name='quality') THEN
			ALTER TABLE episodes ADD COLUMN quality VARCHAR(50);
		END IF;
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='episodes' AND column_name='size') THEN
			ALTER TABLE episodes ADD COLUMN size BIGINT DEFAULT 0;
		END IF;
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='shows' AND column_name='genres') THEN
			ALTER TABLE shows ADD COLUMN genres TEXT;
		END IF;
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='shows' AND column_name='imdb_id') THEN
			ALTER TABLE shows ADD COLUMN imdb_id VARCHAR(50);
		END IF;
	END $$;

	-- Migration for existing episodes table - add torrent_hash and imported_at
	DO $$ 
	BEGIN 
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='episodes' AND column_name='torrent_hash') THEN
			ALTER TABLE episodes ADD COLUMN torrent_hash VARCHAR(255);
		END IF;
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='episodes' AND column_name='imported_at') THEN
			ALTER TABLE episodes ADD COLUMN imported_at TIMESTAMP;
		END IF;
	END $$;
	`
	_, err = DB.Exec(showsTableSQL)
	if err != nil {
		return fmt.Errorf("failed to run shows migrations: %w", err)
	}

	requestsTableSQL := `
	CREATE TABLE IF NOT EXISTS requests (
		id SERIAL PRIMARY KEY,
		user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
		title VARCHAR(255) NOT NULL,
		media_type VARCHAR(20) NOT NULL, -- 'movie' or 'show'
		tmdb_id VARCHAR(50),
		tvdb_id VARCHAR(50),
		imdb_id VARCHAR(50),
		year INTEGER,
		poster_path VARCHAR(255),
		overview TEXT,
		seasons TEXT, -- Comma-separated list of season numbers for shows
		status VARCHAR(50) DEFAULT 'pending',
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- Migration for existing requests table
	DO $$ 
	BEGIN 
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='requests' AND column_name='seasons') THEN
			ALTER TABLE requests ADD COLUMN seasons TEXT;
		END IF;
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='requests' AND column_name='imdb_id') THEN
			ALTER TABLE requests ADD COLUMN imdb_id VARCHAR(50);
		END IF;
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='requests' AND column_name='retry_count') THEN
			ALTER TABLE requests ADD COLUMN retry_count INTEGER DEFAULT 0;
		END IF;
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='requests' AND column_name='last_search_at') THEN
			ALTER TABLE requests ADD COLUMN last_search_at TIMESTAMP;
		END IF;
	END $$;

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

	CREATE TABLE IF NOT EXISTS settings (
		key VARCHAR(255) PRIMARY KEY,
		value TEXT,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS subtitle_queue (
		id SERIAL PRIMARY KEY,
		media_type VARCHAR(20) NOT NULL, -- 'movie' or 'episode'
		media_id INTEGER NOT NULL,
		retry_count INTEGER DEFAULT 0,
		next_retry TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(media_type, media_id)
	);

	CREATE TABLE IF NOT EXISTS indexers (
		id SERIAL PRIMARY KEY,
		name VARCHAR(255) NOT NULL,
		type VARCHAR(50) NOT NULL, -- 'builtin'
		enabled BOOLEAN DEFAULT TRUE,
		url VARCHAR(500), -- Reserved for future use
		api_key VARCHAR(255), -- Reserved for future use
		priority INTEGER DEFAULT 0, -- Lower number = higher priority
		config JSONB, -- Additional configuration (stores scraper_type)
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
	_, err = DB.Exec(requestsTableSQL)
	if err != nil {
		return fmt.Errorf("failed to run requests migration: %w", err)
	}

	return nil
}
