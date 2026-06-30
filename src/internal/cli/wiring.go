package cli

import (
	"github.com/pavelveter/hermem/src/internal/app"
	clienv "github.com/pavelveter/hermem/src/internal/cli/env"
	contradictdomain "github.com/pavelveter/hermem/src/internal/contradiction"
	edgedomain "github.com/pavelveter/hermem/src/internal/edge"
	graphdomain "github.com/pavelveter/hermem/src/internal/graph"
	healthdomain "github.com/pavelveter/hermem/src/internal/health"
	ingestdomain "github.com/pavelveter/hermem/src/internal/ingest"
	memdomain "github.com/pavelveter/hermem/src/internal/memory"
	migrationdomain "github.com/pavelveter/hermem/src/internal/migration"
	reembeddomain "github.com/pavelveter/hermem/src/internal/reembed"
	retentiondomain "github.com/pavelveter/hermem/src/internal/retention"
	retdomain "github.com/pavelveter/hermem/src/internal/retrieval"
	"github.com/pavelveter/hermem/src/internal/server"
	cnd "github.com/pavelveter/hermem/src/internal/server/contradiction"
	"github.com/pavelveter/hermem/src/internal/server/edge"
	graphsrv "github.com/pavelveter/hermem/src/internal/server/graph"
	healthsrv "github.com/pavelveter/hermem/src/internal/server/health"
	ingsrv "github.com/pavelveter/hermem/src/internal/server/ingest"
	mem "github.com/pavelveter/hermem/src/internal/server/memory"
	migrsrv "github.com/pavelveter/hermem/src/internal/server/migration"
	"github.com/pavelveter/hermem/src/internal/server/reembed"
	"github.com/pavelveter/hermem/src/internal/server/retention"
	ret "github.com/pavelveter/hermem/src/internal/server/retrieval"
	tasksvc "github.com/pavelveter/hermem/src/internal/server/task"
	"github.com/pavelveter/hermem/src/internal/server/timeline"
	"github.com/pavelveter/hermem/src/internal/serverstate"
	taskdomain "github.com/pavelveter/hermem/src/internal/task"
	timelinedomain "github.com/pavelveter/hermem/src/internal/timeline"
)

// wireAll constructs all domain services and HTTP shells from an
// *clienv.Env, returning a fully wired *server.Server. Deprecated:
// use WireFromApplication with *app.Application instead.
func wireAll(env *clienv.Env, refs *serverstate.Ref) *server.Server {
	// Domain services
	memSvc := memdomain.New(env.DB, env.VI, env.Embedder)
	edgeSvc := edgedomain.New(env.DB, env.VI, env.Embedder)
	timelineSvc := timelinedomain.New(env.DB)
	healthSvc := healthdomain.New(
		healthdomain.DBProbe(env.DB),
		healthdomain.VectorIndexProbe(env.VI, env.Cfg.VectorDim),
		healthdomain.EmbedderProbe(env.Embedder),
		healthdomain.ExtractorProbe(env.Extractor),
		healthdomain.DiskSpaceProbe(env.Cfg.DBPath),
	).WithMetrics(env.Metrics)
	reembedSvc := reembeddomain.New(env.DB, env.VI, env.Embedder)
	retSvc := retdomain.New(env.DB, env.VI, env.Embedder)
	env.Retriever = retSvc
	cndSvc := contradictdomain.New(env.DB)
	taskSvc := taskdomain.New(env.DB, env.Embedder, env.VI)
	graphSvc := graphdomain.New(env.DB)
	migrSvc := migrationdomain.New(env.DB)
	ingestSvc := ingestdomain.New(env.DB, env.VI, env.Embedder, env.Extractor)
	retentionSvc := retentiondomain.New(env.DB, env.VI)

	// HTTP shells + server
	return server.NewServer(
		refs,
		ret.New(retSvc, env.Metrics, refs),
		tasksvc.New(taskSvc, env.Metrics, refs),
		mem.New(memSvc, env.Metrics, refs, env.Cfg.DedupThreshold),
		edge.New(edgeSvc, env.Metrics, refs),
		timeline.New(timelineSvc, env.Metrics),
		ingsrv.New(ingestSvc, env.Metrics, refs, env.Cfg.DedupThreshold),
		cnd.New(cndSvc, env.Metrics),
		graphsrv.New(graphSvc, env.Metrics, refs, env.Cfg.VectorDim),
		migrsrv.New(migrSvc, env.Metrics, refs),
		retention.New(retentionSvc, env.Metrics, refs, env.Cfg.Retention),
		reembed.New(reembedSvc, env.Metrics),
		healthsrv.New(healthSvc),
		env.Metrics,
	)
}

// WireFromApplication constructs all domain services and HTTP shells
// from an *app.Application, returning a fully wired *server.Server.
// This is the new entry point replacing wireAll; the dependency
// graph is identical but sourced from the typed DI container instead
// of the lazy Env bag.
func WireFromApplication(a *app.Application, refs *serverstate.Ref) *server.Server {
	// Domain services
	memSvc := memdomain.New(a.DB, a.VI, a.Embedder)
	edgeSvc := edgedomain.New(a.DB, a.VI, a.Embedder)
	timelineSvc := timelinedomain.New(a.DB)
	healthSvc := healthdomain.New(
		healthdomain.DBProbe(a.DB),
		healthdomain.VectorIndexProbe(a.VI, a.Cfg.VectorDim),
		healthdomain.EmbedderProbe(a.Embedder),
		healthdomain.ExtractorProbe(a.Extractor),
		healthdomain.DiskSpaceProbe(a.Cfg.DBPath),
	).WithMetrics(a.Metrics)
	reembedSvc := reembeddomain.New(a.DB, a.VI, a.Embedder)
	retSvc := retdomain.New(a.DB, a.VI, a.Embedder)
	cndSvc := contradictdomain.New(a.DB)
	taskSvc := taskdomain.New(a.DB, a.Embedder, a.VI)
	graphSvc := graphdomain.New(a.DB)
	migrSvc := migrationdomain.New(a.DB)
	ingestSvc := ingestdomain.New(a.DB, a.VI, a.Embedder, a.Extractor)
	retentionSvc := retentiondomain.New(a.DB, a.VI)

	// HTTP shells + server
	return server.NewServerFromDeps(server.ServerDeps{
		Refs:          refs,
		Retrieval:     ret.New(retSvc, a.Metrics, refs),
		Task:          tasksvc.New(taskSvc, a.Metrics, refs),
		Memory:        mem.New(memSvc, a.Metrics, refs, a.Cfg.DedupThreshold),
		Edge:          edge.New(edgeSvc, a.Metrics, refs),
		Timeline:      timeline.New(timelineSvc, a.Metrics),
		Ingest:        ingsrv.New(ingestSvc, a.Metrics, refs, a.Cfg.DedupThreshold),
		Contradiction: cnd.New(cndSvc, a.Metrics),
		Graph:         graphsrv.New(graphSvc, a.Metrics, refs, a.Cfg.VectorDim),
		Migration:     migrsrv.New(migrSvc, a.Metrics, refs),
		Retention:     retention.New(retentionSvc, a.Metrics, refs, a.Cfg.Retention),
		Reembed:       reembed.New(reembedSvc, a.Metrics),
		Health:        healthsrv.New(healthSvc),
		Metrics:       a.Metrics,
	})
}
