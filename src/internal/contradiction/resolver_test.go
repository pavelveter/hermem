package contradiction

import (
	"testing"

	"github.com/pavelveter/hermem/src/internal/core"
)

func TestThresholdResolver_DefaultKeepsHighConfidence(t *testing.T) {
	r := &ThresholdResolver{}
	existing := core.Entity{Confidence: 1.0}
	action := r.Resolve(existing, core.ExtractedEntity{Content: "new"})
	if action != ActionKeepBoth {
		t.Fatalf("want ActionKeepBoth, got %v", action)
	}
}

func TestThresholdResolver_DefaultArchivesLowConfidence(t *testing.T) {
	r := &ThresholdResolver{}
	existing := core.Entity{Confidence: 0.3}
	action := r.Resolve(existing, core.ExtractedEntity{Content: "new"})
	if action != ActionPreferIncoming {
		t.Fatalf("want ActionPreferIncoming, got %v", action)
	}
}

func TestThresholdResolver_ZeroConfidenceTreatedAsOne(t *testing.T) {
	r := &ThresholdResolver{}
	existing := core.Entity{Confidence: 0} // zero → treated as 1.0
	action := r.Resolve(existing, core.ExtractedEntity{Content: "new"})
	if action != ActionKeepBoth {
		t.Fatalf("want ActionKeepBoth for zero confidence, got %v", action)
	}
}

func TestThresholdResolver_CustomThreshold(t *testing.T) {
	r := &ThresholdResolver{Threshold: 0.5}
	// confidence exactly at threshold → keep both
	action := r.Resolve(core.Entity{Confidence: 0.5}, core.ExtractedEntity{})
	if action != ActionKeepBoth {
		t.Fatalf("at threshold: want ActionKeepBoth, got %v", action)
	}
	// below threshold → prefer incoming
	action = r.Resolve(core.Entity{Confidence: 0.49}, core.ExtractedEntity{})
	if action != ActionPreferIncoming {
		t.Fatalf("below threshold: want ActionPreferIncoming, got %v", action)
	}
}
