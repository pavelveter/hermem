package store

import (
	"database/sql"
	"fmt"
	"net/url"
	"sort"

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
	// SQLite is single-writer-with-WAL; opening more conns would just
	// race on _busy_timeout / SQLITE_BUSY. One writer is the proven
	// production pattern.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

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

// migrateEntitiesFlexibleSchema is a permanent no-op as of 2026-06-24.
//
// Originally recreated entities to add columns ALTER ADD COLUMN couldn't
// reach on older SQLite. After 002 (entity_metadata) and 005/007 (degree
// / priority) shipped, the rebuild is actively harmful: the hard-coded
// CREATE TABLE entities_new list omits degree / priority /
// conversation_id / message_id / extracted_from / created_at / status /
// valid_from / valid_to — DROP+RENAME would silently drop those columns.
// The mid-tx schema flip also surfaces "no such table: main.entities"
// to users via triggers fired against the unrenamed tablename.
//
// The b2 per-statement runner + 002 together cover the legacy path now
// (002 adds all 002 columns; per-statement duplicate-column skip
// tolerates DBs where some are already present). Kept as a no-op so
// callers can stay; remove entirely if no callers remain.
func migrateEntitiesFlexibleSchema(db *sql.DB) error {
	return nil
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
