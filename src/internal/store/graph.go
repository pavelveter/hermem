package store

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/pavelveter/hermem/pkg/domain"
)

// GetContradictions returns contradicts edges, optionally filtered by entity ID.
func GetContradictions(db *sql.DB, entityID string) ([]domain.ContradictionPair, error) {
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
	out := make([]domain.ContradictionPair, 0)
	for rows.Next() {
		var p domain.ContradictionPair
		if err := rows.Scan(&p.SourceID, &p.SourceContent, &p.TargetID, &p.TargetContent); err != nil {
			return nil, fmt.Errorf("scan contradiction: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetEntitiesByProvenance returns entities matching at least one provenance filter.
func GetEntitiesByProvenance(db *sql.DB, conversationID, messageID, source string, limit int) ([]domain.Entity, error) {
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
	out := make([]domain.Entity, 0)
	for rows.Next() {
		var e domain.Entity
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
	return out, rows.Err()
}

// FindConnectedComponents finds all connected components via BFS.
//
// Entities are addressed by a dense integer index and edges live in a single
// CSR adjacency array. The previous map[string][]string layout allocated a
// slice header per node plus a map bucket per insert and rehashed the visited
// set on every neighbor: 8.9ms and 125k allocations for 5k nodes. Walking
// int32 indices over one flat array removes all of that.
//
// Every entity is indexed (not just live ones) so an edge through an archived
// entity still joins the components it always joined; archived entities are
// merely never used as a BFS seed, which matches the previous behaviour.
func FindConnectedComponents(db *sql.DB, minSize int) ([]domain.ConnectedComponent, error) {
	rows, err := db.Query(`SELECT id, archived FROM entities`)
	if err != nil {
		return nil, fmt.Errorf("find components: read entities: %w", err)
	}
	ids := make([]string, 0, 256)
	live := make([]bool, 0, 256)
	pos := make(map[string]int32, 256)
	var archived int
	for rows.Next() {
		var id string
		if err := rows.Scan(&id, &archived); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("find components: scan entity: %w", err)
		}
		pos[id] = int32(len(ids))
		ids = append(ids, id)
		live = append(live, archived == 0)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("find components: iterate entities: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("find components: close entities: %w", err)
	}

	degree := make([]int32, len(ids))
	edgeRows, err := db.Query(`SELECT source_id, target_id FROM edges`)
	if err != nil {
		return nil, fmt.Errorf("find components: read edges: %w", err)
	}
	var endpoint []int32 // pairs of indices: endpoint[2i], endpoint[2i+1]
	for edgeRows.Next() {
		var src, dst string
		if err := edgeRows.Scan(&src, &dst); err != nil {
			_ = edgeRows.Close()
			return nil, fmt.Errorf("find components: scan edge: %w", err)
		}
		s, sok := pos[src]
		d, dok := pos[dst]
		if !sok || !dok {
			continue
		}
		degree[s]++
		degree[d]++
		endpoint = append(endpoint, s, d)
	}
	if err := edgeRows.Err(); err != nil {
		_ = edgeRows.Close()
		return nil, fmt.Errorf("find components: iterate edges: %w", err)
	}
	if err := edgeRows.Close(); err != nil {
		return nil, fmt.Errorf("find components: close edges: %w", err)
	}

	offset := make([]int32, len(ids)+1)
	for i, deg := range degree {
		offset[i+1] = offset[i] + deg
	}
	adj := make([]int32, offset[len(ids)])
	cursor := make([]int32, len(ids))
	for i := 0; i+1 < len(endpoint); i += 2 {
		s, d := endpoint[i], endpoint[i+1]
		adj[offset[s]+cursor[s]] = d
		cursor[s]++
		adj[offset[d]+cursor[d]] = s
		cursor[d]++
	}

	visited := make([]bool, len(ids))
	queue := make([]int32, 0, len(ids))
	compIdx := make([]int32, 0, 64)
	components := make([]domain.ConnectedComponent, 0, 16)
	for seed := range ids {
		if !live[seed] || visited[seed] {
			continue
		}
		queue = append(queue[:0], int32(seed))
		compIdx = compIdx[:0]
		visited[seed] = true
		totalDegree := 0
		for head := 0; head < len(queue); head++ {
			cur := queue[head]
			compIdx = append(compIdx, cur)
			totalDegree += int(degree[cur])
			for _, nb := range adj[offset[cur]:offset[cur+1]] {
				if !visited[nb] {
					visited[nb] = true
					queue = append(queue, nb)
				}
			}
		}
		if len(compIdx) >= minSize {
			comp := make([]string, len(compIdx))
			for i, idx := range compIdx {
				comp[i] = ids[idx]
			}
			components = append(components, domain.ConnectedComponent{
				IDs:       comp,
				Size:      len(comp),
				AvgDegree: float64(totalDegree) / float64(len(comp)),
			})
		}
	}
	sort.Slice(components, func(i, j int) bool { return components[i].Size > components[j].Size })
	return components, nil
}
