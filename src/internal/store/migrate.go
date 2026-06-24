package store

import (
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"

	"github.com/pavelveter/hermem/src/internal/core"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// RunMigrations applies pending SQL migrations.
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
				recTx.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, CURRENT_TIMESTAMP)", name)
				recTx.Commit()
				continue
			}
			return fmt.Errorf("apply migration %s: %w", name, execErr)
		}
		tx.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, CURRENT_TIMESTAMP)", name)
		tx.Commit()
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
	rows, _ := db.Query("SELECT version, applied_at FROM schema_migrations ORDER BY applied_at")
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var v, at string
			rows.Scan(&v, &at)
			appliedAt[v] = at
		}
	}
	files, _ := PendingMigrations()
	var out []MigStatus
	for _, name := range files {
		out = append(out, MigStatus{name, applied[name], appliedAt[name]})
	}
	return out, nil
}

// PendingMigrations returns migration file names.
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
	db.Exec("DELETE FROM schema_migrations WHERE version = ?", name)
	db.Exec("DELETE FROM migration_checksums WHERE version = ?", name)
	slog.Info("migration rolled back", "migration", name)
	return name, nil
}

// MigrationChecksum returns a hex-encoded FNV-1a hash.
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

type MigMismatch struct {
	Name            string
	StoredChecksum  string
	CurrentChecksum string
}

// VerifyMigrationIntegrity checks all applied migrations against current checksums.
func VerifyMigrationIntegrity(db *sql.DB) ([]MigMismatch, error) {
	stored := make(map[string]string)
	if rows, err := db.Query("SELECT version, checksum FROM migration_checksums"); err == nil {
		defer rows.Close()
		for rows.Next() {
			var v, c string
			rows.Scan(&v, &c)
			stored[v] = c
		}
	}
	applied, _ := appliedMigrations(db)
	var mismatches []MigMismatch
	for name := range applied {
		current, _ := MigrationChecksum(name)
		if st, ok := stored[name]; ok && st != current {
			mismatches = append(mismatches, MigMismatch{name, st, current})
		}
	}
	return mismatches, nil
}

// HashSchema produces a deterministic SHA-256 fingerprint.
func HashSchema(schema core.SchemaConfig) string {
	rep := map[string]interface{}{
		"categories": SortedKeys(schema.AllowedCategories),
		"relations":  SortedKeys(schema.AllowedRelations),
		"stateful":   SortedKeys(schema.StatefulCategories),
		"states":     schema.ValidStateOrder,
		"blocking":   schema.RelationBlocking,
		"unblocking": schema.StateUnblocking,
		"recovery":   schema.RelationRecovery,
	}
	b, _ := json.Marshal(rep)
	h := sha256.Sum256(b)
	return fmt.Sprintf("%x", h[:8])
}

// CheckSchemaFingerprint compares current vs stored schema fingerprint.
func CheckSchemaFingerprint(db *sql.DB, schema core.SchemaConfig) (stored, current string, err error) {
	current = HashSchema(schema)
	err = db.QueryRow("SELECT value FROM meta WHERE key = 'schema_fingerprint'").Scan(&stored)
	if err == sql.ErrNoRows {
		db.Exec("INSERT INTO meta (key, value) VALUES ('schema_fingerprint', ?)", current)
		return "", current, nil
	}
	if err != nil {
		return "", "", err
	}
	return stored, current, nil
}

// StoreSchemaFingerprint writes the current schema fingerprint.
func StoreSchemaFingerprint(db *sql.DB, schema core.SchemaConfig) error {
	_, err := db.Exec("INSERT OR REPLACE INTO meta (key, value) VALUES ('schema_fingerprint', ?)", HashSchema(schema))
	return err
}

// EmbeddingToBytes converts a float32 slice to little-endian bytes.
func EmbeddingToBytes(embedding []float32) []byte {
	buf := make([]byte, len(embedding)*4)
	for i, v := range embedding {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

// BytesToEmbedding converts bytes back to float32 slice.
func BytesToEmbedding(data []byte) []float32 {
	if len(data)%4 != 0 {
		return nil
	}
	embedding := make([]float32, len(data)/4)
	for i := range embedding {
		embedding[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4 : i*4+4]))
	}
	return embedding
}

// DecodeVector decodes a BLOB into a float32 slice with dimension validation.
func DecodeVector(data []byte, expectedDim int) ([]float32, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty vector blob")
	}
	if len(data) != expectedDim*4 {
		return nil, fmt.Errorf("vector dimension drift: blob %d bytes, want %d", len(data), expectedDim*4)
	}
	emb := make([]float32, expectedDim)
	for i := range emb {
		emb[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4 : i*4+4]))
	}
	return emb, nil
}
