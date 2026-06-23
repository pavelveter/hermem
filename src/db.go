package main

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"net/url"
	"strings"
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
	Status         string    `json:"status,omitempty"`
}

type Edge struct {
	SourceID     string `json:"source_id"`
	TargetID     string `json:"target_id"`
	RelationType string `json:"relation_type"`
}

func InitDB(dbPath string, vectorDim int) (*sql.DB, error) {
	v := url.Values{}
	v.Set("_journal_mode", "WAL")
	v.Set("_busy_timeout", "5000")
	v.Set("_sync", "NORMAL")

	var dsn string
	if dbPath == ":memory:" {
		dsn = ":memory:?" + v.Encode()
	} else {
		dsn = "file:" + dbPath + "?" + v.Encode()
	}

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	db.SetMaxOpenConns(4)

	// PRAGMAs as explicit confirmation; DSN params apply first at connect.
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
			category TEXT NOT NULL CHECK(category IN ('world', 'experience', 'opinion', 'observation', 'task')),
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
		{"status", `ALTER TABLE entities ADD COLUMN status TEXT DEFAULT 'pending'`},
	}
	for _, m := range migrations {
		if _, err := db.Exec(m.sql); err != nil {
			// Column already exists — ignore
		}
	}
	if err := migrateCategoryCheck(db); err != nil {
		return err
	}
	return nil
}

// migrateCategoryCheck recreates the entities table if the category
// CHECK constraint doesn't include 'task'. SQLite doesn't support
// ALTER TABLE to modify CHECK constraints, so we recreate the table
// with the updated schema and copy all data in a transaction.
func migrateCategoryCheck(db *sql.DB) error {
	var createSQL string
	err := db.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='entities'").Scan(&createSQL)
	if err != nil {
		return nil
	}
	if strings.Contains(createSQL, "'task'") {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		CREATE TABLE entities_new (
			id TEXT PRIMARY KEY,
			category TEXT NOT NULL CHECK(category IN ('world', 'experience', 'opinion', 'observation', 'task')),
			content TEXT NOT NULL,
			embedding BLOB,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_accessed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			archived INTEGER DEFAULT 0,
			status TEXT DEFAULT 'pending'
		)
	`); err != nil {
		return fmt.Errorf("create entities_new: %w", err)
	}

	if _, err := tx.Exec(`
		INSERT INTO entities_new (id, category, content, embedding, updated_at, last_accessed_at, archived, status)
		SELECT id, category, content, embedding, updated_at, last_accessed_at, archived, status FROM entities
	`); err != nil {
		return fmt.Errorf("copy entities: %w", err)
	}

	if _, err := tx.Exec(`DROP TABLE entities`); err != nil {
		return fmt.Errorf("drop old entities: %w", err)
	}

	if _, err := tx.Exec(`ALTER TABLE entities_new RENAME TO entities`); err != nil {
		return fmt.Errorf("rename entities_new: %w", err)
	}

	return tx.Commit()
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

// DecodeVector decodes a BLOB into a float32 slice, validating that the
// blob size matches the expected embedding dimension. Returns error on
// empty blob or dimension mismatch (silent data corruption guard).
func DecodeVector(data []byte, expectedDim int) ([]float32, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty vector blob")
	}
	if len(data) != expectedDim*4 {
		return nil, fmt.Errorf("vector dimension drift: blob %d bytes, want %d (dim=%d)",
			len(data), expectedDim*4, expectedDim)
	}
	emb := make([]float32, expectedDim)
	for i := range emb {
		bits := binary.LittleEndian.Uint32(data[i*4 : i*4+4])
		emb[i] = math.Float32frombits(bits)
	}
	return emb, nil
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

// UpdateTaskStatus sets the status of a task entity. Only updates rows
// where category = 'task' to avoid touching memory/opinion nodes.
// Returns an error if the entity is not found or is not a task.
func UpdateTaskStatus(db *sql.DB, id, status string) error {
	validStatuses := map[string]bool{
		"pending":   true,
		"running":   true,
		"completed": true,
		"failed":    true,
	}
	if !validStatuses[status] {
		return fmt.Errorf("invalid status: %s (must be pending, running, completed, or failed)", status)
	}
	res, err := db.Exec(
		`UPDATE entities SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND category = 'task'`,
		status, id,
	)
	if err != nil {
		return fmt.Errorf("update task status: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("task not found or not a task entity: %s", id)
	}
	return nil
}

// GetTaskStatus returns the current status of a task entity, or empty
// string if the entity doesn't exist or isn't a task.
func GetTaskStatus(db *sql.DB, id string) (string, error) {
	var status string
	err := db.QueryRow(
		`SELECT status FROM entities WHERE id = ? AND category = 'task'`,
		id,
	).Scan(&status)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get task status: %w", err)
	}
	return status, nil
}
