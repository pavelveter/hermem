package domain

import "time"

// TimePtr returns a pointer to t. Convenience helper for constructing
// *time.Time fields in struct literals.
func TimePtr(t time.Time) *time.Time { return &t }
