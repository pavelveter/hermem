package store

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/pavelveter/hermem/src/internal/core"
)

// GetContradictions returns contradicts edges, optionally filtered by entity ID.
func GetContradictions(db *sql.DB, entityID string) ([]core.ContradictionPair, error) {
	var rows *sql.Rows
	var err error
	if entityID != "" {
		rows, err = db.Query(`SELECT e1.id, e1.content, e2.id, e2.content FROM edges ed JOIN entities e1 ON e1.id = ed.source_id JOIN entities e2 ON e2.id = ed.target_id WHERE ed.relation_type = 'contradicts' AND e1.archived = 0 AND e2.archived = 0 AND (ed.source_id = ? OR ed.target_id = ?) ORDER BY e1.id`, entityID, entityID)
	} else {
		rows, err = db.Query(`SELECT e1.id, e1.content, e2.id, e2.content FROM edges ed JOIN entities e1 ON e1.id = ed.source_id JOIN entities e2 ON e2.id = ed.target_id WHERE ed.relation_type = 'contradicts' AND e1.archived = 0 AND e2.archived = 0 ORDER BY e1.id`)
	}
	if err != nil {
		return nil, fmt.Errorf("query contradictions: %w", err)
	}
	defer rows.Close()
	var out []core.ContradictionPair
	for rows.Next() {
		var p core.ContradictionPair
		if err := rows.Scan(&p.SourceID, &p.SourceContent, &p.TargetID, &p.TargetContent); err != nil {
			return nil, fmt.Errorf("scan contradiction: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetEntitiesByProvenance returns entities matching at least one provenance filter.
func GetEntitiesByProvenance(db *sql.DB, conversationID, messageID, source string, limit int) ([]core.Entity, error) {
	if conversationID == "" && messageID == "" && source == "" {
		return nil, fmt.Errorf("at least one provenance filter required")
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	var wheres []string
	var args []interface{}
	if conversationID != "" {
		wheres = append(wheres, "conversation_id = ?")
		args = append(args, conversationID)
	}
	if messageID != "" {
		wheres = append(wheres, "message_id = ?")
		args = append(args, messageID)
	}
	if source != "" {
		wheres = append(wheres, "source = ?")
		args = append(args, source)
	}
	query := `SELECT id, category, content, updated_at, conversation_id, message_id, source, source_type, created_at, confidence FROM entities WHERE archived = 0 AND (` + strings.Join(wheres, " OR ") + `) ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query provenance: %w", err)
	}
	defer rows.Close()
	var out []core.Entity
	for rows.Next() {
		var e core.Entity
		var convID, msgID, src, srcType sql.NullString
		var confidence sql.NullFloat64
		var createdAt sql.NullTime
		if err := rows.Scan(&e.ID, &e.Category, &e.Content, &e.UpdatedAt, &convID, &msgID, &src, &srcType, &createdAt, &confidence); err != nil {
			return nil, fmt.Errorf("scan provenance entity: %w", err)
		}
		if convID.Valid {
			e.ConversationID = convID.String
		}
		if msgID.Valid {
			e.MessageID = msgID.String
		}
		if src.Valid {
			e.Source = src.String
		}
		if srcType.Valid {
			e.SourceType = srcType.String
		}
		if createdAt.Valid {
			t := createdAt.Time
			e.CreatedAt = &t
		}
		if confidence.Valid {
			e.Confidence = float32(confidence.Float64)
		}
		out = append(out, e)
	}
	return core.NormalizeSlice(out), rows.Err()
}

// FindConnectedComponents finds all connected components via BFS.
func FindConnectedComponents(db *sql.DB, minSize int) ([]core.ConnectedComponent, error) {
	rows, err := db.Query(`SELECT e.id FROM entities e WHERE e.archived = 0`)
	if err != nil {
		return nil, fmt.Errorf("find components: read entities: %w", err)
	}
	defer rows.Close()
	adj := make(map[string][]string)
	var allIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("find components: scan entity: %w", err)
		}
		allIDs = append(allIDs, id)
		adj[id] = nil
	}
	edgeRows, err := db.Query(`SELECT source_id, target_id FROM edges`)
	if err != nil {
		return nil, fmt.Errorf("find components: read edges: %w", err)
	}
	defer edgeRows.Close()
	for edgeRows.Next() {
		var src, dst string
		if err := edgeRows.Scan(&src, &dst); err != nil {
			return nil, fmt.Errorf("find components: scan edge: %w", err)
		}
		adj[src] = append(adj[src], dst)
		adj[dst] = append(adj[dst], src)
	}
	visited := make(map[string]bool)
	var components []core.ConnectedComponent
	for _, id := range allIDs {
		if visited[id] {
			continue
		}
		queue := []string{id}
		visited[id] = true
		comp := []string{}
		totalDegree := 0
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			comp = append(comp, cur)
			totalDegree += len(adj[cur])
			for _, nb := range adj[cur] {
				if !visited[nb] {
					visited[nb] = true
					queue = append(queue, nb)
				}
			}
		}
		if len(comp) >= minSize {
			avgDeg := float64(totalDegree) / float64(len(comp))
			components = append(components, core.ConnectedComponent{IDs: comp, Size: len(comp), AvgDegree: avgDeg})
		}
	}
	sort.Slice(components, func(i, j int) bool { return components[i].Size > components[j].Size })
	return components, nil
}
