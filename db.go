package main

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Entity struct {
	ID        string    `json:"id"`
	Category  string    `json:"category"`
	Content   string    `json:"content"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Edge struct {
	SourceID     string `json:"source_id"`
	TargetID     string `json:"target_id"`
	RelationType string `json:"relation_type"`
}

func InitDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable WAL mode for better concurrent performance
	if _, err := db.Exec("PRAGMA journal_mode = WAL;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to set WAL mode: %w", err)
	}

	// Set synchronous to NORMAL for better performance
	if _, err := db.Exec("PRAGMA synchronous = NORMAL;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to set synchronous mode: %w", err)
	}

	// Create entities table
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS entities (
			id TEXT PRIMARY KEY,
			category TEXT NOT NULL CHECK(category IN ('world', 'experience', 'opinion', 'observation')),
			content TEXT NOT NULL,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create entities table: %w", err)
	}

	// Create edges table with composite primary key for deduplication
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS edges (
			source_id TEXT NOT NULL,
			target_id TEXT NOT NULL,
			relation_type TEXT NOT NULL,
			PRIMARY KEY (source_id, target_id, relation_type),
			FOREIGN KEY (source_id) REFERENCES entities(id) ON DELETE CASCADE,
			FOREIGN KEY (target_id) REFERENCES entities(id) ON DELETE CASCADE
		)
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create edges table: %w", err)
	}

	return db, nil
}
