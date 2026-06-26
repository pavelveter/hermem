// Package shared provides HTTP helpers shared across server sub-packages.
//
// These were previously duplicated verbatim in server/memory and server/edge.
// Consolidating them here eliminates copy-paste and gives a single place
// for schema-conflict guard evolution.
package shared

import (
	"errors"
	"net/http"

	"github.com/pavelveter/hermem/src/internal/core"
	"github.com/pavelveter/hermem/src/internal/httputil"
	"github.com/pavelveter/hermem/src/internal/metrics"
	"github.com/pavelveter/hermem/src/internal/serverstate"
)

// RejectSchemaConflict writes a 409 envelope and returns true if the
// schema generation observed at handler entry differs from the live
// Refs (SIGHUP swapped mid-request). Callers should abort the handler
// when this returns true.
func RejectSchemaConflict(w http.ResponseWriter, gen uint64, refs *serverstate.Ref, m *metrics.Metrics) bool {
	if !refs.IsStale(gen) {
		return false
	}
	m.IncSchemaConflict()
	httputil.WriteErrorWithCode(w, http.StatusConflict,
		"schema changed during request; retry",
		"schema_conflict", "")
	return true
}

// IsSchemaErr reports whether err is a core.DomainError with
// CodeInvalidSchema — the domain's signal that a request field
// violates the current schema. HTTP shells map this to 422.
func IsSchemaErr(err error) bool {
	if err == nil {
		return false
	}
	var de *core.DomainError
	return errors.As(err, &de) && de.Code == core.CodeInvalidSchema
}
