package database

import (
	"database/sql"
	"fmt"
	"time"

	"Arrgo/config"

	_ "github.com/jackc/pgx/v5/stdlib"
)

var DB *sql.DB

func Connect(cfg *config.Config) error {
	var err error
	DB, err = sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	if err = DB.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	// Configure connection pool limits to prevent "too many clients" errors from PostgreSQL
	DB.SetMaxOpenConns(25) // 25 max open connections
	DB.SetMaxIdleConns(5)  // Keep 5 idle connections
	DB.SetConnMaxLifetime(5 * time.Minute)

	return nil
}

func Close() error {
	if DB != nil {
		return DB.Close()
	}
	return nil
}
