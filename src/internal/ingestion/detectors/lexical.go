package detectors

import (
	_ "embed"
	"strings"

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
	return contradiction.DetectionResult{}
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
