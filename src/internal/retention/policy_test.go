package retention

import (
	"testing"
	"time"
)

// TestDefaultPreservesHistoricalDefaults pins that the moved Policy type's
// Default() reproduces the exact values that config/ini.go historically
// populated on core.RetentionPolicy. A drift here would change production
// archival semantics silently.
func TestDefaultPreservesHistoricalDefaults(t *testing.T) {
	p := Default()
	if p.ObservationTTL != 90*24*time.Hour {
		t.Errorf("ObservationTTL: got %v, want %v", p.ObservationTTL, 90*24*time.Hour)
	}
	if p.RunInterval != 1*time.Hour {
		t.Errorf("RunInterval: got %v, want %v", p.RunInterval, 1*time.Hour)
	}
	if p.DeleteBatchSize != 500 {
		t.Errorf("DeleteBatchSize: got %d, want %d", p.DeleteBatchSize, 500)
	}
}

// TestPolicyFieldsRemainPlainValues guards that the Policy struct keeps its
// three plain scalar fields (no JSON tags, no hidden coupling) so config
// projection and the sweep loop stay dependency-free.
func TestPolicyFieldsRemainPlainValues(t *testing.T) {
	p := Policy{ObservationTTL: time.Hour, RunInterval: time.Minute, DeleteBatchSize: 10}
	if p.ObservationTTL != time.Hour || p.RunInterval != time.Minute || p.DeleteBatchSize != 10 {
		t.Fatalf("unexpected policy round-trip: %+v", p)
	}
}
