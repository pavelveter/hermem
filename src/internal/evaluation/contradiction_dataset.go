package evaluation

// ContradictionPair represents a labeled pair of texts for contradiction detection.
type ContradictionPair struct {
	ID          string
	TextA       string
	TextB       string
	IsContradiction bool
	Confidence  float32 // human-annotated confidence (0..1)
}

// ContradictionDataset is a labeled dataset for evaluating contradiction detectors.
type ContradictionDataset struct {
	Name  string
	Pairs []ContradictionPair
}

// DefaultContradictionDataset returns a curated dataset covering
// lexical, semantic, and cross-lingual contradiction patterns.
func DefaultContradictionDataset() ContradictionDataset {
	return ContradictionDataset{
		Name: "default-contradiction-v1",
		Pairs: []ContradictionPair{
			// Lexical negation — English
			{ID: "lex-en-001", TextA: "User likes Go", TextB: "User does not like Go", IsContradiction: true, Confidence: 1.0},
			{ID: "lex-en-002", TextA: "User hates Python", TextB: "User loves Python", IsContradiction: true, Confidence: 1.0},
			{ID: "lex-en-003", TextA: "User likes Go", TextB: "User likes Go", IsContradiction: false, Confidence: 1.0},
			{ID: "lex-en-004", TextA: "User does not like Go", TextB: "User does not like Go", IsContradiction: false, Confidence: 1.0},

			// Lexical negation — Russian
			{ID: "lex-ru-001", TextA: "Я люблю море", TextB: "Я не люблю море", IsContradiction: true, Confidence: 1.0},
			{ID: "lex-ru-002", TextA: "Я люблю это", TextB: "Я ненавижу это", IsContradiction: true, Confidence: 1.0},
			{ID: "lex-ru-003", TextA: "Я люблю это", TextB: "Я не очень люблю это", IsContradiction: false, Confidence: 0.8},
			{ID: "lex-ru-004", TextA: "Я любил это", TextB: "Я разлюбил это", IsContradiction: true, Confidence: 1.0},
			{ID: "lex-ru-005", TextA: "Я люблю это", TextB: "Я люблю это", IsContradiction: false, Confidence: 1.0},

			// Cross-lingual
			{ID: "cross-001", TextA: "User loves X", TextB: "User не любит X", IsContradiction: true, Confidence: 0.9},

			// Semantic contradictions — factual
			{ID: "sem-001", TextA: "The Earth orbits the Sun", TextB: "The Sun orbits the Earth", IsContradiction: true, Confidence: 0.95},
			{ID: "sem-002", TextA: "Water boils at 100°C at sea level", TextB: "Water boils at 50°C at sea level", IsContradiction: true, Confidence: 0.95},
			{ID: "sem-003", TextA: "Python is a statically typed language", TextB: "Python is a dynamically typed language", IsContradiction: true, Confidence: 0.9},

			// Non-contradictions — different topics
			{ID: "non-001", TextA: "User likes Go", TextB: "User uses Python", IsContradiction: false, Confidence: 0.9},
			{ID: "non-002", TextA: "The sky is blue", TextB: "Grass is green", IsContradiction: false, Confidence: 1.0},
			{ID: "non-003", TextA: "User prefers vim", TextB: "User uses Linux", IsContradiction: false, Confidence: 0.9},

			// Non-contradictions — complementary statements
			{ID: "non-004", TextA: "User likes Go", TextB: "User also likes Rust", IsContradiction: false, Confidence: 0.95},
			{ID: "non-005", TextA: "Server is fast", TextB: "Server is reliable", IsContradiction: false, Confidence: 0.9},

			// Edge cases
			{ID: "edge-001", TextA: "", TextB: "Some text", IsContradiction: false, Confidence: 1.0},
			{ID: "edge-002", TextA: "", TextB: "", IsContradiction: false, Confidence: 1.0},
			{ID: "edge-003", TextA: "A", TextB: "A", IsContradiction: false, Confidence: 1.0},
		},
	}
}

// PositivePairs returns only the contradiction pairs.
func (d ContradictionDataset) PositivePairs() []ContradictionPair {
	var out []ContradictionPair
	for _, p := range d.Pairs {
		if p.IsContradiction {
			out = append(out, p)
		}
	}
	return out
}

// NegativePairs returns only the non-contradiction pairs.
func (d ContradictionDataset) NegativePairs() []ContradictionPair {
	var out []ContradictionPair
	for _, p := range d.Pairs {
		if !p.IsContradiction {
			out = append(out, p)
		}
	}
	return out
}
