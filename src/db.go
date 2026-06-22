package main

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Entity struct {
	ID             string    `json:"id"`
	Category       string    `json:"category"`
	Content        string    `json:"content"`
	Embedding      []float32 `json:"embedding,omitempty"`
	UpdatedAt      time.Time `json:"updated_at"`
	LastAccessedAt time.Time `json:"last_accessed_at"`
	Archived       bool      `json:"archived"`
}

type Edge struct {
	SourceID     string `json:"source_id"`
	TargetID     string `json:"target_id"`
	RelationType string `json:"relation_type"`
}

func InitDB(dbPath string, vectorDim int) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	db.SetMaxOpenConns(4)

	if _, err := db.Exec("PRAGMA journal_mode = WAL;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to set WAL mode: %w", err)
	}

	if _, err := db.Exec("PRAGMA synchronous = NORMAL;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to set synchronous mode: %w", err)
	}

	if _, err := db.Exec("PRAGMA busy_timeout = 5000;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to set busy_timeout: %w", err)
	}

	if _, err := db.Exec("PRAGMA auto_vacuum = INCREMENTAL;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to set auto_vacuum mode: %w", err)
	}

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS entities (
			id TEXT PRIMARY KEY,
			category TEXT NOT NULL CHECK(category IN ('world', 'experience', 'opinion', 'observation')),
			content TEXT NOT NULL,
			embedding BLOB,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create entities table: %w", err)
	}

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

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create meta table: %w", err)
	}

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS id_map (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			entity_id TEXT UNIQUE NOT NULL
		)
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create id_map table: %w", err)
	}

	if err := migrateSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema migration: %w", err)
	}

	if err := checkMeta(db, vectorDim); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema validation: %w", err)
	}

	return db, nil
}

func migrateSchema(db *sql.DB) error {
	migrations := []struct {
		name string
		sql  string
	}{
		{"last_accessed_at", `ALTER TABLE entities ADD COLUMN last_accessed_at DATETIME DEFAULT CURRENT_TIMESTAMP`},
		{"archived", `ALTER TABLE entities ADD COLUMN archived INTEGER DEFAULT 0`},
	}
	for _, m := range migrations {
		if _, err := db.Exec(m.sql); err != nil {
			// Column already exists — ignore
		}
	}
	return nil
}

func ensureEntityID(db *sql.DB, entityID string) (int64, error) {
	var rowID int64
	err := db.QueryRow("SELECT id FROM id_map WHERE entity_id = ?", entityID).Scan(&rowID)
	if err == nil {
		return rowID, nil
	}
	if err != sql.ErrNoRows {
		return 0, fmt.Errorf("query id_map: %w", err)
	}
	res, err := db.Exec("INSERT INTO id_map (entity_id) VALUES (?)", entityID)
	if err != nil {
		return 0, fmt.Errorf("insert id_map: %w", err)
	}
	return res.LastInsertId()
}

func checkMeta(db *sql.DB, dim int) error {
	var existingDim int
	err := db.QueryRow("SELECT value FROM meta WHERE key = 'embedding_dim'").Scan(&existingDim)
	if err == sql.ErrNoRows {
		_, err = db.Exec("INSERT OR IGNORE INTO meta (key, value) VALUES ('embedding_dim', ?), ('model_name', '')", fmt.Sprintf("%d", dim))
		return err
	}
	if err != nil {
		return err
	}
	if existingDim != dim && existingDim != 0 {
		return fmt.Errorf("embedding_dim mismatch: database has %d, config specifies %d — re-embedding required", existingDim, dim)
	}
	return nil
}


