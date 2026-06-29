package retrieval

import "strings"

// CountTokens estimates the number of tokens in text using a simple
// heuristic: ~4 characters per token for English, ~2 for CJK.
// This is fast enough for hot-path use and close enough for budget
// enforcement. For precise counting, swap in tiktoken-go later.
func CountTokens(text string) int {
	if len(text) == 0 {
		return 0
	}
	// Rough heuristic: split on whitespace, each word ≈ 1.3 tokens,
	// plus punctuation. Simpler: 1 token ≈ 4 bytes for English.
	// CJK characters are ~1 token each but rare in code-heavy contexts.
	words := strings.Fields(text)
	tokens := 0
	for _, w := range words {
		// Each word: at least 1 token; long words split further.
		t := (len(w) + 3) / 4 // ceil(len/4)
		if t < 1 {
			t = 1
		}
		tokens += t
	}
	return tokens
}

// TokenEstimate returns a rough token count for a RetrievalResult
// by summing all fact contents plus structural overhead per fact.
func TokenEstimate(facts []struct{ Content string }) int {
	total := 0
	for _, f := range facts {
		// Each fact: "- " prefix + content + "\n" suffix ≈ 2 overhead tokens
		total += CountTokens(f.Content) + 2
	}
	return total
}
