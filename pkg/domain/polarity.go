package domain

// Polarity represents whether evidence supports or refutes a belief.
type Polarity string

const (
	PolaritySupport Polarity = "support"
	PolarityRefute  Polarity = "refute"
)
