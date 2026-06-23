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
	ID             string     `json:"id"`
	Category       string     `json:"category"`
	Content        string     `json:"content"`
	Embedding      []float32  `json:"embedding,omitempty"`
	UpdatedAt      time.Time  `json:"updated_at"`
	LastAccessedAt *time.Time `json:"last_accessed_at"`
	Archived       bool       `json:"archived"`
	Status         string     `json:"status,omitempty"`
}

type Edge struct {
	SourceID     string `json:"source_id"`
	TargetID     string `json:"target_id"`
	RelationType string `json:"relation_type"`
}

var activeSchema = defaultSchemaConfig(false)

func SetActiveSchema(schema SchemaConfig) {
	if schema.AllowedCategories == nil {
		schema = defaultSchemaConfig(false)
	}
	activeSchema = schema
}

func ActiveSchema() SchemaConfig { return activeSchema }

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
			category TEXT NOT NULL,
			content TEXT NOT NULL,
			embedding BLOB,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			status TEXT DEFAULT NULL
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
		{"last_accessed_at", `ALTER TABLE entities ADD COLUMN last_accessed_at DATETIME`},
		{"archived", `ALTER TABLE entities ADD COLUMN archived INTEGER DEFAULT 0`},
		{"status", `ALTER TABLE entities ADD COLUMN status TEXT DEFAULT NULL`},
	}
	for _, m := range migrations {
		if _, err := db.Exec(m.sql); err != nil {
			// Column already exists — ignore
		}
	}
	// Backfill last_accessed_at for rows added before the column existed.
	// SQLite does not allow non-constant defaults in ALTER TABLE ADD COLUMN,
	// so we add the column nullable and populate it in a separate pass.
	if _, err := db.Exec("UPDATE entities SET last_accessed_at = updated_at WHERE last_accessed_at IS NULL"); err != nil {
		return fmt.Errorf("backfill last_accessed_at: %w", err)
	}
	if err := migrateEntitiesFlexibleSchema(db); err != nil {
		return err
	}
	return nil
}

func migrateEntitiesFlexibleSchema(db *sql.DB) error {
	var createSQL string
	err := db.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='entities'").Scan(&createSQL)
	if err != nil {
		return nil
	}
	if !strings.Contains(strings.ToUpper(createSQL), "CHECK(CATEGORY IN") && strings.Contains(createSQL, "status TEXT DEFAULT NULL") {
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

func SetStatus(db *sql.DB, id, status string) error {
	schema := ActiveSchema()
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

func UpdateTaskStatus(db *sql.DB, id, status string) error { return SetStatus(db, id, status) }
func GetTaskStatus(db *sql.DB, id string) (string, error)  { return GetStatus(db, id) }

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
func ListTasks(db *sql.DB, status, goalID string) ([]Entity, error) {
	var wheres []string
	var args []interface{}
	catPH, catArgs := boolMapInClause(ActiveSchema().StatefulCategories)
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
		args = append(args, ActiveSchema().RelationBlocking)
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
func GetTaskWithRelations(db *sql.DB, id string) (Entity, []Edge, []Edge, error) {
	var e Entity
	catPH, catArgs := boolMapInClause(ActiveSchema().StatefulCategories)
	args := append([]interface{}{id}, catArgs...)
	err := db.QueryRow("SELECT id, category, content, status, updated_at FROM entities WHERE id = ? AND category IN ("+catPH+")", args...).Scan(&e.ID, &e.Category, &e.Content, &e.Status, &e.UpdatedAt)
	if err == sql.ErrNoRows {
		return Entity{}, nil, nil, fmt.Errorf("task not found: %s", id)
	}
	if err != nil {
		return Entity{}, nil, nil, fmt.Errorf("get task: %w", err)
	}
	blocked, err := queryEdges(db, "SELECT source_id, target_id, relation_type FROM edges WHERE source_id = ? AND relation_type = ?", id, ActiveSchema().RelationBlocking)
	if err != nil {
		return Entity{}, nil, nil, err
	}
	recovers, err := queryEdges(db, "SELECT source_id, target_id, relation_type FROM edges WHERE source_id = ? AND relation_type = ?", id, ActiveSchema().RelationRecovery)
	if err != nil {
		return Entity{}, nil, nil, err
	}
	return e, blocked, recovers, nil
}

// GetTaskByID returns a task entity by ID.
func GetTaskByID(db *sql.DB, id string) (Entity, error) {
	var e Entity
	catPH, catArgs := boolMapInClause(ActiveSchema().StatefulCategories)
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
func GetTasksByIDs(db *sql.DB, ids []string) (map[string]Entity, error) {
	if len(ids) == 0 {
		return map[string]Entity{}, nil
	}
	phs, args := inClauseArgs(ids)
	catPH, catArgs := boolMapInClause(ActiveSchema().StatefulCategories)
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
func GetBlockedBy(db *sql.DB, id string) ([]Edge, error) {
	return queryEdges(db, "SELECT source_id, target_id, relation_type FROM edges WHERE target_id = ? AND relation_type = ?", id, ActiveSchema().RelationBlocking)
}

// GetRecoversVia returns edges of type 'recovers_via' where target_id = id.
func GetRecoversVia(db *sql.DB, id string) ([]Edge, error) {
	return queryEdges(db, "SELECT source_id, target_id, relation_type FROM edges WHERE target_id = ? AND relation_type = ?", id, ActiveSchema().RelationRecovery)
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
func GetTaskTree(db *sql.DB, rootID string) ([]*TreeNode, error) {
	if rootID != "" {
		_, err := GetTaskByID(db, rootID)
		if err != nil {
			return nil, err
		}
		node, err := buildNode(db, rootID, nil)
		if err != nil {
			return nil, err
		}
		return []*TreeNode{node}, nil
	}

	roots, err := GetRootTasks(db)
	if err != nil {
		return nil, err
	}
	var out []*TreeNode
	for _, root := range roots {
		node, err := buildNode(db, root.ID, nil)
		if err != nil {
			return nil, err
		}
		out = append(out, node)
	}
	return out, nil
}

// GetRootTasks returns tasks that have no blocked_by edges (roots of the DAG).
func GetRootTasks(db *sql.DB) ([]Entity, error) {
	catPH, catArgs := boolMapInClause(ActiveSchema().StatefulCategories)
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
	args := append(catArgs, ActiveSchema().RelationBlocking)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("get root tasks: %w", err)
	}
	defer rows.Close()
	return scanTaskEntities(rows)
}

// buildNode recursively builds a tree node, fetching blocked_by parents as children.
func buildNode(db *sql.DB, id string, visited map[string]bool) (*TreeNode, error) {
	if visited == nil {
		visited = make(map[string]bool)
	}
	if visited[id] {
		return &TreeNode{ID: id, Content: "(cycle)", Status: "cycle"}, nil
	}
	visited[id] = true

	e, err := GetTaskByID(db, id)
	if err != nil {
		return nil, err
	}

	node := &TreeNode{
		ID:      e.ID,
		Content: e.Content,
		Status:  e.Status,
	}

	blocked, err := GetBlockedBy(db, id)
	if err != nil {
		return nil, err
	}

	var childIDs []string
	for _, edge := range blocked {
		childIDs = append(childIDs, edge.SourceID)
	}

	tasks, err := GetTasksByIDs(db, childIDs)
	if err != nil {
		return nil, err
	}

	for _, cid := range childIDs {
		child, err := buildNode(db, cid, visited)
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
