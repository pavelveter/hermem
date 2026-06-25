package store

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/pavelveter/hermem/src/internal/core"
)

// ListTasks returns task entities filtered by optional status and goal subtree.
func ListTasks(db *sql.DB, schema core.SchemaConfig, status, goalID string) ([]core.Entity, error) {
	var wheres []string
	var args []interface{}
	catPH, catArgs := BoolMapInClause(schema.StatefulCategories)
	if catPH == "" {
		return []core.Entity{}, nil
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
	query := "SELECT e.id, e.category, e.content, COALESCE(e.status, '') AS status, e.updated_at, COALESCE(e.priority, 0) FROM entities e WHERE " + strings.Join(wheres, " AND ") + " ORDER BY e.id"
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()
	return ScanTaskEntities(rows)
}

// GetTaskWithRelations returns a task entity plus its blocked_by and recovers_via edges.
func GetTaskWithRelations(db *sql.DB, schema core.SchemaConfig, id string) (core.Entity, []core.Edge, []core.Edge, error) {
	var e core.Entity
	catPH, catArgs := BoolMapInClause(schema.StatefulCategories)
	args := append([]interface{}{id}, catArgs...)
	err := db.QueryRow("SELECT id, category, content, COALESCE(status, '') AS status, updated_at FROM entities WHERE id = ? AND category IN ("+catPH+")", args...).Scan(&e.ID, &e.Category, &e.Content, &e.Status, &e.UpdatedAt)
	if err == sql.ErrNoRows {
		return core.Entity{}, nil, nil, fmt.Errorf("task not found: %s", id)
	}
	if err != nil {
		return core.Entity{}, nil, nil, fmt.Errorf("get task: %w", err)
	}
	blocked, err := QueryEdges(db, "SELECT source_id, target_id, relation_type, COALESCE(weight, 1.0) FROM edges WHERE source_id = ? AND relation_type = ?", id, schema.RelationBlocking)
	if err != nil {
		return core.Entity{}, nil, nil, err
	}
	recovers, err := QueryEdges(db, "SELECT source_id, target_id, relation_type, COALESCE(weight, 1.0) FROM edges WHERE source_id = ? AND relation_type = ?", id, schema.RelationRecovery)
	if err != nil {
		return core.Entity{}, nil, nil, err
	}
	return e, blocked, recovers, nil
}

// GetTaskByID returns a task entity by ID.
func GetTaskByID(db *sql.DB, schema core.SchemaConfig, id string) (core.Entity, error) {
	var e core.Entity
	catPH, catArgs := BoolMapInClause(schema.StatefulCategories)
	args := append([]interface{}{id}, catArgs...)
	err := db.QueryRow("SELECT id, category, content, COALESCE(status, '') AS status, updated_at FROM entities WHERE id = ? AND category IN ("+catPH+")", args...).Scan(&e.ID, &e.Category, &e.Content, &e.Status, &e.UpdatedAt)
	if err == sql.ErrNoRows {
		return core.Entity{}, fmt.Errorf("task not found: %s", id)
	}
	if err != nil {
		return core.Entity{}, fmt.Errorf("get task: %w", err)
	}
	return e, nil
}

// GetTasksByIDs returns a map of task entities for the given IDs.
func GetTasksByIDs(db *sql.DB, schema core.SchemaConfig, ids []string) (map[string]core.Entity, error) {
	if len(ids) == 0 {
		return map[string]core.Entity{}, nil
	}
	phs, args := InClauseArgs(ids)
	catPH, catArgs := BoolMapInClause(schema.StatefulCategories)
	args = append(args, catArgs...)
	query := "SELECT id, category, content, COALESCE(status, '') AS status, updated_at FROM entities WHERE id IN (" + phs + ") AND category IN (" + catPH + ")"
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("get tasks by ids: %w", err)
	}
	defer rows.Close()
	out := make(map[string]core.Entity)
	for rows.Next() {
		var e core.Entity
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

// GetBlockedBy returns edges of type blocked_by where target_id = id.
func GetBlockedBy(db *sql.DB, schema core.SchemaConfig, id string) ([]core.Edge, error) {
	return QueryEdges(db, "SELECT source_id, target_id, relation_type, COALESCE(weight, 1.0) FROM edges WHERE target_id = ? AND relation_type = ?", id, schema.RelationBlocking)
}

// GetRecoversVia returns edges of type recovers_via where target_id = id.
func GetRecoversVia(db *sql.DB, schema core.SchemaConfig, id string) ([]core.Edge, error) {
	return QueryEdges(db, "SELECT source_id, target_id, relation_type, COALESCE(weight, 1.0) FROM edges WHERE target_id = ? AND relation_type = ?", id, schema.RelationRecovery)
}

// GetRootTasks returns tasks that have no blocked_by edges.
func GetRootTasks(db *sql.DB, schema core.SchemaConfig) ([]core.Entity, error) {
	catPH, catArgs := BoolMapInClause(schema.StatefulCategories)
	if catPH == "" {
		return []core.Entity{}, nil
	}
	query := `SELECT e.id, e.category, e.content, COALESCE(e.status, '') AS status, e.updated_at, COALESCE(e.priority, 0) FROM entities e WHERE e.category IN (` + catPH + `) AND e.archived = 0 AND NOT EXISTS (SELECT 1 FROM edges WHERE target_id = e.id AND relation_type = ?)`
	args := append(catArgs, schema.RelationBlocking)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("get root tasks: %w", err)
	}
	defer rows.Close()
	return ScanTaskEntities(rows)
}

// GetTaskTree builds a tree of tasks starting from rootID.
func GetTaskTree(db *sql.DB, schema core.SchemaConfig, rootID string) ([]*core.TreeNode, error) {
	if rootID != "" {
		if _, err := GetTaskByID(db, schema, rootID); err != nil {
			return nil, err
		}
		node, err := BuildNode(db, schema, rootID, nil)
		if err != nil {
			return nil, err
		}
		return []*core.TreeNode{node}, nil
	}
	roots, err := GetRootTasks(db, schema)
	if err != nil {
		return nil, err
	}
	var out []*core.TreeNode
	for _, root := range roots {
		node, err := BuildNode(db, schema, root.ID, nil)
		if err != nil {
			return nil, err
		}
		out = append(out, node)
	}
	return out, nil
}

// BuildNode iteratively walks the task subtree rooted at id using a work-
// stack (DFS pre-order) and returns a *core.TreeNode.
//
// Stays compatible with the recursive signature so callers don't change,
// but actually walks the tree iteratively so deeply-nested dependency
// graphs no longer risk Go runtime-stack overflow. The cycle sentinel
// "(cycle)" is emitted on revisited nodes — same observability as the
// recursive version.
//
// kidIDs are sorted by source_id so child order is stable across runs;
// Go map iteration over edges is randomized and the prior recursive
// version inherited that variance.
func BuildNode(db *sql.DB, schema core.SchemaConfig, id string, visited map[string]bool) (*core.TreeNode, error) {
	if visited == nil {
		visited = make(map[string]bool)
	}
	// Pre-flight cycle check: a caller that pre-marks `id` in visited
	// (e.g. a previous iteration's BuildNode) expects the recursive
	// sentinel shape — *TreeNode{ID: id, Content: "(cycle)", Status: "cycle"}.
	// Doing the check BEFORE the GetTaskByID call preserves that contract
	// for tests like TestBuildNode_CycleAvoidedWithMarker.
	if visited[id] {
		return &core.TreeNode{ID: id, Content: "(cycle)", Status: "cycle"}, nil
	}

	type frame struct {
		tree  *core.TreeNode
		kids  []string // blocked_by child source IDs (sorted)
		kidIx int      // next kid to process
	}

	rootEntity, err := GetTaskByID(db, schema, id)
	if err != nil {
		return nil, err
	}
	visited[id] = true

	rootBlocked, err := GetBlockedBy(db, schema, id)
	if err != nil {
		return nil, err
	}
	root := &core.TreeNode{ID: rootEntity.ID, Content: rootEntity.Content, Status: rootEntity.Status}
	stack := []frame{
		{tree: root, kids: blockedEdgesToSourceIDs(rootBlocked), kidIx: 0},
	}

	for len(stack) > 0 {
		// Peek at the top — only pop when current frame's kids are drained.
		// This mirrors DFS pre-order: assemble the parent first, then
		// descend into each child so the rendered tree reads top-down.
		top := &stack[len(stack)-1]
		if top.kidIx >= len(top.kids) {
			stack = stack[:len(stack)-1]
			continue
		}
		cid := top.kids[top.kidIx]
		top.kidIx++

		if visited[cid] {
			// Cycle sentinel — exact same shape the recursive version
			// used so existing tooling (rendering, CLI output) still
			// recognises it.
			top.tree.Children = append(top.tree.Children, &core.TreeNode{
				ID: cid, Content: "(cycle)", Status: "cycle",
			})
			continue
		}
		visited[cid] = true

		e, err := GetTaskByID(db, schema, cid)
		if err != nil {
			return nil, err
		}
		childNode := &core.TreeNode{ID: e.ID, Content: e.Content, Status: e.Status}
		top.tree.Children = append(top.tree.Children, childNode)

		childBlocked, err := GetBlockedBy(db, schema, cid)
		if err != nil {
			return nil, err
		}
		stack = append(stack, frame{
			tree:  childNode,
			kids:  blockedEdgesToSourceIDs(childBlocked),
			kidIx: 0,
		})
	}
	return root, nil
}

// blockedEdgesToSourceIDs returns the source_id of each edge in
// deterministic (sorted) order so BuildNode's iteration is reproducible.
func blockedEdgesToSourceIDs(edges []core.Edge) []string {
	out := make([]string, 0, len(edges))
	for _, e := range edges {
		out = append(out, e.SourceID)
	}
	sort.Strings(out)
	return out
}

// ScanTaskEntities scans rows into an Entity slice.
func ScanTaskEntities(rows *sql.Rows) ([]core.Entity, error) {
	var tasks []core.Entity
	for rows.Next() {
		var e core.Entity
		var priority sql.NullInt64
		if err := rows.Scan(&e.ID, &e.Category, &e.Content, &e.Status, &e.UpdatedAt, &priority); err != nil {
			return nil, fmt.Errorf("scan task entity: %w", err)
		}
		if priority.Valid {
			e.Priority = int(priority.Int64)
		}
		tasks = append(tasks, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tasks: %w", err)
	}
	return tasks, nil
}

// RenderTaskTree returns a human-readable tree representation.
func RenderTaskTree(nodes []*core.TreeNode, prefix string) string {
	var sb strings.Builder
	for i, node := range nodes {
		status := ""
		if node.Status != "" && node.Status != "pending" {
			status = fmt.Sprintf(" (%s)", node.Status)
		}
		fmt.Fprintf(&sb, "%s[%s] %s%s\n", prefix, node.ID, node.Content, status)
		for _, child := range node.Children {
			childPrefix := prefix
			if i == len(nodes)-1 {
				childPrefix += "    "
			} else {
				childPrefix += "│   "
			}
			sb.WriteString(RenderTaskTree([]*core.TreeNode{child}, childPrefix))
		}
	}
	return sb.String()
}
