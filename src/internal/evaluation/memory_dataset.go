// Package evaluation — memory benchmark dataset.
package evaluation

// MemoryFact is one fact-to-store with its expected retrieval key.
type MemoryFact struct {
	ID      string
	Content string
	Query   string // a natural-language query that should retrieve this fact
}

// MemoryDataset bundles facts and expected query→fact mappings for
// evaluating memory quality (embedding + retrieval pipeline).
type MemoryDataset struct {
	Name  string
	Facts []MemoryFact
	// Expected maps query-text → set of fact IDs that should be retrieved.
	Expected map[string][]string
}

// DefaultMemoryDataset returns a curated dataset covering common
// memory scenarios: factual knowledge, preferences, negations, and
// cross-topic distinctions.
func DefaultMemoryDataset() MemoryDataset {
	return MemoryDataset{
		Name: "default-memory-v1",
		Facts: []MemoryFact{
			{ID: "mem-001", Content: "User likes Go programming language", Query: "What language does the user like?"},
			{ID: "mem-002", Content: "User uses Python for data science", Query: "What does the user use Python for?"},
			{ID: "mem-003", Content: "User prefers Linux over macOS", Query: "Which OS does the user prefer?"},
			{ID: "mem-004", Content: "User works at Acme Corp", Query: "Where does the user work?"},
			{ID: "mem-005", Content: "User lives in Berlin", Query: "Where does the user live?"},
			{ID: "mem-006", Content: "User does not like Java", Query: "Does the user like Java?"},
			{ID: "mem-007", Content: "User's favourite editor is vim", Query: "What editor does the user use?"},
		},
		Expected: map[string][]string{
			"What language does the user like?":  {"mem-001"},
			"What does the user use Python for?": {"mem-002"},
			"Which OS does the user prefer?":     {"mem-003"},
			"Where does the user work?":          {"mem-004"},
			"Where does the user live?":          {"mem-005"},
			"Does the user like Java?":           {"mem-006"},
			"What editor does the user use?":     {"mem-007"},
		},
	}
}

// QueryIDs returns all query strings in stable order so callers can
// iterate deterministically.
func (d MemoryDataset) QueryIDs() []string {
	// Derive from Expected to keep a single source of truth.
	out := make([]string, 0, len(d.Expected))
	for q := range d.Expected {
		out = append(out, q)
	}
	// Sort for deterministic iteration order across runs.
	sortStrings(out)
	return out
}

// sortStrings is a local stable sort — avoids importing "sort" in
// every dataset file and keeps the package surface minimal.
func sortStrings(s []string) {
	for i := 0; i < len(s); i++ {
		for j := i + 1; j < len(s); j++ {
			if s[i] > s[j] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}
