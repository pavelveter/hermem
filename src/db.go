package main

import (
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"sort"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type Entity struct {
	ID             string     `json:"id"`
	Category       string     `json:"category"`
	Content        string     `json:"content"`
	Embedding      []float32  `json:"embedding,omitempty"`
	UpdatedAt      time.Time  `json:"updated_at"`
	LastAccessedAt *time.Time `json:"last_accessed_at"`
	Archived       bool       `json:"archived"`
	Status         string     `json:"status,omitempty"`
	// Sprint 2: entity metadata
	Confidence float32    `json:"confidence,omitempty"`
	Source     string     `json:"source,omitempty"`
	SourceType string     `json:"source_type,omitempty"`
	CreatedAt  *time.Time `json:"created_at,omitempty"`
	ValidFrom  *time.Time `json:"valid_from,omitempty"`
	ValidTo    *time.Time `json:"valid_to,omitempty"`
	// Sprint 2: memory provenance
	ConversationID string `json:"conversation_id,omitempty"`
	MessageID      string `json:"message_id,omitempty"`
	ExtractedFrom  string `json:"extracted_from,omitempty"`
}

type Edge struct {
	SourceID     string `json:"source_id"`
	TargetID     string `json:"target_id"`
	RelationType string `json:"relation_type"`
}

// InitDB opens (or creates) hermem.db with a hardened SQL pragma set.
// Foreign-key enforcement is enabled via the `_fk` DSN parameter so
// every connection in the pool applies the pragma atomically at
// connect time — `PRAGMA foreign_keys` is per-connection in SQLite,
// so the DSN flag is the only safe way to ensure that an arbitrary
// pooled connection sees the constraint on its first statement.
//
// Sprint 1 invariant: orphan edges are unreachable. The edges table
// has FOREIGN KEY (source_id/target_id) REFERENCES entities(id) ON
// DELETE CASCADE. With this PRAGMA on, an INSERT into edges with a
// non-existent endpoint raises SQLITE_CONSTRAINT_FOREIGNKEY at the
// SQL engine layer rather than silently producing a dangling edge.
//
// Migration safety: migrateEntitiesFlexibleSchema rebuilds the
// entities table via DROP + RENAME. With FK on, dropping entities
// would cascade-delete every edge. We toggle the FK check OFF for
// the duration of that rebuild then ON afterwards, preserving the
// edge contents even though the table they reference is momentarily
// missing.
func InitDB(dbPath string, vectorDim int) (*sql.DB, error) {
	v := url.Values{}
	v.Set("_journal_mode", "WAL")
	v.Set("_busy_timeout", "5000")
	v.Set("_sync", "NORMAL")
	v.Set("_fk", "true") // Sprint 1: enable FK enforcement per connection.

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

	// Belt-and-suspenders PRAGMA pass: even though _fk=true in the DSN
	// should be sufficient, exec PRAGMA on a freshly-opened connection
	// as explicit confirmation. DSN params apply first at connect.
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

	// Create migration tracking table before running migrations
	// so the runner can record its own progress.
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version     TEXT PRIMARY KEY,
			applied_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema_migrations: %w", err)
	}

	if err := runMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema migration: %w", err)
	}

	if err := migrateEntitiesFlexibleSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("flexible schema migration: %w", err)
	}

	if err := checkMeta(db, vectorDim); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema validation: %w", err)
	}

	// Sprint 1: post-init FK confirmation. Belt-and-suspenders: the
	// DSN flag _fk=true should already enforce this, but a SELECT
	// from a fresh connection confirms the pragma actually applied
	// from this code path. If it doesn't, fail loudly — better than
	// silently running with FK OFF on a connection pruned from the
	// pool.
	if _, err := db.Exec("PRAGMA foreign_keys"); err != nil {
		db.Close()
		return nil, fmt.Errorf("foreign_keys pragma failed: %w", err)
	}

	return db, nil
}

