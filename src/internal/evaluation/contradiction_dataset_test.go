package evaluation

import "testing"

func TestDefaultContradictionDataset(t *testing.T) {
	ds := DefaultContradictionDataset()
	if ds.Name == "" {
		t.Fatal("dataset name empty")
	}
	if len(ds.Pairs) == 0 {
		t.Fatal("dataset has no pairs")
	}

	pos := ds.PositivePairs()
	neg := ds.NegativePairs()
	if len(pos)+len(neg) != len(ds.Pairs) {
		t.Fatalf("PositivePairs(%d) + NegativePairs(%d) != total(%d)", len(pos), len(neg), len(ds.Pairs))
	}
	if len(pos) == 0 {
		t.Fatal("no positive (contradiction) pairs")
	}
	if len(neg) == 0 {
		t.Fatal("no negative (non-contradiction) pairs")
	}

	// Verify all pairs have non-empty IDs.
	for _, p := range ds.Pairs {
		if p.ID == "" {
			t.Errorf("pair %+v has empty ID", p)
		}
	}
}

func TestContradictionDataset_CoversLanguages(t *testing.T) {
	ds := DefaultContradictionDataset()
	hasEnglish := false
	hasRussian := false
	for _, p := range ds.Pairs {
		if containsCyrillic(p.TextA) || containsCyrillic(p.TextB) {
			hasRussian = true
		}
		if !containsCyrillic(p.TextA) && !containsCyrillic(p.TextB) && p.TextA != "" {
			hasEnglish = true
		}
	}
	if !hasEnglish {
		t.Error("dataset has no English pairs")
	}
	if !hasRussian {
		t.Error("dataset has no Russian pairs")
	}
}

func containsCyrillic(s string) bool {
	for _, r := range s {
		if r >= 0x0400 && r <= 0x04FF {
			return true
		}
	}
	return false
}
