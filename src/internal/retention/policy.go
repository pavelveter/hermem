package retention

import "time"

// Policy controls automatic archival of stale nodes.
type Policy struct {
	ObservationTTL  time.Duration
	RunInterval     time.Duration
	DeleteBatchSize int
}

// Default returns the production retention defaults. It is the single
// source of truth for the default values; config/ini.go uses it as the
// boot default.
func Default() Policy {
	return Policy{
		ObservationTTL:  90 * 24 * time.Hour,
		RunInterval:     1 * time.Hour,
		DeleteBatchSize: 500,
	}
}
