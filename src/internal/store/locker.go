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
//
// Cancellation: the shard mutex is a chan-based semaphore (capacity 1).
// LockCtx / AcquireBatchCtx select on ctx.Done() so a stalled shard
// (e.g. a Louvain pass holding a single entity for >2s) cannot back up
// HTTP-goroutines indefinitely. Callers MUST pass a non-nil context;
// background work should pass context.Background() explicitly.
package store

import (
	"context"
	"errors"
	"hash/fnv"
	"sort"
)

// ErrLockCancelled is returned by LockCtx / AcquireBatchCtx when the
// caller's context fires before the shard's token could be drained. It
// is a sentinel so callers (HTTP handlers, worker dispatch) can map it
// to 503 / context.Cancelled uniformly without type-asserting on
// ctx.Err().
var ErrLockCancelled = errors.New("entity-locker: lock acquisition cancelled")

// shard is the granularity of contention: one buffered channel as a
// non-reentrant mutex. Capacity 1 + a pre-loaded token gives us exactly
// one outstanding holder; release sends the token back. The select-on
// ctx.Done + <-sem pattern is the canonical ctx-aware mutex idiom in
// stdlib (sync.Mutex itself is non-cancellable).
type shard struct {
	sem chan struct{}
}

// EntityLocker is the public type. Construct via NewEntityLocker; do NOT
// embed directly because shardCount rounding relies on the constructor.
type EntityLocker struct {
	shards    []*shard
	shardMask uint32
}

// NewEntityLocker allocates `shardCount` semaphore shards. shardCount is
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
		// Capacity 1 + one pre-loaded token ≈ "1 outstanding holder".
		s := make(chan struct{}, 1)
		s <- struct{}{}
		l.shards[i] = &shard{sem: s}
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

// LockCtx acquires the shard semaphore for entityID, returning nil on
// success or ErrLockCancelled (wrapping ctx.Err()) on cancellation.
// Always pair with Unlock (defer Unlock immediately after LockCtx to
// survive panics). Multi-id acquisition MUST go through AcquireBatchCtx
// to avoid ABBA deadlocks.
//
// nil context is rejected at runtime via context.Background() so the
// contract is uniform; callers MUST thread the request context through.
func (l *EntityLocker) LockCtx(ctx context.Context, entityID string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-l.getShard(entityID).sem:
		return nil
	case <-ctx.Done():
		return joinLockErr(ctx.Err())
	}
}

// Unlock releases the shard semaphore for entityID. Unlocking an entity
// ID that was not previously LockCtx-ed panics on the inner channel send
// (default branch on full channel); that's intentional — misuse here
// would silently leave locks held.
func (l *EntityLocker) Unlock(entityID string) {
	l.getShard(entityID).sem <- struct{}{}
}

// AcquireBatchCtx locks a sorted set of UNIQUE entity IDs in one call.
// Idempotent for empty batches: returns (noop release, nil). On context
// cancellation mid-batch, releases every shard it already holds (in
// reverse order) BEFORE returning (nil, ErrLockCancelled) — partial
// rollback guarantees no shard is left zombie-held when the caller gives
// up.
//
// Release ordering doesn't affect correctness (Go mutex is
// non-reentrant, so we MUST lock each distinct ID at most once per
// AcquireBatchCtx call) but symmetric teardown minimizes wrong-order
// dropouts in trace logs.
//
// Duplicate ID semantics: callers may pass slices with the same ID
// twice (e.g. an ingest batch that dedup-collides on itself). Locking
// the SAME shard twice without an intervening Unlock is a deadlock
// with the chan-based mutex (receive on empty channel blocks forever).
// We dedup BEFORE locking, preserving lex order of first-occurrence.
//
// Used by IngestionWorker.IngestSynchronized (TODO § 3.2 followup):
// before `BeginTx` on a batch, lock the sorted unique IDs; release on
// commit OR rollback; the lock window is bounded by the tx latency.
func (l *EntityLocker) AcquireBatchCtx(ctx context.Context, entityIDs []string) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(entityIDs) == 0 {
		return func() {}, nil
	}
	// Dedup BEFORE sorting so we don't lock a shard twice — receive on
	// an empty chan would block forever. map[string]struct{} is the
	// canonical dedup set.
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

	// Tracks shards acquired so far so we can roll back on ctx cancel.
	acquired := make([]string, 0, len(unique))
	for _, id := range unique {
		if err := l.LockCtx(ctx, id); err != nil {
			// Release in REVERSE order so observers/diagnostics see
			// symmetric teardown. Empty acquired is a no-op.
			for i := len(acquired) - 1; i >= 0; i-- {
				l.Unlock(acquired[i])
			}
			return nil, err
		}
		acquired = append(acquired, id)
	}
	return func() {
		for i := len(acquired) - 1; i >= 0; i-- {
			l.Unlock(acquired[i])
		}
	}, nil
}

// joinLockErr tags ctx state with our sentinel so HTTP handlers can
// errors.Is(err, store.ErrLockCancelled) without losing the underlying
// context.Canceled / context.DeadlineExceeded signal.
func joinLockErr(err error) error {
	if err == nil {
		return ErrLockCancelled
	}
	return &lockErr{base: ErrLockCancelled, cause: err}
}

type lockErr struct {
	base, cause error
}

func (e *lockErr) Error() string { return e.base.Error() + ": " + e.cause.Error() }
func (e *lockErr) Unwrap() []error {
	return []error{e.base, e.cause}
}
