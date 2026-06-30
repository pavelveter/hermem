package store

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

// ErrMigrationsPending is returned by InitDBStrict when autoMigrate=false
// and the DB has pending migrations.
var ErrMigrationsPending = errors.New("schema has pending migrations; run `./hermem db migrate apply` or set `[database] auto_migrate = true` in hermem.ini")

// ErrSchemaIntegrityBroken is returned by InitDBStrict when the stored
// migration checksums diverge from the on-disk migration files.
var ErrSchemaIntegrityBroken = errors.New("schema migration checksums diverge from on-disk files; verify migration files integrity")

// InitDB opens (or creates) the SQLite database with hardened pragma
// settings AND auto-applies pending migrations.
func InitDB(dbPath string, vectorDim int) (*sql.DB, error) {
	return InitDBStrict(dbPath, vectorDim, true)
}

// InitDBStrict opens the SQLite database and respects the autoMigrate gate.
func InitDBStrict(dbPath string, vectorDim int, autoMigrate bool) (*sql.DB, error) {
	return InitDBStrictWithOptions(dbPath, vectorDim, autoMigrate, false)
}

// InitDBStrictWithOptions is like InitDBStrict but also accepts skipSchemaCheck.
func InitDBStrictWithOptions(dbPath string, vectorDim int, autoMigrate bool, skipSchemaCheck bool) (*sql.DB, error) {
	db, err := openConnection(dbPath)
	if err != nil {
		return nil, err
	}

	if err := applyPragmas(db); err != nil {
		db.Close()
		return nil, err
	}

	if err := ensureMigrationTables(db); err != nil {
		db.Close()
		return nil, err
	}

	if err := runOrCheckMigrations(db, autoMigrate, skipSchemaCheck); err != nil {
		db.Close()
		return nil, err
	}

	if err := verifyIntegrity(db, vectorDim); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

// openConnection builds the DSN, opens the DB, and configures the pool.
func openConnection(dbPath string) (*sql.DB, error) {
	v := url.Values{}
	v.Set("_journal_mode", "WAL")
	v.Set("_busy_timeout", "5000")
	v.Set("_sync", "NORMAL")
	v.Set("_fk", "true")

	var dsn string
	switch {
	case dbPath == ":memory:":
		dsn = ":memory:?" + v.Encode()
	case strings.HasPrefix(dbPath, "file:"):
		dsn = dbPath + "&" + v.Encode()
	default:
		dsn = "file:" + dbPath + "?" + v.Encode()
	}

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	return db, nil
}

// applyPragmas sets WAL, synchronous, busy_timeout, auto_vacuum,
// journal_size_limit, cache_size, and verifies journal_mode is WAL.
func applyPragmas(db *sql.DB) error {
	pragmas := []struct {
		stmt string
		name string
	}{
		{"PRAGMA journal_mode = WAL;", "WAL"},
		{"PRAGMA synchronous = NORMAL;", "sync"},
		{"PRAGMA busy_timeout = 5000;", "busy_timeout"},
		{"PRAGMA auto_vacuum = INCREMENTAL;", "auto_vacuum"},
		{"PRAGMA journal_size_limit = 67108864", "journal_size_limit"},
		{"PRAGMA cache_size = -20000", "cache_size"},
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p.stmt); err != nil {
			return fmt.Errorf("%s: %w", p.name, err)
		}
	}
	if _, err := verifyPragmaOrder(db); err != nil {
		return fmt.Errorf("pragma verify: %w", err)
	}
	return nil
}

// ensureMigrationTables creates schema_migrations and migration_checksums
// tables if they don't exist.
func ensureMigrationTables(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at DATETIME DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		return fmt.Errorf("schema_migrations: %w", err)
	}
	if err := ensureMigrationChecksumsTable(db); err != nil {
		return fmt.Errorf("checksums: %w", err)
	}
	return nil
}

// runOrCheckMigrations either auto-applies pending migrations or asserts
// the schema is up to date (§4 refusal mode).
func runOrCheckMigrations(db *sql.DB, autoMigrate, skipSchemaCheck bool) error {
	if autoMigrate {
		if err := RunMigrations(db); err != nil {
			return fmt.Errorf("migrations: %w", err)
		}
	} else if !skipSchemaCheck {
		if err := assertSchemaUpToDate(db); err != nil {
			return err
		}
	}
	return nil
}

// verifyIntegrity runs the flexible schema migration (now a no-op),
// checks embedding_dim meta, and verifies FK pragma.
func verifyIntegrity(db *sql.DB, vectorDim int) error {
	if err := migrateEntitiesFlexibleSchema(db); err != nil {
		return fmt.Errorf("flexible schema: %w", err)
	}
	if err := CheckMeta(db, vectorDim); err != nil {
		return fmt.Errorf("schema validation: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys"); err != nil {
		return fmt.Errorf("fk pragma: %w", err)
	}
	return nil
}

func assertSchemaUpToDate(db *sql.DB) error {
	status, err := MigrationStatus(db)
	if err != nil {
		return fmt.Errorf("refusal-mode migration status: %w", err)
	}
	var pending []string
	for _, m := range status {
		if !m.Applied {
			pending = append(pending, m.Name)
		}
	}
	if len(pending) > 0 {
		sort.Strings(pending)
		return fmt.Errorf("%w: %d pending (%s)", ErrMigrationsPending, len(pending), strings.Join(pending, ", "))
	}
	mismatches, err := VerifyMigrationIntegrity(db)
	if err != nil {
		return fmt.Errorf("refusal-mode integrity check: %w", err)
	}
	if len(mismatches) > 0 {
		names := make([]string, 0, len(mismatches))
		for _, m := range mismatches {
			names = append(names, m.Name)
		}
		sort.Strings(names)
		return fmt.Errorf("%w: %d mismatched (%s)", ErrSchemaIntegrityBroken, len(mismatches), strings.Join(names, ", "))
	}
	return nil
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
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS migration_checksums (version TEXT PRIMARY KEY, checksum TEXT NOT NULL, checksum_sha256 TEXT)`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`ALTER TABLE migration_checksums ADD COLUMN checksum_sha256 TEXT`)
	if err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return err
	}
	return nil
}

// migrateEntitiesFlexibleSchema is a permanent no-op as of 2026-06-24.
func migrateEntitiesFlexibleSchema(_ *sql.DB) error { return nil }

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

// MemDBRandom opens an in-memory database with a per-call random DSN.
func MemDBRandom() (*sql.DB, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, fmt.Errorf("rand: %w", err)
	}
	dsn := fmt.Sprintf("file:memdb-%x?mode=memory&cache=shared", b[:])
	return InitDB(dsn, 3)
}

func verifyPragmaOrder(db *sql.DB) (string, error) {
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		slog.Error("InitDB: PRAGMA journal_mode query failed", "err", err)
		return "", fmt.Errorf("PRAGMA journal_mode: %w", err)
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "wal" {
		slog.Warn("InitDB: journal_mode != WAL; synchronous=NORMAL may be unsafe under power loss",
			"active_mode", mode)
	}
	return mode, nil
}
