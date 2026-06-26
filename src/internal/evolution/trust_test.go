package evolution

import (
	"testing"
	"time"
)

func TestTrustScore_DefaultWeights(t *testing.T) {
	w := TrustDefaults()
	now := time.Now().UTC()
	ts := TrustScore(1.0, "user", now, w)
	if roundTo(ts, 4) != 1.0 {
		t.Errorf("expected 1.0, got %f", ts)
	}
}

func TestTrustScore_LowerConfidence(t *testing.T) {
	w := TrustDefaults()
	ts := TrustScore(0.5, "user", time.Now().UTC(), w)
	if roundTo(ts, 4) != 0.5 {
		t.Errorf("expected 0.5, got %f", ts)
	}
}

func TestTrustScore_UnknownSource(t *testing.T) {
	w := TrustDefaults()
	ts := TrustScore(1.0, "unknown_source", time.Now().UTC(), w)
	if roundTo(ts, 4) != 0.5 {
		t.Errorf("expected 0.5 (default source weight), got %f", ts)
	}
}

func TestTrustScore_RecencyDecay(t *testing.T) {
	w := TrustDefaults()
	w.RecencyHalfLifeHours = 1 // fast decay
	old := time.Now().UTC().Add(-2 * time.Hour)
	ts := TrustScore(1.0, "user", old, w)
	if ts >= 1.0 {
		t.Errorf("expected decayed trust < 1.0, got %f", ts)
	}
	if ts <= 0 {
		t.Errorf("expected positive trust, got %f", ts)
	}
}

func TestTrustScore_ZeroUpdatedAt(t *testing.T) {
	w := TrustDefaults()
	ts := TrustScore(1.0, "user", time.Time{}, w)
	if ts != 1.0 {
		t.Errorf("expected 1.0 for zero time, got %f", ts)
	}
}

func BenchmarkTrustScore(b *testing.B) {
	w := TrustDefaults()
	now := time.Now().UTC()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		TrustScore(0.8, "user", now, w)
	}
}
