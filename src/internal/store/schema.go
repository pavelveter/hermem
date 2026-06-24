package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/pavelveter/hermem/src/internal/core"
)

// HashSchema produces a deterministic SHA-256 fingerprint of the schema config.
// Used to detect SIGHUP-triggered schema drift between disk and DB.
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
// On first run (no row) it inserts the current fingerprint and returns stored="".
func CheckSchemaFingerprint(db *sql.DB, schema core.SchemaConfig) (stored, current string, err error) {
	current = HashSchema(schema)
	err = db.QueryRow("SELECT value FROM meta WHERE key = 'schema_fingerprint'").Scan(&stored)
	if err == sql.ErrNoRows {
		_, _ = db.Exec("INSERT INTO meta (key, value) VALUES ('schema_fingerprint', ?)", current)
		return "", current, nil
	}
	if err != nil {
		return "", "", err
	}
	return stored, current, nil
}

// StoreSchemaFingerprint overwrites the stored schema fingerprint with the current one.
func StoreSchemaFingerprint(db *sql.DB, schema core.SchemaConfig) error {
	_, err := db.Exec("INSERT OR REPLACE INTO meta (key, value) VALUES ('schema_fingerprint', ?)", HashSchema(schema))
	return err
}
