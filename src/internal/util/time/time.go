// Package time provides UTC-aware time helpers used across the codebase.
//
// All temporal data flowing through hermem should be expressed in UTC to
// avoid TZ drift between the host clock, container clock, and the SQLite
// DATABASE — a server started under one TZ and migrated to another would
// see "fact from yesterday" rendered as "fact from today" otherwise.
package time

import stdtime "time"

// NowUTCUnix returns the current wall-clock time as Unix seconds in UTC.
//
// Use this anywhere a fresh "now" value is captured for storage or
// temporal-comparison purposes. Existing callers passing time.Now()
// directly should migrate to this helper so the TZ semantics are
// visible at the call site rather than implicit in the storage layout.
func NowUTCUnix() int64 {
	return stdtime.Now().UTC().Unix()
}
