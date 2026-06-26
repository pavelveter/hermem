package server

import (
	"net/http"

	"github.com/pavelveter/hermem/src/internal/server/contradiction"
	"github.com/pavelveter/hermem/src/internal/server/edge"
	"github.com/pavelveter/hermem/src/internal/server/graph"
	"github.com/pavelveter/hermem/src/internal/server/health"
	"github.com/pavelveter/hermem/src/internal/server/ingest"
	"github.com/pavelveter/hermem/src/internal/server/memory"
	"github.com/pavelveter/hermem/src/internal/server/migration"
	"github.com/pavelveter/hermem/src/internal/server/reembed"
	"github.com/pavelveter/hermem/src/internal/server/retention"
	"github.com/pavelveter/hermem/src/internal/server/retrieval"
	"github.com/pavelveter/hermem/src/internal/server/task"
	"github.com/pavelveter/hermem/src/internal/server/timeline"
)

// RouteProvider is the contract every HTTP shell implements to expose
// its URL → handler mapping. server.Server.mount() iterates over a slice
// of providerSlot (each holding a name + a RouteProvider) so a single
// registrations loop handles every shell while still surfacing clear
// slog.Warn log lines if any shell is mis-wired.
//
// Pre-§3.1: Every shell implemented Routes() structurally (12 copies of
// the same map[string]http.HandlerFunc shape), but no Go interface existed.
// Server.mount() carried 12 separate for-range blocks, one per shell, with
// the field name hard-coded — adding a 13th shell required editing both
// the Server struct and mount().
//
// post-§3.1: Server.mount() iterates []providerSlot{...} built from the
// existing typed Server fields. Fields stay typed (compile-time safety on
// type assertions / field-specific middleware), the interface is the
// adapter between the typed Server struct and the dynamic mux registration.
type RouteProvider interface {
	Routes() map[string]http.HandlerFunc
}

// Compile-time assertions: every HTTP shell MUST satisfy RouteProvider.
// A signature change to any shell's Routes() method (DRIFT: signature
// drift) breaks these checks at compile time, before any request fires.
var (
	_ RouteProvider = (*contradiction.HTTPService)(nil)
	_ RouteProvider = (*edge.HTTPService)(nil)
	_ RouteProvider = (*graph.HTTPService)(nil)
	_ RouteProvider = (*health.HTTPService)(nil)
	_ RouteProvider = (*ingest.HTTPService)(nil)
	_ RouteProvider = (*memory.HTTPService)(nil)
	_ RouteProvider = (*migration.HTTPService)(nil)
	_ RouteProvider = (*reembed.HTTPService)(nil)
	_ RouteProvider = (*retention.HTTPService)(nil)
	_ RouteProvider = (*retrieval.HTTPService)(nil)
	_ RouteProvider = (*task.HTTPService)(nil)
	_ RouteProvider = (*timeline.HTTPService)(nil)
)

// providerSlot pairs a shell name with its RouteProvider so mount() can
// warn by name when a shell is nil — without the name the operator can't
// fix the wiring from the warning alone.
type providerSlot struct {
	name string
	p    RouteProvider
}
