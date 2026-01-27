package models

import "time"

type Indexer struct {
	ID       int       `json:"id" db:"id"`
	Name     string    `json:"name" db:"name"`
	Type     string    `json:"type" db:"type"` // "builtin"
	Enabled  bool      `json:"enabled" db:"enabled"`
	URL      string    `json:"url,omitempty" db:"url"`
	APIKey   string    `json:"api_key,omitempty" db:"api_key"`
	Priority int       `json:"priority" db:"priority"`
	Config   string    `json:"config,omitempty" db:"config"` // JSON string
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}
