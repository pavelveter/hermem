package detectors

import (
	"strings"

	"github.com/pavelveter/hermem/src/internal/contradiction"
	"github.com/pavelveter/hermem/src/internal/core"
)

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
	return contradiction.DetectionResult{}
}

func lexicalNegationFlip(a, b string) bool {
	al := strings.ToLower(a)
	bl := strings.ToLower(b)
	negWords := []string{
		"not", "don't", "doesn't", "isn't", "aren't", "won't", "can't", "never", "no ", "hate", "dislike",
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
	if sa == al && sb == bl {
		return false
	}
	if strings.Contains(sa, " не") != strings.Contains(sb, " не") {
		return true
	}
	return false
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
	for _, suf := range russianSuffixes {
		if strings.HasSuffix(w, suf) && len(w)-len(suf) >= 3 {
			return w[:len(w)-len(suf)]
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
