// Ref-and-atomic-wrapper side of the serverstate package.
//
// The State struct + New() constructor live in state.go. This file holds
// only the *Ref type — a thread-safe atomic.Pointer[State] wrapper used by
// every service to read a consistent snapshot of dynamic server config,
// and by the standalone server to swap that snapshot atomically on SIGHUP.
//
// The Generation stamp (State.Generation) is what makes cross-state
// transactions safe under concurrent SIGHUP. Ref owns the gen counter
// (atomic.Uint64); every Store bumps it and writes the new value into the
// incoming State before the atomic.Pointer swap. Handlers that captured
// state.Generation at request start re-read Load().Generation immediately
// before committing a write; a mismatch means the schema swapped out from
// under them and they must return 409 Conflict.
package serverstate

import "sync/atomic"

// Ref is a thread-safe holder for *State. Always pass *Ref by pointer —
// the embedded atomic.Pointer[State] is no-copy and must not be copied.
//
// All reads of dynamic config must go through Ref.Load(). All writes must
// go through Ref.Store(). Storing a non-nil *State is required; passing nil
// is a programmer error and will panic on the next Load (or Store).
type Ref struct {
	ptr atomic.Pointer[State]
	gen atomic.Uint64
}

// NewRef wraps an initial State. Returns a *Ref. Stamps the State with
// Generation = 1 (skip 0 so tests that construct a State by hand still
// detect a swap against the post-NewRef baseline).
func NewRef(s *State) *Ref {
	if s == nil {
		panic("serverstate: NewRef called with nil State")
	}
	r := &Ref{}
	r.gen.Store(1)
	s.Generation = r.gen.Load()
	r.ptr.Store(s)
	return r
}

// Load returns the current State. Always non-nil after NewRef.
func (r *Ref) Load() *State {
	return r.ptr.Load()
}

// Store atomically replaces the State and stamps the incoming State with
// the next Generation (Ref's atomic counter is incremented before the
// pointer swap, so a concurrent reader either sees the old State with the
// old Generation or the new State with the new one — never an inconsistent
// mix). The incoming State is mutated in place (its Generation field is
// overwritten with the new stamp); callers must NOT retain a pointer to
// s and re-read its Generation after Store and expect the pre-stamp value.
// nil is rejected to prevent Load() panic.
func (r *Ref) Store(s *State) {
	if s == nil {
		panic("serverstate: Ref.Store called with nil State")
	}
	s.Generation = r.gen.Add(1)
	r.ptr.Store(s)
}

// IsStale reports whether the global config has been swapped since the
// caller captured generation `gen`. Callers (HTTP handlers, batch jobs)
// capture state.Generation at request / decision start, then re-check
// here immediately before committing any write derived from that snapshot.
// Returns true means a SIGHUP replaced the State; the caller must abort
// the write and return 409 Conflict so the client retries against fresh
// config. Returns false means the snapshot is still authoritative.
func (r *Ref) IsStale(gen uint64) bool {
	return r.Load().Generation != gen
}
