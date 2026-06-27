// Package time provides UTC-aware time helpers used across the codebase.
//
// All temporal data flowing through hermem should be expressed in UTC to
// avoid TZ drift between the host clock, container clock, and the SQLite
// DATABASE — a server started under one TZ and migrated to another would
// see "fact from yesterday" rendered as "fact from today" otherwise. As
// of migration 013, schemas that previously stored DATETIME strings now
// store INTEGER Unix milliseconds (UTC), which is unambiguous at read
// time and sortable without TZ-aware library code.
package time

import stdtime "time"

// NowUTCUnix returns the current wall-clock time as Unix seconds in UTC.
//
// Use this anywhere a legacy column still expects a DATETIME-shaped
// value (seconds granularity). New schema columns (INTEGER unix ms)
// should call NowUTCUnixMillis instead so sub-second ordering is
// preserved.
func NowUTCUnix() int64 {
	return stdtime.Now().UTC().Unix()
}

// NowUTCUnixMillis returns the current wall-clock time as Unix
// milliseconds in UTC. Use this for INTEGER-typed schema columns
// (e.g. episodes.started_at_ms, events.timestamp_ms — see migration
// 013 for the introduced columns). The value is the result of
// time.Now().UTC().UnixMilli() so sub-second ordering is preserved
// and the writer-side TZ invariant (.UTC() first) is visible at the
// call site.
func NowUTCUnixMillis() int64 {
	return stdtime.Now().UTC().UnixMilli()
}

// UnixMillisFromTime converts t to Unix milliseconds in UTC. The
// returned value is the canonical storage representation for INTEGER
// ms columns. Pass any time.Time; the helper normalises to UTC
// before serialising so callers cannot accidentally persist a
// non-UTC value (the .UTC() call makes the TZ invariant part of the
// storage shape, not a convention callers must remember).
//
// Zero t yields zero ms — the same convention SQLite uses when an
// INTEGER column is INSERTed with no explicit value in nullable
// context. Callers that need to distinguish "absent" from "epoch"
// should pass an explicit sql.NullInt64 path instead.
func UnixMillisFromTime(t stdtime.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UTC().UnixMilli()
}

// TimeFromUnixMillis converts an INTEGER-ms schema value back to a
// time.Time in UTC. Zero ms yields the zero time.Time so the round
// trip is the inverse of UnixMillisFromTime(zero). Use this at read
// sites; the returned time.Time has the UTC Location set so callers
// passing it back through UnixMillisFromTime are stable under
// repeated round trips.
func TimeFromUnixMillis(ms int64) stdtime.Time {
	if ms == 0 {
		return stdtime.Time{}
	}
	return stdtime.UnixMilli(ms).UTC()
}