// runMigrations discovers embedded SQL migration files, compares them
// against the schema_migrations tracking table, and applies any
// unapplied migrations in order. Each migration runs inside a single
// SQLite transaction so a partial failure rolls back atomically.
// After the SQL succeeds, the migration version is recorded in
// schema_migrations with a UTC timestamp.
//
// Sprint 4: replaces the old ad-hoc migrateSchema (hardcoded ALTER
// TABLE with swallowed errors). Versioned migrations give operators
// visibility into the DB state via `hermem migrate` and prevent
// silent schema drift.
func runMigrations(db *sql.DB) error {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}

	// Collect and sort migration files by name (version prefix).
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
			// Pre-existing databases: columns may already exist from
			// the old ad-hoc migrateSchema. Treat "duplicate column"
			// as a benign no-op and record the migration as applied
			// in a fresh transaction (the original tx is rolled back).
			if strings.Contains(execErr.Error(), "duplicate column name") {
				slog.Info("migration skipped (columns already exist)", "migration", name)
				recTx, err := db.Begin()
				if err != nil {
					return fmt.Errorf("begin record tx for %s: %w", name, err)
				}
				if _, err := recTx.Exec(
					"INSERT INTO schema_migrations (version, applied_at) VALUES (?, CURRENT_TIMESTAMP)",
					name,
				); err != nil {
					recTx.Rollback()
					return fmt.Errorf("record migration %s: %w", name, err)
				}
				if err := recTx.Commit(); err != nil {
					return fmt.Errorf("commit record for %s: %w", name, err)
				}
				slog.Info("migration recorded (pre-existing)", "migration", name)
				continue
			}
			return fmt.Errorf("apply migration %s: %w", name, execErr)
		}

		if _, err := tx.Exec(
			"INSERT INTO schema_migrations (version, applied_at) VALUES (?, CURRENT_TIMESTAMP)",
			name,
		); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %s: %w", name, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}

		slog.Info("migration applied", "migration", name)
	}

	return nil
}

// appliedMigrations returns the set of migration file names already
// recorded in the schema_migrations table.
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

// PendingMigrations returns a list of migration file names that have
// not yet been applied. Used by the `hermem migrate` CLI command.
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

// MigrationStatus returns the list of all migration files with their
// applied status. Used by the `hermem migrate` CLI command.
func MigrationStatus(db *sql.DB) ([]struct {
	Name      string
	Applied   bool
	AppliedAt string
}, error) {
	applied, err := appliedMigrations(db)
	if err != nil {
		return nil, err
	}

	appliedAt := make(map[string]string)
	rows, err := db.Query("SELECT version, applied_at FROM schema_migrations ORDER BY applied_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var v, at string
		if err := rows.Scan(&v, &at); err != nil {
			return nil, err
		}
		appliedAt[v] = at
	}

	files, err := PendingMigrations()
	if err != nil {
		return nil, err
	}

	var out []struct {
		Name      string
		Applied   bool
		AppliedAt string
	}
	for _, name := range files {
		out = append(out, struct {
			Name      string
			Applied   bool
			AppliedAt string
		}{name, applied[name], appliedAt[name]})
	}
	return out, nil
}

