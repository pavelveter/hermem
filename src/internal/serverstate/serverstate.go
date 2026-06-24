// Ref-and-atomic-wrapper side of the serverstate package.
//
// The State struct + New() constructor live in state.go. This file holds
// only the *Ref type — a thread-safe atomic.Pointer[State] wrapper used by
// every service to read a consistent snapshot of dynamic server config,
// and by the standalone server to swap that snapshot atomically on SIGHUP.
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
}

// NewRef wraps an initial State. Returns a *Ref.
func NewRef(s *State) *Ref {
	if s == nil {
		panic("serverstate: NewRef called with nil State")
	}
	r := &Ref{}
	r.ptr.Store(s)
	return r
}

// Load returns the current State. Always non-nil after NewRef.
func (r *Ref) Load() *State {
	return r.ptr.Load()
}

// Store atomically replaces the State. nil is rejected to prevent Load() panic.
func (r *Ref) Store(s *State) {
	if s == nil {
		panic("serverstate: Ref.Store called with nil State")
	}
	r.ptr.Store(s)
}
