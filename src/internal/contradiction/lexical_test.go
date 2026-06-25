package contradiction

import (
	"testing"

	"github.com/pavelveter/hermem/src/internal/core"
)

// TestLexicalDetector mirrors TestIsIngestionContradiction's 17
// regression rows at the ContradictionDetector interface boundary.
// Each case exercises the same dual-scan heuristic through
// detector.Detect, and additionally asserts that the reason string
// is non-empty on hits (per the ContradictionDetector contract).
//
// Reason-content assertion is intentionally loose — exact string
// matching would couple tests to the human-readable label, which
// is allowed to evolve. Only the (true → non-empty) and (false →
// empty) shape is locked.
func TestLexicalDetector(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		// English baseline (carried over from pre-round-7)
		{"english_identical", "User likes Go", "User likes Go", false},
		{"english_neg_flip", "User likes Go", "User does not like Go", true},
		{"english_identical_neg", "User does not like Go", "User does not like Go", false},
		{"english_hate_vs_love", "User hates Go", "User loves Go", true},

		// Russian — round-7 § 7 additions
		{"russian_neg_particle", "Я люблю море", "Я не люблю море", true},
		{"russian_hate_to_love", "Я люблю это", "Я ненавижу это", true},
		// Substring-boundary trade-off: see TestIsIngestionContradiction
		// for the full rationale. Locks the "не очень" fall-through.
		{"russian_ne_ochen_falls_through", "Я люблю это", "Я не очень люблю это", false},
		{"russian_razlub_inflection", "Я любил это", "Я разлюбил это", true},
		{"russian_substring_falls_through_nravitsya", "Мне нравится это", "Это красиво", false},
		{"russian_nikogda_neg", "Хочу туда поехать", "Никогда не хочу туда ехать", true},
		{"russian_identical", "Я люблю это", "Я люблю это", false},
		{"russian_neg_identical", "Я не люблю это", "Я не люблю это", false},
		{"russian_double_neg_vs_plain_neg", "Я не ненавижу это", "Я ненавижу это", true},
		{"russian_cross_lang_detect", "User loves X", "User не любит X", true},

		// Round-9 § 7.1 — inline stemmer coverage.
		{"russian_stemmer_lubit_not_lubit", "Я люблю это", "Я не люблю это", true},
		{"russian_stemmer_lubit_ne_lubit", "Я любит море", "Я не любит море", true},
		{"russian_stemmer_polubil_ne_polubil", "Я полюбил это", "Я не полюбил это", true},
		{"russian_stemmer_lubit_labila_no_neg", "Я любит это", "Я любила это", false},
		{"english_does_not_does", "User does not", "User does", true},
	}
	detector := NewLexicalDetector()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, reason := detector.Detect(core.Entity{Content: c.a}, core.Entity{Content: c.b})
			if got != c.want {
				t.Errorf("Detect(%q, %q) detected=%v, want %v", c.a, c.b, got, c.want)
			}
			if got && reason == "" {
				t.Errorf("Detect(%q, %q) hit but reason empty — contract requires non-empty reason on Detected=true", c.a, c.b)
			}
			if !got && reason != "" {
				t.Errorf("Detect(%q, %q) miss but reason non-empty (%q) — contract requires empty reason on Detected=false", c.a, c.b, reason)
			}
		})
	}
}
