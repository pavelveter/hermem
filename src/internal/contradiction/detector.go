// Package contradiction also hosts the ingestion-time detection interface
// (PHASE 2.4 — see detector.go). This file declares the contract only;
// concrete detectors land in lexical.go and composite.go.
package contradiction

import "github.com/pavelveter/hermem/src/internal/core"

// DetectionResult is the unified return shape for a contradiction
// detection pass.
//
// Detected is the boolean signal callers branch on. Reason is a short
// human-readable label suitable for logging — exact wording is not
// part of the contract. Confidence is a calibrated score in [0, 1]:
//
//   - 0 means "not a contradiction" (the detector did not fire).
//   - 1 means "fully confident this is a contradiction" — used by
//     detectors whose logic is binary (e.g. the round-7 / round-9
//     lexical heuristic, which either matches a negWord or it does
//     not; there is no in-between).
//
// Future semantic detectors (e.g. embedding-similarity passes) will
// return fractional scores between 0 and 1 and let downstream
// callers pick a threshold. The lexical detector intentionally does
// NOT participate in that gradient — the substring scan is
// deterministic, so its result is binary.
type DetectionResult struct {
	Detected   bool
	Reason     string
	Confidence float32
}

// ContradictionDetector flags whether an incoming entity contradicts an
// existing one.
//
// The DetectionResult return shape keeps the contract trivial for the
// first cut: callers need only "is it?", "why?", and "how confident?" —
// a short reason string is enough for logging and a confidence score
// is reserved for future semantic detectors. The CompositeDetector
// short-circuits on the first Detected=true so a cheap lexical pass
// can run before a more expensive semantic pass.
//
// Existing/Incoming are passed by value because core.Entity is small
// enough to copy and Detect must not mutate either side.
type ContradictionDetector interface {
	Detect(existing, incoming core.Entity) DetectionResult
}
