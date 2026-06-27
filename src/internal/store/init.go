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
// and the DB has pending migrations. Caller should either run
// `./hermem db migrate apply` (recommended in a K8s InitContainer or
// pre-deploy step) OR set `[database] auto_migrate = true` in hermem.ini
// to opt in to apply-on-boot.
//
// errors.Is(err, ErrMigrationsPending) is the canonical branch for the
// caller — distinguishes "schema needs upgrade" from "checksums don't
// match" without parsing the error string.
var ErrMigrationsPending = errors.New("schema has pending migrations; run `./hermem db migrate apply` or set `[database] auto_migrate = true` in hermem.ini")

// ErrSchemaIntegrityBroken is returned by InitDBStrict when the stored
// migration checksums diverge from the on-disk migration files. Distinct
// from ErrMigrationsPending so callers can branch: a pending count = an
// out-of-date build, a checksum mismatch = tampered-on-disk files or a
// partial init-container copy.
var ErrSchemaIntegrityBroken = errors.New("schema migration checksums diverge from on-disk files; verify migration files integrity")

// InitDB opens (or creates) the SQLite database with hardened pragma
// settings AND auto-applies pending migrations. Kept for backwards
// compatibility with the pre-§4 codebase (tests, custom embeddings,
// MemDB helpers — all of which benefit from the ergonomic
// apply-on-open). New callers SHOULD use InitDBStrict with
// autoMigrate=false so production inherits the §4 refusal semantic.
//
// §4 audit closure: production boot must NOT auto-apply migrations
// (long-running migrations held up K8s liveness probes → CrashLoopBackOff
// + half-migrated DBs). The refusal semantic lets the operator run
// `./hermem db migrate apply` separately without blocking the daemon.
func InitDB(dbPath string, vectorDim int) (*sql.DB, error) {
	return InitDBStrict(dbPath, vectorDim, true)
}

// InitDBStrict opens the SQLite database and respects the autoMigrate
// gate. When autoMigrate=false, asserts the schema is already at the
// latest migration (zero pending + zero integrity mismatches) and
// refuses to boot otherwise with ErrMigrationsPending or
// ErrSchemaIntegrityBroken (callers can errors.Is to branch).
//
// Refusal-mode UX:
//   - pending migrations > 0  → ErrMigrationsPending wrapped with
//     "N pending (file_a, file_b, ...)" so the operator sees which
//     migrations to apply.
//   - integrity mismatches > 0 → ErrSchemaIntegrityBroken wrapped with
//     "N mismatched (file_a, file_b, ...)" so the operator can inspect.
//   - error message ends with the explicit hinto to run
//     `./hermem db migrate apply` (or set auto_migrate=true).
//
// When autoMigrate=true, behaves identically to InitDB (auto-applies).
func InitDBStrict(dbPath string, vectorDim int, autoMigrate bool) (*sql.DB, error) {
	v := url.Values{}
	v.Set("_journal_mode", "WAL")
	v.Set("_busy_timeout", "5000")
	v.Set("_sync", "NORMAL")
	v.Set("_fk", "true")

	// Build a SQLite URI. The builder is idempotent against a caller that
	// already supplied a `file:` prefix (per-test fixtures use
	// `file:memdb-X?mode=memory&cache=shared`; production uses bare paths
	// like `hermem.db` — both must end up with a single `file:` prefix).
	// When the caller already supplied a `?`, our appended pragmas use
	// `&` so we don't reset the query boundary.
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
	if _, err := verifyPragmaOrder(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("pragma verify: %w", err)
	}

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at DATETIME DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema_migrations: %w", err)
	}

	if err := ensureMigrationChecksumsTable(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("checksums: %w", err)
	}

	if autoMigrate {
		if err := RunMigrations(db); err != nil {
			db.Close()
			return nil, fmt.Errorf("migrations: %w", err)
		}
	} else {
		// §4 refuse-mode: assert schema is at latest migration BEFORE
		// continuing. The schema_migrations/checksums tables are already
		// created above, so MigrationStatus + VerifyMigrationIntegrity
		// can read them. failure modes:
		//   - pending > 0 → operator must run `./hermem db migrate apply`
		//   - mismatched > 0 → tampered/partial init-container — manual fix
		// both paths short-circuit before migrateEntitiesFlexibleSchema
		// (which sets up entity-table columns) and CheckMeta (which
		// writes the embedding_dim row), so a refused boot doesn't
		// partially mutate the DB even on meta-touching paths.
		if err := assertSchemaUpToDate(db); err != nil {
			db.Close()
			return nil, err
		}
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

// assertSchemaUpToDate queries MigrationStatus + VerifyMigrationIntegrity.
// Returns nil iff zero pending AND zero mismatches.
//
// On failure, wraps the appropriate sentinel with a deterministic,
// sorted list of offender file names so two consecutive failures yield
// the same error message (operator-friendly for alerting / log diffs).
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
	// PHASE 3.11: backfill checksum_sha256 for DBs created before the
	// column was added to the CREATE TABLE. Idempotent: ignores
	// "duplicate column name" if the column already exists.
	_, err = db.Exec(`ALTER TABLE migration_checksums ADD COLUMN checksum_sha256 TEXT`)
	if err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return err
	}
	return nil
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

// WARNING: shared in-memory DBs cannot use PRAGMA journal_mode=WAL.
// SQLite silently reverts the mode to "memory" because there is no
// underlying file to host WAL frames. Tests calling verifyPragmaOrder
// on a fixture from this helper will see mode "memory", not "wal".
// Use a file-backed DSN via t.TempDir() instead when asserting WAL.
//
// InitDB's pragmas (`_journal_mode=WAL&_busy_timeout=5000&_sync=NORMAL&_fk=true`)
// are AUTHORITATIVE: SQLite's URI parser uses last-write-wins for any
// duplicate query key — no error is surfaced, and any caller-supplied
// value is silently overridden. So we do NOT add ANY sqlite3 DSN pragma
// here; doing so could let a stale value of e.g. `_fk` or `cache` slip
// through if a future driver changes precedence.
//
// MemDBRandom opens an in-memory database with a per-call random DSN so
// concurrent tests don't share the global `:memory:` cache. Each call
// gets a fresh `file:memdb-<hex>?mode=memory&cache=shared` DSN — the
// random suffix prevents two goroutines opening the SAME shared cache
// from racing on schema_migrations / entities table creation. InitDB
// itself appends `&_journal_mode=WAL&_busy_timeout=5000&_sync=NORMAL&_fk=true`
// to the query string. `_busy_timeout=5000` keeps concurrent Commits
// from eating SQLITE_BUSY under `-race`. Drop this on every test that
// opens its own DB; production code stays on MemDB() (or InitDB(realpath, ...)).
func MemDBRandom() (*sql.DB, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, fmt.Errorf("rand: %w", err)
	}
	dsn := fmt.Sprintf("file:memdb-%x?mode=memory&cache=shared", b[:])
	return InitDB(dsn, 3)
}

// verifyPragmaOrder asserts journal_mode is WAL. Without WAL,
// `synchronous=NORMAL` may corrupt on power loss (the DSN we set is
// journal_mode=WAL _and_ _sync=NORMAL; SQLite applies them in order,
// and the SQL PRAGMAs below are a defence-in-depth). If journal_mode
// is not WAL, log a WARN so an operator notices on first start-up
// rather than on the first failing checkpoint.
//
// Returns the active mode (trimmed, lower-cased) so callers and tests
// can branch on observed state without re-querying.
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
