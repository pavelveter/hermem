package config

import (
	"sync/atomic"
	"unsafe"
)

// AtomicConfig provides lock-free, atomic config replacement.
// Reads return an immutable snapshot; writes swap the pointer atomically.
// Configuration updates are expected to be rare (SIGHUP, admin API).
//
// Usage:
//
//	ac := config.NewAtomicConfig(initialCfg)
//	cfg := ac.Load()  // lock-free read
//	ac.Store(newCfg)  // atomic replacement
type AtomicConfig struct {
	ptr unsafe.Pointer // *Config
}

// NewAtomicConfig wraps cfg in an AtomicConfig.
func NewAtomicConfig(cfg *Config) *AtomicConfig {
	return &AtomicConfig{ptr: unsafe.Pointer(cfg)}
}

// Load returns the current immutable config snapshot. Lock-free.
func (ac *AtomicConfig) Load() *Config {
	return (*Config)(atomic.LoadPointer(&ac.ptr))
}

// Store replaces the config atomically. Subsequent Load calls return
// the new snapshot. Callers must not mutate the old snapshot after Store.
func (ac *AtomicConfig) Store(cfg *Config) {
	atomic.StorePointer(&ac.ptr, unsafe.Pointer(cfg))
}

// Swap atomically replaces the config and returns the previous snapshot.
func (ac *AtomicConfig) Swap(cfg *Config) *Config {
	old := (*Config)(atomic.SwapPointer(&ac.ptr, unsafe.Pointer(cfg)))
	return old
}
