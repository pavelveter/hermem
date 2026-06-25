package store

import (
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// RunMigrations applies pending SQL migrations embedded at compile time.
//
// Each migration file is split via splitSQL (per-line comment dropping,
// per-statement buffer accumulation, CREATE TRIGGER BEGIN/END-aware)
// and its statements are executed one-by-one INSIDE a single transaction.
// A statement whose error matches one of the idempotency strings
// ("duplicate column name", "already exists") is skipped with a single
// warning; the transaction continues. Any other error rolls back the
// entire migration so the next run starts cleanly.
//
// Per-statement execution is the b2 hardening: migrations that mix
// `ALTER TABLE ADD COLUMN` with later `CREATE TABLE` / `CREATE INDEX`
// / `CREATE TRIGGER` statements can now be re-applied safely on
// partially-applied databases (the prior design rolled back the entire
// tx on first duplicate and silently marked the file applied without
// running the remaining statements).
func RunMigrations(db *sql.DB) error {
	files, err := migrationFiles()
	if err != nil {
		return err
	}
	applied, err := appliedMigrations(db)
	if err != nil {
		return fmt.Errorf("read applied migrations: %w", err)
	}

	for _, name := range files {
		if applied[name] {
			continue
		}
		slog.Info("applying migration", "migration", name)
		sqlBytes, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		stmts := splitSQL(string(sqlBytes))
		if len(stmts) == 0 {
			slog.Info("migration applied (empty)", "migration", name)
			continue
		}

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin tx for %s: %w", name, err)
		}

		var hardErr error
		for _, stmt := range stmts {
			if _, err := tx.Exec(stmt); err != nil {
				if isIdempotentMigrationError(err) {
					trim := strings.SplitN(stmt, "\n", 2)[0]
					slog.Info("migration statement skipped (already applied)",
						"migration", name, "stmt", trim, "err", err.Error())
					continue
				}
				// Best-effort rollback; the tx is already doomed
				// after the failed Exec.
				_ = tx.Rollback()
				hardErr = fmt.Errorf("apply migration %s: %w", name, err)
				break
			}
		}
		if hardErr != nil {
			return hardErr
		}

		if _, err := tx.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, CURRENT_TIMESTAMP)", name); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		checksum, err := MigrationChecksum(name)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("checksum %s: %w", name, err)
		}
		checksumSHA256, err := MigrationChecksumSHA256(name)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("checksum sha256 %s: %w", name, err)
		}
		if _, err := tx.Exec("INSERT INTO migration_checksums (version, checksum, checksum_sha256) VALUES (?, ?, ?)", name, checksum, checksumSHA256); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record checksum %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
		slog.Info("migration applied", "migration", name)
	}
	return nil
}

// splitSQL breaks a SQL file into individual statements for per-
// statement execution inside the migration transaction.
//
//   - Whole-line comments starting with `--` are dropped before splitting.
//   - A statement ends at a top-level `;`.
//   - A `CREATE TRIGGER ... BEGIN ... END;` block is ONE statement that
//     ends on a standalone `END;` line — `;` characters inside the
//     trigger body are NOT statement terminators. Detection uses an
//     explicit `seenCreateTrigger` bool that flips when a top-level line
//     contains `CREATE TRIGGER` and clears on any `;`-terminated line,
//     so a stray `BEGIN` (e.g. a future `BEGIN TRANSACTION`) cannot
//     accidentally drag us into trigger mode.
//
// Single-line triggers (`CREATE TRIGGER foo BEGIN ... END;` all on
// one line) and quoted string literals with embedded `;` are NOT
// tracked. Today's 001–007 migrations use neither, so naïve line-
// walking is safe. Extend with a real tokeniser if either is ever
// introduced.
func splitSQL(sqlText string) []string {
	var out []string
	var cur strings.Builder
	inTrigger := false
	seenCreateTrigger := false
	for _, line := range strings.Split(sqlText, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") {
			continue
		}
		cur.WriteString(line)
		cur.WriteByte('\n')

		// Track CREATE TRIGGER being assembled; reset on top-level `;`.
		if !inTrigger {
			if strings.Contains(strings.ToUpper(trimmed), "CREATE TRIGGER") {
				seenCreateTrigger = true
			}
			if trimmed == "BEGIN" && seenCreateTrigger {
				inTrigger = true
				seenCreateTrigger = false
				continue
			}
			if strings.HasSuffix(trimmed, ";") {
				out = append(out, strings.TrimSpace(cur.String()))
				cur.Reset()
				seenCreateTrigger = false
			}
			continue
		}
		// Inside a CREATE TRIGGER body: emit at standalone `END;`.
		if trimmed == "END;" || trimmed == "END" {
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
			inTrigger = false
		}
	}
	rest := strings.TrimSpace(cur.String())
	if rest != "" {
		out = append(out, rest)
	}
	return out
}

func migrationFiles() ([]string, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		files = append(files, e.Name())
	}
	sort.Strings(files)
	return files, nil
}

// isIdempotentMigrationError reports whether a single statement's
// error text indicates the construct is already present and the
// transaction can safely skip past it. Catches:
//
//   - "duplicate column name: <X>"   (ALTER TABLE ... ADD COLUMN)
//   - "index <X> already exists"   (CREATE INDEX without IF NOT EXISTS)
//   - "trigger <X> already exists" (CREATE TRIGGER without IF NOT EXISTS)
//   - "table <X> already exists"   (CREATE TABLE without IF NOT EXISTS,
//     defensive — our Migrations all use IF NOT EXISTS)
//
// Returns false for everything else; the caller treats those as hard
// errors and rolls back.
func isIdempotentMigrationError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "duplicate column name") ||
		strings.Contains(msg, "already exists")
}