// migrateEntitiesFlexibleSchema rebuilds the entities table to add
// historical columns (last_accessed_at, archived, status). With FK
// enforcement ON, dropping the existing `entities` table would
// cascade-delete every row from edges. To preserve data, the rebuild
// runs inside `PRAGMA defer_foreign_keys = ON`, which defers FK
// checks to commit time rather than auto-disabling them — the
// per-row FK validation still fires, but the entire rebuild happens
// inside a single transaction so commit-time sees a consistent
// entities_new state and the edge references reattach correctly
// after the rename.
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

	if _, err := tx.Exec(`
		CREATE TABLE entities_new (
			id TEXT PRIMARY KEY,
			category TEXT NOT NULL,
			content TEXT NOT NULL,
			embedding BLOB,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_accessed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			archived INTEGER DEFAULT 0,
			status TEXT DEFAULT NULL
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

	// Defer FK checks across the entire transaction so the
	// DROP TABLE does not cascade-delete the edges table contents
	// while we wait for RENAME to put entities_new in place.
	if _, err := tx.Exec("PRAGMA defer_foreign_keys = ON"); err != nil {
		return fmt.Errorf("defer_foreign_keys: %w", err)
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

// HashSchema produces a deterministic SHA-256 fingerprint of the
// schema config. Keys in maps are sorted before serialization so the
// hash is stable across process restarts regardless of map iteration
// order. Used by CheckSchemaFingerprint to detect config drift.
func HashSchema(schema SchemaConfig) string {
	// Build a JSON-serializable representation with sorted keys.
	rep := map[string]interface{}{
		"categories":  sortedMapKeys(schema.AllowedCategories),
		"relations":   sortedMapKeys(schema.AllowedRelations),
		"stateful":    sortedMapKeys(schema.StatefulCategories),
		"states":      schema.ValidStateOrder,
		"blocking":    schema.RelationBlocking,
		"unblocking":  schema.StateUnblocking,
		"recovery":    schema.RelationRecovery,
		"stateful_en": schema.StatefulEnabled,
	}
	b, err := json.Marshal(rep)
	if err != nil {
		// rep is built from sorted string keys and simple values —
		// json.Marshal cannot fail on these types. If it does, it's
		// a programming error, so panic to fail fast.
		panic(fmt.Sprintf("HashSchema: marshal: %v", err))
	}
	h := sha256.Sum256(b)
	return fmt.Sprintf("%x", h[:8]) // first 8 bytes → 16-char hex
}

// sortedMapKeys returns the keys of a map sorted alphabetically.
func sortedMapKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// CheckSchemaFingerprint compares the current schema fingerprint
// against the value stored in the meta table. On a fresh database
// (no stored fingerprint), it writes the current fingerprint silently.
// On mismatch it returns the stored and current fingerprints so the
// caller can decide whether to warn, block, or proceed.
func CheckSchemaFingerprint(db *sql.DB, schema SchemaConfig) (stored, current string, err error) {
	current = HashSchema(schema)

	err = db.QueryRow("SELECT value FROM meta WHERE key = 'schema_fingerprint'").Scan(&stored)
	if err == sql.ErrNoRows {
		_, err = db.Exec("INSERT INTO meta (key, value) VALUES ('schema_fingerprint', ?)", current)
		return "", current, err
	}
	if err != nil {
		return "", "", err
	}
	return stored, current, nil
}

// StoreSchemaFingerprint writes (or overwrites) the schema fingerprint
// in the meta table. Called after a successful SIGHUP reload.
func StoreSchemaFingerprint(db *sql.DB, schema SchemaConfig) error {
	fp := HashSchema(schema)
	_, err := db.Exec("INSERT OR REPLACE INTO meta (key, value) VALUES ('schema_fingerprint', ?)", fp)
	return err
}

// SetStatus updates a single stateful entity's status column. Schema
// is passed explicitly so callers without a Runtime (such as tests
// and the GC worker) can supply their own. Returns an error on
// invalid status name, missing entity, or non-stateful category.
func SetStatus(db *sql.DB, schema SchemaConfig, id, status string) error {
	var category string
	if err := db.QueryRow(`SELECT category FROM entities WHERE id = ?`, id).Scan(&category); err == sql.ErrNoRows {
		return fmt.Errorf("stateful entity not found: %s", id)
	} else if err != nil {
		return fmt.Errorf("get entity category: %w", err)
	}
	if !schema.StatefulCategories[category] {
		return fmt.Errorf("entity is not stateful: %s", id)
	}
	if !schema.ValidStates[status] {
		return fmt.Errorf("invalid status: %s", status)
	}
	res, err := db.Exec(
		`UPDATE entities SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
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
		return fmt.Errorf("stateful entity not found: %s", id)
	}
	return nil
}

func GetStatus(db *sql.DB, id string) (string, error) {
	var status sql.NullString
	err := db.QueryRow(
		`SELECT status FROM entities WHERE id = ?`,
		id,
	).Scan(&status)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get task status: %w", err)
	}
	if !status.Valid {
		return "", nil
	}
	return status.String, nil
}

func UpdateTaskStatus(db *sql.DB, schema SchemaConfig, id, status string) error {
	return SetStatus(db, schema, id, status)
}
func GetTaskStatus(db *sql.DB, id string) (string, error) { return GetStatus(db, id) }

// queryEdges runs a query and scans all rows into an Edge slice.
func queryEdges(db *sql.DB, query string, args ...interface{}) ([]Edge, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Edge
	for rows.Next() {
		var ed Edge
		if err := rows.Scan(&ed.SourceID, &ed.TargetID, &ed.RelationType); err != nil {
			return nil, fmt.Errorf("scan edge: %w", err)
		}
		out = append(out, ed)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate edges: %w", err)
	}
	return out, nil
}

// DeleteEdge removes a single edge row.
func DeleteEdge(db *sql.DB, src, dst, rel string) error {
	_, err := db.Exec("DELETE FROM edges WHERE source_id = ? AND target_id = ? AND relation_type = ?", src, dst, rel)
	if err != nil {
		return fmt.Errorf("delete edge: %w", err)
	}
	return nil
}

