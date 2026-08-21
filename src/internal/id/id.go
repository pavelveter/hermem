// Package id owns identity generation for server-minted identifiers.
//
// It exists so identity strategy has a single, explicit home outside the
// legacy core facade. ADR-035 owns the eventual upgrade path (UUIDv7/ULID
// and content-addressed entity IDs); until that lands, this package keeps
// the historical process-local counter behavior intact so the facade can be
// removed without changing runtime semantics.
package id

import (
	"fmt"
	"sync/atomic"
)

// taskSeq is a monotonic counter for unique task IDs within a process.
var taskSeq atomic.Uint64

// NewTaskID returns a unique task ID using an atomic counter.
// Guaranteed unique within a process — no collision under concurrency.
func NewTaskID() string {
	return fmt.Sprintf("task-%d", taskSeq.Add(1))
}
