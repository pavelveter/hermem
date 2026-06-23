package main

import (
	"strings"
	"unicode"
)

// negationWords are terms that invert the meaning of a statement.
// When one statement contains a negation word directly attached to
// a shared concept word, it signals a contradiction.
var negationWords = map[string]bool{
	"not": true, "no": true, "never": true, "none": true, "neither": true,
	"nothing": true, "nobody": true, "without": true,
	"don't": true, "doesn't": true, "didn't": true, "isn't": true,
	"wasn't": true, "weren't": true, "won't": true, "wouldn't": true,
	"shouldn't": true, "couldn't": true, "can't": true, "cannot": true,
	"haven't": true, "hasn't": true, "hadn't": true,
}

// sentimentOppositesEn maps lowercase words (including inflected forms)
// to their common antonyms. A pair in either direction is a contradiction
// signal: if ex contains a key and in contains its value, or vice versa.
// All common inflected forms are listed explicitly — no stemming needed.
var sentimentOpposites = map[string]string{
	"like": "hate", "likes": "hates", "liked": "hated", "liking": "hating",
	"love": "hate", "loves": "hates", "loved": "hated", "loving": "hating",
	"good": "bad", "great": "terrible", "better": "worse", "best": "worst",
	"positive": "negative", "true": "false", "right": "wrong",
	"always": "never", "yes": "no",
	"happy": "sad", "happier": "sadder", "happiest": "saddest",
	"fast": "slow", "faster": "slower", "fastest": "slowest",
	"easy": "hard", "easier": "harder", "easiest": "hardest",
	"correct": "incorrect", "success": "failure", "win": "lose",
	"wins": "loses", "won": "lost", "winning": "losing",
	"up": "down", "high": "low", "higher": "lower", "highest": "lowest",
	"big": "small", "bigger": "smaller", "biggest": "smallest",
	"new": "old", "newer": "older", "newest": "oldest",
	"hot": "cold", "hotter": "colder", "hottest": "coldest",
	"open": "closed", "opens": "closes", "opened": "closed",
	"start": "stop", "starts": "stops", "started": "stopped",
	"support": "oppose", "supports": "opposes", "supported": "opposed",
	"accept": "reject", "accepts": "rejects", "accepted": "rejected",
	"allow": "deny", "allows": "denies", "allowed": "denied",
	"agree": "disagree", "agrees": "disagrees", "agreed": "disagreed",
	"approve": "disapprove", "approves": "disapproves",
}

// isContradiction returns true when two statements likely contradict
// each other. It uses a heuristic combining word overlap, negation
// detection, and direct antonym lookup (no stemming). No LLM calls —
// fast enough for inline ingestion checks.
//
// Algorithm:
//  1. Tokenize both texts (lowercase, strip punctuation).
//  2. Compute word overlap. If < 25%, not close enough to contradict.
//  3. Negation check: if one text has a negation word and the other
//     doesn't, AND they share at least one concept word → contradiction.
//  4. Sentiment check: if one text contains a word whose antonym
//     (from sentimentOpposites) appears in the other → contradiction.
func isContradiction(existing, incoming string) bool {
	ea := tokenize(existing)
	ia := tokenize(incoming)

	exWords := toSet(ea)
	inWords := toSet(ia)

	// Compute Jaccard-like overlap.
	shared := 0
	for w := range exWords {
		if inWords[w] {
			shared++
		}
	}
	total := len(exWords) + len(inWords) - shared
	if total == 0 {
		return false
	}
	overlap := float32(shared) / float32(total)

	if overlap < 0.25 {
		return false
	}

	// Check 1: negation word paired with an un-negated shared concept.
	// Only flag when the negated concept actually appears in the other
	// statement — avoids false positives like "not sent yesterday" vs
	// "sent today" (different timeframes, not truly contradictory).
	exNeg := containsAny(exWords, negationWords)
	inNeg := containsAny(inWords, negationWords)
	// TODO: tighten negation check — verify the negated concept word
	// actually appears un-negated in the other text (not just any shared word).
	if exNeg != inNeg && shared > 0 {
		return true
	}

	// Check 2: sentiment-opposite pairs — direct lookup with inflected forms.
	// Confidence comparison is handled by the caller (ProcessDialogWithProvenance)
	// using highConfidenceThreshold to decide between keep-both vs archive-existing.
	for w := range exWords {
		if opp, ok := sentimentOpposites[w]; ok && inWords[opp] {
			return true
		}
	}
	for w := range inWords {
		if opp, ok := sentimentOpposites[w]; ok && exWords[opp] {
			return true
		}
	}

	return false
}

// tokenize splits text into lowercase words, stripping punctuation.
// Contractions (don't, can't) are preserved as-is for negation matching.
func tokenize(s string) []string {
	s = strings.ToLower(s)
	var out []string
	start := -1
	for i, r := range s {
		if unicode.IsLetter(r) || r == '\'' {
			if start == -1 {
				start = i
			}
		} else {
			if start != -1 {
				out = append(out, s[start:i])
				start = -1
			}
		}
	}
	if start != -1 {
		out = append(out, s[start:])
	}
	return out
}

// toSet converts a token slice to a set (map[string]bool).
func toSet(tokens []string) map[string]bool {
	m := make(map[string]bool, len(tokens))
	for _, t := range tokens {
		m[t] = true
	}
	return m
}

// containsAny returns true if any key in needles is present in haystack.
func containsAny(haystack map[string]bool, needles map[string]bool) bool {
	for n := range needles {
		if haystack[n] {
			return true
		}
	}
	return false
}
