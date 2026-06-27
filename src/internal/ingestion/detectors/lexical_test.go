package detectors

import (
	"testing"

	"golang.org/x/text/unicode/norm"

	"github.com/pavelveter/hermem/src/internal/core"
)

// TestStemPair_NormalizesNFD — Audit Part 5 #4 regression. Two strings
// that LOOK identical but differ in Unicode normalization form (NFC
// vs NFD) MUST produce byte-identical stems after stemPair. Without
// the NFC entry-point normalization, the NFD "й" / "ё" / "й" would
// decompose into a base letter + combining diacritic, slipping past
// the suffix byte-equality check and creating phantom duplicate
// vertices in the graph.
//
// We exercise the conversion explicitly (norm.NFD.String of the NFC
// source) and re-apply stemPair to the NFD form — its stem MUST equal
// the stem of the NFC form.
func TestStemPair_NormalizesNFD(t *testing.T) {
	cases := []struct {
		name string
		nfc  string
	}{
		{"yogurt_with_breve", "йогурт"},  // NFC: и + U+0306 combining breve
		{"yelka_with_diaeresis", "ёлка"}, // NFC: е + U+0308 combining diaeresis
		{"mixed_ru_word", "разлюбил"},    // plain Russian, smoke test
		{"neg_particle", "не люблю"},     // detection path
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			nfd := norm.NFD.String(c.nfc)
			if nfd == c.nfc {
				// Sanity: NFC/NFD differ for these inputs. If a future
				// Go/x-text version ships where NFD is identity for
				// these characters, the audit premise weakens — surface.
				t.Logf("NOTE: NFC == NFD for %q; assertion tighter than necessary but still valid", c.nfc)
			}
			nfcStem, nfdStem := stemPair(c.nfc, nfd)
			if nfcStem != nfdStem {
				t.Errorf("NFC stem %q ≠ NFD stem %q for input %q (NFD=%q) — NFC normalization missing or incomplete at stemPair entry",
					nfcStem, nfdStem, c.nfc, nfd)
			}
		})
	}

	// Property: stemPair on identical input (modulo normalization) MUST
	// collapse to the same stem as the NFC form, even when one side is
	// explicitly NFD.
	a, b := stemPair("йогурт", norm.NFD.String("йогурт"))
	if a != b {
		t.Errorf("non-pair stemPair mismatch: NFC %q vs NFD %q", a, b)
	}
}

func TestLexicalDetector(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{"english_identical", "User likes Go", "User likes Go", false},
		{"english_neg_flip", "User likes Go", "User does not like Go", true},
		{"english_identical_neg", "User does not like Go", "User does not like Go", false},
		{"english_hate_vs_love", "User hates Go", "User loves Go", true},

		{"russian_neg_particle", "Я люблю море", "Я не люблю море", true},
		{"russian_hate_to_love", "Я люблю это", "Я ненавижу это", true},
		{"russian_ne_ochen_falls_through", "Я люблю это", "Я не очень люблю это", false},
		{"russian_razlub_inflection", "Я любил это", "Я разлюбил это", true},
		{"russian_substring_falls_through_nravitsya", "Мне нравится это", "Это красиво", false},
		{"russian_nikogda_neg", "Хочу туда поехать", "Никогда не хочу туда ехать", true},
		{"russian_identical", "Я люблю это", "Я люблю это", false},
		{"russian_neg_identical", "Я не люблю это", "Я не люблю это", false},
		{"russian_double_neg_vs_plain_neg", "Я не ненавижу это", "Я ненавижу это", true},
		{"russian_cross_lang_detect", "User loves X", "User не любит X", true},

		{"russian_stemmer_lubit_not_lubit", "Я люблю это", "Я не люблю это", true},
		{"russian_stemmer_lubit_ne_lubit", "Я любит море", "Я не любит море", true},
		{"russian_stemmer_polubil_ne_polubil", "Я полюбил это", "Я не полюбил это", true},
		{"russian_stemmer_lubit_labila_no_neg", "Я любит это", "Я любила это", false},
		{"english_does_not_does", "User does not", "User does", true},
	}
	detector := NewLexicalDetector()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := detector.Detect(core.Entity{Content: c.a}, core.Entity{Content: c.b})
			if result.Detected != c.want {
				t.Errorf("Detect(%q, %q) detected=%v, want %v", c.a, c.b, result.Detected, c.want)
			}
			if result.Detected && result.Reason == "" {
				t.Errorf("Detect(%q, %q) hit but reason empty", c.a, c.b)
			}
			if !result.Detected && result.Reason != "" {
				t.Errorf("Detect(%q, %q) miss but reason non-empty (%q)", c.a, c.b, result.Reason)
			}
			if result.Detected && result.Confidence != 1.0 {
				t.Errorf("Detect(%q, %q) hit but confidence=%v; want 1.0", c.a, c.b, result.Confidence)
			}
			if !result.Detected && result.Confidence != 0 {
				t.Errorf("Detect(%q, %q) miss but confidence=%v; want 0", c.a, c.b, result.Confidence)
			}
		})
	}
}
