package cli

import (
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

// wireAll constructs all domain services and HTTP shells, returning a
// fully wired *server.Server ready to serve. This centralizes the
// construction boilerplate so adding a new service requires changes in
// only this function (plus the Server struct and NewServer signature).
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
