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
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	`

	_, err := DB.Exec(migrationSQL)
	if err != nil {
		return fmt.Errorf("failed to run users migration: %w", err)
	}

	moviesTableSQL := `
	CREATE TABLE IF NOT EXISTS movies (
		id SERIAL PRIMARY KEY,
		title VARCHAR(255) NOT NULL,
		year INTEGER,
		tmdb_id VARCHAR(50),
		path TEXT UNIQUE NOT NULL,
		overview TEXT,
		poster_path VARCHAR(255),
		status VARCHAR(50) DEFAULT 'discovered',
		raw_metadata JSONB,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
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
		status VARCHAR(50) DEFAULT 'discovered',
		raw_metadata JSONB,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

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
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(season_id, episode_number)
	);
	`
	_, err = DB.Exec(showsTableSQL)
	if err != nil {
		return fmt.Errorf("failed to run shows migrations: %w", err)
	}

	return nil
}

