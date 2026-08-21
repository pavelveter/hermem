package contradiction

import (
	"testing"

	"github.com/pavelveter/hermem/pkg/domain"
)

func TestThresholdResolver_DefaultKeepsHighConfidence(t *testing.T) {
	r := &ThresholdResolver{}
	existing := domain.Entity{Confidence: 1.0}
	action := r.Resolve(existing, domain.ExtractedEntity{Content: "new"})
	if action != ActionKeepBoth {
		t.Fatalf("want ActionKeepBoth, got %v", action)
	}
}

func TestThresholdResolver_DefaultArchivesLowConfidence(t *testing.T) {
	r := &ThresholdResolver{}
	existing := domain.Entity{Confidence: 0.3}
	action := r.Resolve(existing, domain.ExtractedEntity{Content: "new"})
	if action != ActionPreferIncoming {
		t.Fatalf("want ActionPreferIncoming, got %v", action)
	}
}

func TestThresholdResolver_ZeroConfidenceTreatedAsOne(t *testing.T) {
	r := &ThresholdResolver{}
	existing := domain.Entity{Confidence: 0} // zero → treated as 1.0
	action := r.Resolve(existing, domain.ExtractedEntity{Content: "new"})
	if action != ActionKeepBoth {
		t.Fatalf("want ActionKeepBoth for zero confidence, got %v", action)
	}
}

func TestThresholdResolver_CustomThreshold(t *testing.T) {
	r := &ThresholdResolver{Threshold: 0.5}
	// confidence exactly at threshold → keep both
	action := r.Resolve(domain.Entity{Confidence: 0.5}, domain.ExtractedEntity{})
	if action != ActionKeepBoth {
		t.Fatalf("at threshold: want ActionKeepBoth, got %v", action)
	}
	// below threshold → prefer incoming
	action = r.Resolve(domain.Entity{Confidence: 0.49}, domain.ExtractedEntity{})
	if action != ActionPreferIncoming {
		t.Fatalf("below threshold: want ActionPreferIncoming, got %v", action)
	}
}
