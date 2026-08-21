// Package fsutil consolidates filesystem helpers that are shared by
// multiple internal/ packages but are not part of the public API.
//
// The first (and currently only) exported entry is WriteOwnerOnly, which
// writes a file at mode 0o600 AND issues a post-WriteFile os.Chmod(0o600)
// so legacy 0o644 files are actively narrowed to 0o600 on the next
// mutation. Two call sites — src/internal/config/update.go writeConfig
// and src/internal/ingestion/checkpoint.go writeOwnerOnly — currently
// keep package-local duplicates with the same body. They are kept until
// a third internal/ package needs the same primitive, at which point
// both duplicates migrate to this package.
//
// See the MIGRATION TRIGGER block in WriteOwnerOnly's godoc for the
// exact moment to consolidate, and why cross-package primitive
// exposure is intentionally deferred until that trigger fires. The
// trigger scope is "anywhere in the codebase" — not strictly
// internal/ — so SDK callers, test fixtures, and future generators
// all count.
package fsutil