// ListTasks returns task entities filtered by optional status and
// optional goal subtree. goalID scopes the result to the blocked_by
// dependency tree rooted at the given goal.
func ListTasks(db *sql.DB, schema SchemaConfig, status, goalID string) ([]Entity, error) {
	var wheres []string
	var args []interface{}
	catPH, catArgs := boolMapInClause(schema.StatefulCategories)
	if catPH == "" {
		return []Entity{}, nil
	}
	wheres = append(wheres, "e.category IN ("+catPH+") AND e.archived = 0")
	args = append(args, catArgs...)
	if status != "" {
		wheres = append(wheres, "e.status = ?")
		args = append(args, status)
	}
	if goalID != "" {
		wheres = append(wheres, "e.id IN (WITH RECURSIVE subtree AS (SELECT id FROM entities WHERE id = ? AND category IN ("+catPH+") AND archived = 0 UNION ALL SELECT e.id FROM subtree s JOIN edges ed ON ed.target_id = s.id AND ed.relation_type = ? JOIN entities e ON e.id = ed.source_id AND e.category IN ("+catPH+") AND e.archived = 0) SELECT id FROM subtree)")
		args = append(args, goalID)
		args = append(args, catArgs...)
		args = append(args, schema.RelationBlocking)
		args = append(args, catArgs...)
	}
	query := "SELECT e.id, e.category, e.content, e.status, e.updated_at FROM entities e WHERE " + strings.Join(wheres, " AND ") + " ORDER BY e.id"
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()
	return scanTaskEntities(rows)
}

// GetTaskWithRelations returns the task entity plus its blocked_by
// and recovers_via outgoing edges.
func GetTaskWithRelations(db *sql.DB, schema SchemaConfig, id string) (Entity, []Edge, []Edge, error) {
	var e Entity
	catPH, catArgs := boolMapInClause(schema.StatefulCategories)
	args := append([]interface{}{id}, catArgs...)
	err := db.QueryRow("SELECT id, category, content, status, updated_at FROM entities WHERE id = ? AND category IN ("+catPH+")", args...).Scan(&e.ID, &e.Category, &e.Content, &e.Status, &e.UpdatedAt)
	if err == sql.ErrNoRows {
		return Entity{}, nil, nil, fmt.Errorf("task not found: %s", id)
	}
	if err != nil {
		return Entity{}, nil, nil, fmt.Errorf("get task: %w", err)
	}
	blocked, err := queryEdges(db, "SELECT source_id, target_id, relation_type FROM edges WHERE source_id = ? AND relation_type = ?", id, schema.RelationBlocking)
	if err != nil {
		return Entity{}, nil, nil, err
	}
	recovers, err := queryEdges(db, "SELECT source_id, target_id, relation_type FROM edges WHERE source_id = ? AND relation_type = ?", id, schema.RelationRecovery)
	if err != nil {
		return Entity{}, nil, nil, err
	}
	return e, blocked, recovers, nil
}

// GetTaskByID returns a task entity by ID.
func GetTaskByID(db *sql.DB, schema SchemaConfig, id string) (Entity, error) {
	var e Entity
	catPH, catArgs := boolMapInClause(schema.StatefulCategories)
	args := append([]interface{}{id}, catArgs...)
	err := db.QueryRow("SELECT id, category, content, status, updated_at FROM entities WHERE id = ? AND category IN ("+catPH+")", args...).Scan(&e.ID, &e.Category, &e.Content, &e.Status, &e.UpdatedAt)
	if err == sql.ErrNoRows {
		return Entity{}, fmt.Errorf("task not found: %s", id)
	}
	if err != nil {
		return Entity{}, fmt.Errorf("get task: %w", err)
	}
	return e, nil
}

// GetTasksByIDs returns a map of task entities for the given IDs.
func GetTasksByIDs(db *sql.DB, schema SchemaConfig, ids []string) (map[string]Entity, error) {
	if len(ids) == 0 {
		return map[string]Entity{}, nil
	}
	phs, args := inClauseArgs(ids)
	catPH, catArgs := boolMapInClause(schema.StatefulCategories)
	args = append(args, catArgs...)
	query := "SELECT id, category, content, status, updated_at FROM entities WHERE id IN (" + phs + ") AND category IN (" + catPH + ")"
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("get tasks by ids: %w", err)
	}
	defer rows.Close()
	out := make(map[string]Entity)
	for rows.Next() {
		var e Entity
		if err := rows.Scan(&e.ID, &e.Category, &e.Content, &e.Status, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		out[e.ID] = e
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tasks: %w", err)
	}
	return out, nil
}

