// Package store — EntityLocker: striped mutex pool keyed on FNV32(entityID).
//
// Why this exists: SQLite serialises ALL writers at the file level, so two
// goroutines writing intersecting batches inevitably land on SQLITE_BUSY.
// Existing `ResilientClient` retries protect the network from transient
// upstreams but cannot defend against internal lock contention from
// parallel ingest goroutines. An in-process mutex pool keyed on the target
// entity ID (the same graph mutations AFTER the DB write, not BEFORE) lets
// goroutines working on disjoint subgraphs proceed in parallel while still
// serialising concurrent writes to the same entity.
//
// Lock acquisition ORDER: callers MUST lex-sort the entity IDs in a batch
// BEFORE acquiring locks. Without a global order, two goroutines locking
// {A,B} and {B,A} respectively deadlock once both hold one of the two
// mutexes and wait for the other.
//
// Construction round-up: shardCount is rounded up to the nearest power of
// two inside NewEntityLocker so the FNV32 modulo becomes a bitwise AND
// (`h.Sum32() & shardMask`), shaving a division per acquire. The default
// is 32 — enough parallelism for the current 50k-vec corpus without
// exhausting the Go runtime's goroutine stack space.
package store

import (
	"hash/fnv"
	"sort"
	"sync"
)

// shard is the granularity of contention: one sync.Mutex per shard.
// Each Lock/Unlock call lands on exactly one shard, so concurrent
// acquisitions on disjoint entity IDs never block.
type shard struct {
	mu sync.Mutex
}

// EntityLocker is the public type. Construct via NewEntityLocker; do NOT
// embed directly because shardCount rounding relies on the constructor.
type EntityLocker struct {
	shards    []*shard
	shardMask uint32
}

// NewEntityLocker allocates `shardCount` mutex shards. shardCount is
// rounded UP to the nearest power of two for fast bitwise masking
// (`h.Sum32() & shardMask`); non-positive values default to 32.
//
// Upper bound: shardCount above 1<<31 is CLAMPED — the bit-trick below
// uses `uint32(shardCount - 1)` which silently truncates on 64-bit Go
// when callers pass absurd inputs (e.g. 1<<33). The clamp keeps the
// pre-cast value within uint32 range so the shift-OR proceeds without
// losing bits. Document this so future readers don't enlarge the upper
// bound expecting the bit-trick to scale.
//
// Rounding policy matches the out.txt § 3.2 spirit (power-of-two shard
// count) and the user's existing test fixture:
//
//	shardCount=0/-5       → 32 (default)
//	shardCount=7          → 8      (next power of two >= in)
//	shardCount=31         → 32
//	shardCount=100        → 128
//	shardCount > (1<<31) → (1<<31) (clamped to uint32 range)
//
// Bit-trick for `nextPow2(n)` (where `n` is positive): shift-and-or
// until all low bits are set, then add 1. Safe after the upper-bound
// clamp because the intermediate fits uint32.
func NewEntityLocker(shardCount int) *EntityLocker {
	if shardCount <= 0 {
		shardCount = 32
	}
	if shardCount > (1 << 31) {
		shardCount = 1 << 31
	}
	if shardCount&(shardCount-1) != 0 {
		n := uint32(shardCount - 1)
		n |= n >> 1
		n |= n >> 2
		n |= n >> 4
		n |= n >> 8
		n |= n >> 16
		shardCount = int(n) + 1
	}
	l := &EntityLocker{
		shards:    make([]*shard, shardCount),
		shardMask: uint32(shardCount - 1),
	}
	for i := 0; i < shardCount; i++ {
		l.shards[i] = &shard{}
	}
	return l
}

// getShard returns the shard corresponding to entityID. The FNV32 hash
// distributes entity IDs across shards uniformly for the natural input
// shape (UUID-shaped strings); pathological inputs collide predictably
// and contend on the same shard, which is the desired outcome (per-key
// serialisation, not per-ID-and-prefix serialisation).
func (l *EntityLocker) getShard(entityID string) *shard {
	h := fnv.New32a()
	_, _ = h.Write([]byte(entityID))
	return l.shards[h.Sum32()&l.shardMask]
}

// Lock acquires the shard mutex for entityID. Always pair with Unlock
// (defer Unlock immediately after Lock to survive panics). Multi-id
// acquisition MUST go through AcquireBatch to avoid ABBA deadlocks.
func (l *EntityLocker) Lock(entityID string) {
	l.getShard(entityID).mu.Lock()
}

// Unlock releases the shard mutex for entityID. Unlocking an entity ID
// that was not previously Lock-ed panics on the inner sync.Mutex; that's
// intentional — misuse here would silently leave locks held.
func (l *EntityLocker) Unlock(entityID string) {
	l.getShard(entityID).mu.Unlock()
} // AcquireBatch locks a sorted set of UNIQUE entity IDs in one call.
// Idempotent for empty batches. Returns a function that releases all
// locks in the REVERSE order — release ordering doesn't affect
// correctness here (Go mutex is non-reentrant, so we MUST lock each
// distinct ID at most once per AcquireBatch call) but symmetric
// teardown minimizes wrong-order dropouts in trace logs.
//
// Duplicate ID semantics: callers may pass slices with the same ID
// twice (e.g. an ingest batch that dedup-collides on itself). Locking
// the SAME shard twice without an intervening Unlock is a deadlock
// with sync.Mutex (which is non-reentrant). We dedup BEFORE locking,
// preserving lex order of first-occurrence.
//
// Used by IngestionWorker.IngestSynchronized (TODO § 3.2 followup):
// before `BeginTx` on a batch, lock the sorted unique IDs; release on
// commit OR rollback; the lock window is bounded by the tx latency.
func (l *EntityLocker) AcquireBatch(entityIDs []string) func() {
	if len(entityIDs) == 0 {
		return func() {}
	}
	// Dedup BEFORE sorting so we don't lock a shard twice — sync.Mutex
	// is non-reentrant so a duplicate Lock against the same id would
	// block forever. map[string]struct{} is the canonical dedup set.
	seen := make(map[string]struct{}, len(entityIDs))
	unique := make([]string, 0, len(entityIDs))
	for _, id := range entityIDs {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	sort.Strings(unique)
	for _, id := range unique {
		l.Lock(id)
	}
	return func() {
		for i := len(unique) - 1; i >= 0; i-- {
			l.Unlock(unique[i])
		}
	}
}
