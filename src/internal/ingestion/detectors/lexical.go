package detectors

import (
	_ "embed"
	"strings"

	"golang.org/x/text/unicode/norm"

	"github.com/pavelveter/hermem/src/internal/contradiction"
	"github.com/pavelveter/hermem/src/internal/core"
)

//go:embed assets/negations_en.txt
var negationsEN string

//go:embed assets/negations_ru.txt
var negationsRU string

const lexicalReasonHit = "lexical negation flip"

// LexicalDetector implements ContradictionDetector using the
// round-7 / round-9 negation-flip heuristic.
type LexicalDetector struct{}

// NewLexicalDetector returns a stateless LexicalDetector.
func NewLexicalDetector() *LexicalDetector {
	return &LexicalDetector{}
}

// Detect reports whether existing.Content and incoming.Content
// disagree on a negation token.
func (d *LexicalDetector) Detect(existing, incoming core.Entity) contradiction.DetectionResult {
	if lexicalNegationFlip(existing.Content, incoming.Content) {
		return contradiction.DetectionResult{Detected: true, Reason: lexicalReasonHit, Confidence: 1.0}
	}
	// Lexical can't rule out a contradiction — signal inconclusive so
	// heavier detectors (embedding, LLM) can verify.
	return contradiction.DetectionResult{Inconclusive: true}
}

func lexicalNegationFlip(a, b string) bool {
	al := strings.ToLower(a)
	bl := strings.ToLower(b)
	negWords := parseNegationWords(negationsEN + "\n" + negationsRU)
	for _, n := range negWords {
		if strings.Contains(al, n) != strings.Contains(bl, n) {
			return true
		}
	}
	sa, sb := stemPair(a, b)
	if sa == al && sb == bl {
		return false
	}
	if strings.Contains(sa, " не") != strings.Contains(sb, " не") {
		return true
	}
	return false
}

// parseNegationWords splits a newline-separated list of negation words,
// trimming whitespace and dropping empties.
func parseNegationWords(s string) []string {
	lines := strings.Split(s, "\n")
	words := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			words = append(words, line)
		}
	}
	return words
}

var russianSuffixes = []string{
	"ите", "ешь", "ете", "ем", "ет",
	"ют", "ут", "ят", "ат",
	"лась", "лось", "лись", "лся",
	"ил", "ел", "ал", "ёл", "ол", "ул", "юл",
	"ла", "ло", "ли",
	"ть", "ти",
	"ный", "ная", "ное", "ные", "ого", "ому", "ыми", "ая", "ое", "ые",
}

func stemRussian(w string) string {
	w = strings.ToLower(w)
	wr := []rune(w)
	for _, suf := range russianSuffixes {
		sr := []rune(suf)
		if len(wr) >= len(sr) && string(wr[len(wr)-len(sr):]) == suf && len(wr)-len(sr) >= 3 {
			return string(wr[:len(wr)-len(sr)])
		}
	}
	return w
}

func isCyrillicToken(s string) bool {
	for _, r := range s {
		if r >= 0x0400 && r <= 0x04FF {
			return true
		}
	}
	return false
}

// stemPair produces the stems of a and b for suffix-flip detection.
//
// UNICODE NORMALIZATION (Audit Part 5 #4): both inputs are forced to
// NFC at the entry point before tokenization and case-folding. Go's
// strings.ToLower does NOT normalize, so a Cyrillic token from a user
// in NFD (e.g. "й" rendered as "и" + U+0306 combining breve) would
// otherwise skip past the suffix-matching in stemRussian and create a
// duplicate vertex for the same concept in the knowledge graph.
//
// Cost: one O(len(s)) pass per input. Acceptable because stemPair is
// called once per (existing, incoming) contradiction candidate pair,
// NOT in a tight search loop.
func stemPair(a, b string) (string, string) {
	// Normalize to NFC before any further transformation. Strings that
	// are already in NFC pass through with zero observable change.
	a = norm.NFC.String(a)
	b = norm.NFC.String(b)
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