func appliedMigrations(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query("SELECT version FROM schema_migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	applied := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

// MigStatus is one migration's applied state.
//
// JSON tags added in PHASE 3.2 so the migration HTTP shell can
// write the type as the envelope directly — same precedent as
// core.ContradictionPair / core.ConnectedComponent / core.Community.
// omitempty on AppliedAt matches the CLI's per-row print contract
// (non-applied rows omit the field rather than render as "").
type MigStatus struct {
	Name           string `json:"name"`
	Applied        bool   `json:"applied"`
	AppliedAt      string `json:"applied_at,omitempty"`
	ChecksumSHA256 string `json:"checksum_sha256,omitempty"`
	ChecksumMatch  *bool  `json:"checksum_match,omitempty"`
}

// MigrationStatus returns the list of all migration files with their
// applied status and SHA-256 checksums (where stored).
func MigrationStatus(db *sql.DB) ([]MigStatus, error) {
	applied, err := appliedMigrations(db)
	if err != nil {
		return nil, err
	}
	appliedAt := make(map[string]string)
	if rows, err := db.Query("SELECT version, applied_at FROM schema_migrations ORDER BY applied_at"); err == nil {
		defer rows.Close()
		for rows.Next() {
			var v, at string
			if err := rows.Scan(&v, &at); err == nil {
				appliedAt[v] = at
			}
		}
	}
	storedChecksums := make(map[string]string)
	if rows, err := db.Query("SELECT version, checksum_sha256 FROM migration_checksums"); err == nil {
		defer rows.Close()
		for rows.Next() {
			var v, c string
			if err := rows.Scan(&v, &c); err == nil {
				storedChecksums[v] = c
			}
		}
	}
	files, _ := PendingMigrations()
	out := make([]MigStatus, 0, len(files))
	for _, name := range files {
		m := MigStatus{Name: name, Applied: applied[name], AppliedAt: appliedAt[name]}
		if sc, ok := storedChecksums[name]; ok && sc != "" {
			m.ChecksumSHA256 = sc
			current, err := MigrationChecksumSHA256(name)
			match := err == nil && sc == current
			m.ChecksumMatch = &match
		}
		out = append(out, m)
	}
	return out, nil
}

// PendingMigrations returns sorted migration file names.
func PendingMigrations() ([]string, error) {
	return migrationFiles()
}

// RollbackMigration removes the last-applied migration.
// When target is non-empty, rolls back every migration applied after
// (and not including) the target version.
func RollbackMigration(db *sql.DB, target string) (string, error) {
	var name string
	err := db.QueryRow("SELECT version FROM schema_migrations ORDER BY applied_at DESC LIMIT 1").Scan(&name)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read last migration: %w", err)
	}
	if target == "" {
		_, _ = db.Exec("DELETE FROM schema_migrations WHERE version = ?", name)
		_, _ = db.Exec("DELETE FROM migration_checksums WHERE version = ?", name)
		slog.Info("migration rolled back", "migration", name)
		return name, nil
	}
	// Target-based rollback: remove everything after target.
	_, err = db.Exec(`DELETE FROM schema_migrations WHERE version > ?`, target)
	if err != nil {
		return "", fmt.Errorf("rollback to %s: %w", target, err)
	}
	_, err = db.Exec(`DELETE FROM migration_checksums WHERE version > ?`, target)
	if err != nil {
		return "", fmt.Errorf("rollback checksums to %s: %w", target, err)
	}
	slog.Info("migration rolled back to target", "target", target)
	return target, nil
}

// MigrationChecksum returns a hex-encoded FNV-1a hash of the migration file contents.
func MigrationChecksum(name string) (string, error) {
	sqlBytes, err := migrationFS.ReadFile("migrations/" + name)
	if err != nil {
		return "", err
	}
	h := uint64(14695981039346656037)
	for _, b := range sqlBytes {
		h ^= uint64(b)
		h *= 1099511628211
	}
	return fmt.Sprintf("%016x", h), nil
}

// MigrationChecksumSHA256 returns a hex-encoded SHA-256 hash of the
// migration file contents. Used for hardened integrity verification.
func MigrationChecksumSHA256(name string) (string, error) {
	sqlBytes, err := migrationFS.ReadFile("migrations/" + name)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(sqlBytes)
	return hex.EncodeToString(h[:]), nil
}

// MigMismatch reports one migration whose stored checksum diverges
// from current.
//
// JSON tags added in PHASE 3.2 so the migration HTTP shell can
// serialise the type directly into the /db/verify envelope (no
// parallel transport struct required).
type MigMismatch struct {
	Name            string `json:"name"`
	StoredChecksum  string `json:"stored_checksum"`
	CurrentChecksum string `json:"current_checksum"`
}

// VerifyMigrationIntegrity compares applied migrations against their
// stored SHA-256 checksums. Returns every migration whose checksum
// diverges (tampered, or applied before SHA-256 was tracked).
func VerifyMigrationIntegrity(db *sql.DB) ([]MigMismatch, error) {
	stored := make(map[string]string)
	if rows, err := db.Query("SELECT version, checksum_sha256 FROM migration_checksums WHERE checksum_sha256 IS NOT NULL"); err == nil {
		defer rows.Close()
		for rows.Next() {
			var v, c string
			if err := rows.Scan(&v, &c); err == nil {
				stored[v] = c
			}
		}
	}
	applied, _ := appliedMigrations(db)
	var mismatches []MigMismatch
	for name := range applied {
		st, ok := stored[name]
		if !ok {
			continue // pre-SHA-256 migration, skip
		}
		current, _ := MigrationChecksumSHA256(name)
		if st != current {
			mismatches = append(mismatches, MigMismatch{Name: name, StoredChecksum: st, CurrentChecksum: current})
		}
	}
	return mismatches, nil
}
