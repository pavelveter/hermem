package domain

// SearchResult pairs an entity with its cosine similarity to a query.
// Returned by SearchByVector / SearchByVectorStore from the vector
// package and threaded back to the retrieval pipeline as the initial
// hit set for ResolveSeeds.
//
// Lives in pkg/domain (not retrieval) because both the vector package and
// the retrieval-package consumers depend on it, and each imports a
// version of the other — putting SearchResult in retrieval would force
// vector → retrieval → vector. Domain is leaf.
type SearchResult struct {
	Entity     Entity  `json:"entity"`
	Similarity float32 `json:"similarity"`
}
