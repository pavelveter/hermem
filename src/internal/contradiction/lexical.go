package contradiction

import (
	"strings"

	"github.com/pavelveter/hermem/src/internal/core"
)

// lexicalReasonHit is the canonical reason string emitted by
// LexicalDetector on a hit. Intentionally generic so the contract
// boundary doesn't leak the rule name (substring vs stem-augmented)
// into upstream logs — the rule surface belongs to the heuristic's
// internal docs.
const lexicalReasonHit = "lexical negation flip"

// LexicalDetector implements ContradictionDetector using the
// round-7 / round-9 negation-flip heuristic.
//
// The heuristic runs TWO scans in series:
//
//  1. Original substring scan against a fixed negWords list (preserves
//     the 14 English/Russian regression cases from round-7). Catches
//     bare inflections and cross-verb antonyms in `ненавид-` / `люб-` /
//     `разлюб-` / `не ненавид-` / `не + verb`.
//
//  2. Stem-augmented scan (round-9 § 7.1). Both `a` and `b` are
//     tokenized on whitespace, lower-cased, and each Cyrillic token is
//     stripped of common verb endings by stemRussian(). After
//     stemming, the scan checks for `не + verb_lemma` presence
//     differences — this catches inflected forms the substring list
//     cannot.
//
// The detector reports a hit if EITHER scan reports a flip.
//
// Existing/Incoming are passed by value because core.Entity is small
// enough to copy and Detect must not mutate either side.
type LexicalDetector struct{}

// NewLexicalDetector returns a stateless LexicalDetector.
func NewLexicalDetector() *LexicalDetector {
	return &LexicalDetector{}
}

// Detect reports whether existing.Content and incoming.Content
// disagree on a negation token (the round-7 / round-9 heuristic).
// Returns (true, lexicalReasonHit) on hit, (false, "") on miss.
func (d *LexicalDetector) Detect(existing, incoming core.Entity) (bool, string) {
	if lexicalNegationFlip(existing.Content, incoming.Content) {
		return true, lexicalReasonHit
	}
	return false, ""
}

// lexicalNegationFlip runs the round-7 / round-9 dual-scan
// negation-flip heuristic. Returns true if EITHER scan reports a flip.
// Package-private; callers go through LexicalDetector (or
// IsIngestionContradiction, which is itself a thin wrapper).
func lexicalNegationFlip(a, b string) bool {
	al := strings.ToLower(a)
	bl := strings.ToLower(b)
	negWords := []string{
		// English
		"not", "don't", "doesn't", "isn't", "aren't", "won't", "can't", "never", "no ", "hate", "dislike",
		// Russian — see function godoc for the bare vs inflected rationale.
		"разлюбил", "разлюбила", "разлюбили",
		"ненавижу", "ненавидит", "ненавидел", "ненавидела",
		"не ненавижу", "не ненавидит", "не ненавидел", "не ненавидела",
		"не люблю", "не любит", "не любил", "не любила", "не любили",
		"не хочу", "не хочет", "не хотел", "не хотела",
	}
	for _, n := range negWords {
		if strings.Contains(al, n) != strings.Contains(bl, n) {
			return true
		}
	}
	sa, sb := stemPair(a, b)
	// Bare-particle scan GUARDED by the stemmer actually matching
	// something on at least one side. If sa == al AND sb == bl,
	// neither Cyrillic verb lost a suffix — fall through with false,
	// because the heuristic deliberately under-detects soft adverb
	// negations like "не очень люблю" (see
	// `russian_ne_ochen_falls_through` regression row).
	//
	// Round-9 § 7.1 followup note: an EARLIER round's intermediate
	// fix removed this guard to always fire the bare-particle check.
	// That changed a regression row from "want:false / got:false" to
	// "want:false / got:true" — a real over-detection regression on
	// partial adverb forms. The reintroduction of this guard is
	// paired with the round-9 § 7.1 suffix-table additions (ил/ел/...
	// below): for verbs with a stemmable past-m.sg. ending, the
	// stemmer now matches and sa != al, so the bare-particle check
	// correctly fires on inflected forms like `полюбил` vs `не полюбил`.
	if sa == al && sb == bl {
		return false
	}
	if strings.Contains(sa, " не") != strings.Contains(sb, " не") {
		return true
	}
	return false
}

// russianSuffixes is the round-9 § 7.1 inline stripper's suffix
// table. Order matters: longest matches first. The list covers the
// inflected forms documented in TestIsIngestionContradiction's
// round-9 rows (любит/любила/любили/полюбил). Nominal-case coverage
// is intentionally absent — the stem-augmented scan is invoked only
// after the substring scan fails, so nominal-flip false positives
// stay bounded by the `не + verb_lemma` surface-form check below.
var russianSuffixes = []string{
	"ите", "ешь", "ете", "ем", "ет",
	"ют", "ут", "ят", "ат",
	"лась", "лось", "лись", "лся",
	// Past-tense singular masculine (and small set of vowel-stem
	// m.sg. variants). Added in round-9 § 7.1 so the stemmer strips
	// "полюбил" → "полюби", "любил" → "люб", etc. Without these the
	// bare-particle scan couldn't fire on past-m.sg. inflections
	// (see `russian_stemmer_polubil_ne_polubil` regression row).
	"ил", "ел", "ал", "ёл", "ол", "ул", "юл",
	"ла", "ло", "ли",
	"ть", "ти",
	"ный", "ная", "ное", "ные", "ого", "ому", "ыми", "ая", "ое", "ые",
}

// stemRussian applies the minimal inline suffix-stripper to a single
// Russian token. Returns the lower-cased token with the longest
// matching suffix removed (provided the remaining stem is at least
// 3 characters — never produce empty stems that would alias every
// short preposition onto the same canonical form).
func stemRussian(w string) string {
	w = strings.ToLower(w)
	for _, suf := range russianSuffixes {
		if strings.HasSuffix(w, suf) && len(w)-len(suf) >= 3 {
			return w[:len(w)-len(suf)]
		}
	}
	return w
}

// isCyrillicToken is true iff the token contains at least one
// Cyrillic codepoint (U+0400..U+04FF). Token-level test so English
// surface forms pass through unchanged.
func isCyrillicToken(s string) bool {
	for _, r := range s {
		if r >= 0x0400 && r <= 0x04FF {
			return true
		}
	}
	return false
}

// stemPair returns the joined-token strings of (a, b) after
// per-token stem-strip. If neither side contains Cyrillic, the
// function returns the input strings unchanged so the original
// lowercase pass-through above retains its substring semantics
// (and the function's "sa==al" early-return short-circuits the
// stem-augmented scan).
func stemPair(a, b string) (string, string) {
	stem := func(s string) string {
		parts := strings.Fields(strings.ToLower(s))
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if isCyrillicToken(p) {
				out = append(out, stemRussian(p))
			} else {
				out = append(out, p)
			}
		}
		return strings.Join(out, " ")
	}
	return stem(a), stem(b)
}