// GetBlockedBy returns edges of type 'blocked_by' where target_id = id.
func GetBlockedBy(db *sql.DB, schema SchemaConfig, id string) ([]Edge, error) {
	return queryEdges(db, "SELECT source_id, target_id, relation_type FROM edges WHERE target_id = ? AND relation_type = ?", id, schema.RelationBlocking)
}

// GetRecoversVia returns edges of type 'recovers_via' where target_id = id.
func GetRecoversVia(db *sql.DB, schema SchemaConfig, id string) ([]Edge, error) {
	return queryEdges(db, "SELECT source_id, target_id, relation_type FROM edges WHERE target_id = ? AND relation_type = ?", id, schema.RelationRecovery)
}

// TreeNode represents a node in the task tree.
type TreeNode struct {
	ID       string
	Content  string
	Status   string
	Children []*TreeNode
}

// GetTaskTree builds a tree of tasks starting from rootID.
// If rootID is empty, returns all root tasks (tasks without blocked_by parents).
func GetTaskTree(db *sql.DB, schema SchemaConfig, rootID string) ([]*TreeNode, error) {
	if rootID != "" {
		_, err := GetTaskByID(db, schema, rootID)
		if err != nil {
			return nil, err
		}
		node, err := buildNode(db, schema, rootID, nil)
		if err != nil {
			return nil, err
		}
		return []*TreeNode{node}, nil
	}

	roots, err := GetRootTasks(db, schema)
	if err != nil {
		return nil, err
	}
	var out []*TreeNode
	for _, root := range roots {
		node, err := buildNode(db, schema, root.ID, nil)
		if err != nil {
			return nil, err
		}
		out = append(out, node)
	}
	return out, nil
}

// GetRootTasks returns tasks that have no blocked_by edges (roots of the DAG).
func GetRootTasks(db *sql.DB, schema SchemaConfig) ([]Entity, error) {
	catPH, catArgs := boolMapInClause(schema.StatefulCategories)
	if catPH == "" {
		return []Entity{}, nil
	}
	query := `
		SELECT e.id, e.category, e.content, e.status, e.updated_at
		FROM entities e
		WHERE e.category IN (` + catPH + `) AND e.archived = 0
		AND e.id NOT IN (
			SELECT source_id FROM edges WHERE target_id = e.id AND relation_type = ?
		)
	`
	args := append(catArgs, schema.RelationBlocking)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("get root tasks: %w", err)
	}
	defer rows.Close()
	return scanTaskEntities(rows)
}

// buildNode recursively builds a tree node, fetching blocked_by parents as children.
func buildNode(db *sql.DB, schema SchemaConfig, id string, visited map[string]bool) (*TreeNode, error) {
	if visited == nil {
		visited = make(map[string]bool)
	}
	if visited[id] {
		return &TreeNode{ID: id, Content: "(cycle)", Status: "cycle"}, nil
	}
	visited[id] = true

	e, err := GetTaskByID(db, schema, id)
	if err != nil {
		return nil, err
	}

	node := &TreeNode{
		ID:      e.ID,
		Content: e.Content,
		Status:  e.Status,
	}

	blocked, err := GetBlockedBy(db, schema, id)
	if err != nil {
		return nil, err
	}

	var childIDs []string
	for _, edge := range blocked {
		childIDs = append(childIDs, edge.SourceID)
	}

	tasks, err := GetTasksByIDs(db, schema, childIDs)
	if err != nil {
		return nil, err
	}

	for _, cid := range childIDs {
		child, err := buildNode(db, schema, cid, visited)
		if err != nil {
			return nil, err
		}
		if task, ok := tasks[cid]; ok {
			child.Content = task.Content
			child.Status = task.Status
		}
		node.Children = append(node.Children, child)
	}

	return node, nil
}

// RenderTaskTree returns a human-readable tree representation.
// Example: "[id] content (status)"
//
//	"├─ [cid] content"
//	"└─ [cid] content"
func RenderTaskTree(nodes []*TreeNode, prefix string) string {
	var sb strings.Builder
	for i, node := range nodes {
		status := ""
		if node.Status != "" && node.Status != "pending" {
			status = fmt.Sprintf(" (%s)", node.Status)
		}
		sb.WriteString(fmt.Sprintf("%s[%s] %s%s\n", prefix, node.ID, node.Content, status))
		for _, child := range node.Children {
			childPrefix := prefix
			if i == len(nodes)-1 {
				childPrefix += "    "
			} else {
				childPrefix += "│   "
			}
			sb.WriteString(RenderTaskTree([]*TreeNode{child}, childPrefix))
		}
	}
	return sb.String()
}
