package store

import (
	"database/sql"
	"fmt"
	"net/url"
	"sort"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

// InitDB opens (or creates) the SQLite database with hardened pragma settings.
func InitDB(dbPath string, vectorDim int) (*sql.DB, error) {
	v := url.Values{}
	v.Set("_journal_mode", "WAL")
	v.Set("_busy_timeout", "5000")
	v.Set("_sync", "NORMAL")
	v.Set("_fk", "true")

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

	if _, err := db.Exec("PRAGMA journal_mode = WAL;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("WAL: %w", err)
	}
	if _, err := db.Exec("PRAGMA synchronous = NORMAL;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("sync: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout = 5000;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("busy_timeout: %w", err)
	}
	if _, err := db.Exec("PRAGMA auto_vacuum = INCREMENTAL;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("auto_vacuum: %w", err)
	}

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at DATETIME DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema_migrations: %w", err)
	}

	if err := ensureMigrationChecksumsTable(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("checksums: %w", err)
	}
	if err := RunMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrations: %w", err)
	}
	if err := migrateEntitiesFlexibleSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("flexible schema: %w", err)
	}
	if err := CheckMeta(db, vectorDim); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema validation: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys"); err != nil {
		db.Close()
		return nil, fmt.Errorf("fk pragma: %w", err)
	}

	return db, nil
}

// CheckMeta verifies that the stored embedding_dim matches the configured one.
func CheckMeta(db *sql.DB, dim int) error {
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
		return fmt.Errorf("embedding_dim mismatch: database has %d, config specifies %d", existingDim, dim)
	}
	return nil
}

func ensureMigrationChecksumsTable(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS migration_checksums (version TEXT PRIMARY KEY, checksum TEXT NOT NULL)`)
	return err
}

func migrateEntitiesFlexibleSchema(db *sql.DB) error {
	var createSQL string
	err := db.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='entities'").Scan(&createSQL)
	if err != nil {
		return nil
	}
	if !strings.Contains(strings.ToUpper(createSQL), "CHECK(CATEGORY IN") && strings.Contains(strings.ToUpper(createSQL), "ARCHIVED INTEGER DEFAULT 0") {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`CREATE TABLE entities_new (id TEXT PRIMARY KEY, category TEXT NOT NULL, content TEXT NOT NULL, embedding BLOB, updated_at DATETIME DEFAULT CURRENT_TIMESTAMP, last_accessed_at DATETIME DEFAULT CURRENT_TIMESTAMP, archived INTEGER DEFAULT 0, status TEXT DEFAULT NULL)`); err != nil {
		return fmt.Errorf("entities_new: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO entities_new SELECT id, category, content, embedding, updated_at, last_accessed_at, archived, status FROM entities`); err != nil {
		return fmt.Errorf("copy: %w", err)
	}
	if _, err := tx.Exec("PRAGMA defer_foreign_keys = ON"); err != nil {
		return fmt.Errorf("defer_fk: %w", err)
	}
	if _, err := tx.Exec(`DROP TABLE entities`); err != nil {
		return fmt.Errorf("drop: %w", err)
	}
	if _, err := tx.Exec(`ALTER TABLE entities_new RENAME TO entities`); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return tx.Commit()
}

// SortedKeys returns sorted keys of a bool map.
func SortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// MemDB opens an in-memory database for testing.
func MemDB() (*sql.DB, error) { return InitDB(":memory:", 3) }
