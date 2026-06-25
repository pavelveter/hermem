// Package contradiction also hosts the ingestion-time detection interface
// (PHASE 2.4 — see detector.go). This file declares the contract only;
// concrete detectors land in lexical.go and composite.go.
package contradiction

import "github.com/pavelveter/hermem/src/internal/core"

// ContradictionDetector flags whether an incoming entity contradicts an
// existing one.
//
// Returning (bool, string) keeps the contract trivial for the first cut:
// callers need only "is it?" and "why?" — a short reason string is enough
// for logging and for the LOW-CONF/HIGH-CONF branch decisions downstream.
// The CompositeDetector short-circuits on the first Detected=true so a
// cheap lexical pass can run before a more expensive semantic pass.
//
// Existing/Incoming are passed by value because core.Entity is small
// enough to copy and Detect must not mutate either side.
type ContradictionDetector interface {
	Detect(existing, incoming core.Entity) (bool, string)
}
