package domain

// VerifyReport summarises the results of a graph integrity verification.
type VerifyReport struct {
	Issues []string `json:"issues"`
}

// Pass returns true if there are no issues.
func (r *VerifyReport) Pass() bool { return len(r.Issues) == 0 }

// String returns a human-readable report.
func (r *VerifyReport) String() string {
	if r.Pass() {
		return "Graph integrity verified: no issues found.\n"
	}
	s := ""
	for _, issue := range r.Issues {
		s += "  - " + issue + "\n"
	}
	return "Graph integrity issues found:\n" + s
}

// Community is the result of Louvain community detection.
type Community struct {
	ID         string   `json:"id"`
	Members    []string `json:"members"`
	Size       int      `json:"size"`
	Modularity float64  `json:"modularity"`
}

// ContradictionPair is one directed contradicts edge.
type ContradictionPair struct {
	SourceID      string `json:"source_id"`
	SourceContent string `json:"source_content"`
	TargetID      string `json:"target_id"`
	TargetContent string `json:"target_content"`
}

// ConnectedComponent is a group of mutually reachable entity IDs.
type ConnectedComponent struct {
	IDs       []string `json:"ids"`
	Size      int      `json:"size"`
	AvgDegree float64  `json:"avg_degree"`
}
