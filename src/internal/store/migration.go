package store

import (
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// RunMigrations applies pending SQL migrations embedded at compile time.
func RunMigrations(db *sql.DB) error {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		files = append(files, e.Name())
	}
	sort.Strings(files)
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
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin tx for %s: %w", name, err)
		}
		_, execErr := tx.Exec(string(sqlBytes))
		if execErr != nil {
			tx.Rollback()
			if strings.Contains(execErr.Error(), "duplicate column name") {
				slog.Info("migration skipped (columns already exist)", "migration", name)
				recTx, _ := db.Begin()
				_, _ = recTx.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, CURRENT_TIMESTAMP)", name)
				_ = recTx.Commit()
				continue
			}
			return fmt.Errorf("apply migration %s: %w", name, execErr)
		}
		_, _ = tx.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, CURRENT_TIMESTAMP)", name)
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
		slog.Info("migration applied", "migration", name)
	}
	return nil
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
type MigStatus struct {
	Name      string
	Applied   bool
	AppliedAt string
}

// MigrationStatus returns the list of all migration files with their applied status.
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
	files, _ := PendingMigrations()
	out := make([]MigStatus, 0, len(files))
	for _, name := range files {
		out = append(out, MigStatus{Name: name, Applied: applied[name], AppliedAt: appliedAt[name]})
	}
	return out, nil
}

// PendingMigrations returns sorted migration file names.
func PendingMigrations() ([]string, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, err
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

// RollbackMigration removes the last-applied migration.
func RollbackMigration(db *sql.DB) (string, error) {
	var name string
	err := db.QueryRow("SELECT version FROM schema_migrations ORDER BY applied_at DESC LIMIT 1").Scan(&name)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read last migration: %w", err)
	}
	_, _ = db.Exec("DELETE FROM schema_migrations WHERE version = ?", name)
	_, _ = db.Exec("DELETE FROM migration_checksums WHERE version = ?", name)
	slog.Info("migration rolled back", "migration", name)
	return name, nil
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

// MigMismatch reports one migration whose stored checksum diverges from current.
type MigMismatch struct {
	Name            string
	StoredChecksum  string
	CurrentChecksum string
}

// VerifyMigrationIntegrity compares applied migrations against their current checksums.
func VerifyMigrationIntegrity(db *sql.DB) ([]MigMismatch, error) {
	stored := make(map[string]string)
	if rows, err := db.Query("SELECT version, checksum FROM migration_checksums"); err == nil {
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
		current, _ := MigrationChecksum(name)
		if st, ok := stored[name]; ok && st != current {
			mismatches = append(mismatches, MigMismatch{Name: name, StoredChecksum: st, CurrentChecksum: current})
		}
	}
	return mismatches, nil
}
