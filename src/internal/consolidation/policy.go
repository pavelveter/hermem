package consolidation

import "time"

// Policy controls the background consolidation ("graph sleep cycle").
type Policy struct {
	// Interval between automatic consolidation runs.
	Interval time.Duration
	// Max number of source nodes per synthesis cluster.
	ClusterSize int
	// Similarity threshold above which two nodes are considered redundant.
	DedupThreshold float32
}

// Default returns the production consolidation defaults.
func Default() Policy {
	return Policy{
		Interval:       24 * time.Hour,
		ClusterSize:    20,
		DedupThreshold: 0.88,
	}
}

// DefaultPolicy is an alias for Default.
func DefaultPolicy() Policy { return Default() }
