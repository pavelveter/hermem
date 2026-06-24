// Package graph holds graph-shaped helpers that don't fit cleanly under
// store/ or retrieval/ — currently provenance lineage + nil-safe
// source rendering, used by both the CLI's graph commands and the
// server's response formatters.
package graph

import (
	"time"

	"github.com/pavelveter/hermem/src/internal/core"
)

// Source mirrors the metadata a deleted-from-the-DB provenance row
// would have carried. We accept *Source rather than a value so callers
// can pass a known-missing lookup without an additional "exists" flag.
type Source struct {
	ID    string
	Label string
}

// SafeSourceLabel is nil-safe + empty-safe. Returns the canonical
// "unknown_or_deleted_source" sentinel when:
//
//   - s is nil (the source pointer was never populated, e.g. the DB
//     lookup returned sql.ErrNoRows and the caller did NOT propagate
//     the error), OR
//   - s.ID is empty (the source row existed but lacked a primary key,
//     which smells like a deleted / archived record).
//
// Without this guard, dereferencing nil Source.ID or rendering "" in
// appendChainLineage / RenderTaskTree surfaces as a runtime panic at
// the worst possible moment — usually inside a CLI command mid-flush.
func SafeSourceLabel(s *Source) string {
	if s == nil || s.ID == "" {
		return "unknown_or_deleted_source"
	}
	return s.Label
}

// LineageEntry is the rendered shape of one provenance lineage step.
// FactID is the entity whose lineage we're tracing; SourceTag is the
// human-readable tag returned by SafeSourceLabel; CreatedAt is the
// entity's UpdatedAt at the time of recording.
type LineageEntry struct {
	FactID    string    `json:"fact_id"`
	SourceTag string    `json:"source_tag"`
	CreatedAt time.Time `json:"created_at"`
}

// WalkLineage flattens a slice of nodes into one LineageEntry per node.
// Deleted sources map through SafeSourceLabel so the output never breaks
// on a missing provenance row.
//
// Time complexity: O(n). Memory cost: one LineageEntry per input node —
// pre-allocated to len(nodes) so the loop never reallocates the slice.
func WalkLineage(nodes []core.Entity) []LineageEntry {
	out := make([]LineageEntry, 0, len(nodes))
	for _, n := range nodes {
		tag := SafeSourceLabel(&Source{ID: n.Source, Label: n.Source})
		out = append(out, LineageEntry{
			FactID:    n.ID,
			SourceTag: tag,
			CreatedAt: n.UpdatedAt,
		})
	}
	return out
}
