// Package evolution provides belief lifecycle management: trust scoring,
// history tracking, revision chains, confidence propagation, evidence
// aggregation, and superseded-belief queries.
//
// Organization:
//
//   - trust.go — TrustScore, TrustDefaults: composite trust scoring formula
//   - history.go — RecordHistory, ListHistory: append-only belief mutation log
//   - chains.go — TraceRevisions: parent_chain_id revision chain traversal
//   - propagation.go — PropagateConfidence: confidence propagation through edges
//   - aggregation.go — AggregateEvidence: evidence aggregation for beliefs
//   - relationships.go — GetSupportRefute: support/refute edge queries
//   - superseded.go — superseded-belief tracking and cleanup
//   - queries.go — GetSupersededBy, StateAt: point-in-time belief state queries
//
// All functions are stateless (no Service struct) — callers pass *sql.DB directly.
// This package is currently used by test/evaluation infrastructure only.
package evolution
