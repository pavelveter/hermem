# Hermem Route Inventory

> **Generated file** — do not edit manually. Regenerate with:
> `make routes` or `go run ./scripts/gen-routes.go`

Auto-generated from the OpenAPI spec (`api/spec.go`) and server route
registration. Cross-reference with `api/paths.go`.

## Domain Routes (registered via `Routes()` methods)

| # | Method | Path | Handler | Package | Wrapped | Spec? |
|---|--------|------|---------|---------|---------|-------|
| 1 | POST | `/store` | HandleStore | server/memory | Yes | ✅ |
| 2 | POST | `/edge` | HandleEdge | server/edge | Yes | ✅ |
| 3 | GET | `/timeline` | HandleTimeline | server/timeline | Yes | ✅ |
| 4 | POST | `/search` | HandleSearch | server/retrieval | Yes | ✅ |
| 5 | POST | `/retrieve` | HandleRetrieve | server/retrieval | Yes | ✅ |
| 6 | POST | `/query` | HandleQuery | server/retrieval | Yes | ✅ |
| 7 | POST | `/query/explain` | HandleQueryExplain | server/retrieval | Yes | ✅ |
| 8 | POST | `/query/temporal` | HandleQueryTemporal | server/retrieval | Yes | ✅ |
| 9 | POST | `/response` | HandleResponse | server/retrieval | Yes | ✅ |
| 10 | GET | `/provenance` | HandleProvenance | server/retrieval | No (bespoke) | ✅ |
| 11 | POST | `/task/status` | HandleTaskStatus | server/task | No (bespoke) | ✅ |
| 12 | POST | `/task/executable` | HandleTaskExecutable | server/task | Yes | ✅ |
| 13 | POST | `/task/next` | HandleTaskExecutable (alias) | server/task | Yes | ✅ |
| 14 | POST | `/task/claim-next` | HandleTaskClaimNext | server/task | Yes | ✅ |
| 15 | POST | `/task/list` | HandleTaskList | server/task | Yes | ✅ |
| 16 | POST | `/task/show` | HandleTaskShow | server/task | Yes | ✅ |
| 17 | POST | `/task/dep` | HandleTaskDep | server/task | Yes | ✅ |
| 18 | POST | `/task/tree` | HandleTaskTree | server/task | Yes | ✅ |
| 19 | POST | `/task/create` | HandleTaskCreate | server/task | Yes | ✅ |
| 20 | POST | `/task/rollback` | HandleTaskRollback | server/task | Yes | ✅ |
| 21 | GET | `/recovery-plan` | HandleRecoveryPlan | server/task | Yes | ✅ |
| 22 | POST | `/ingest` | HandleIngest | server/ingest | Yes | ✅ |
| 23 | GET | `/ingest/jobs` | HandleJobs | server/ingest | Yes | ✅ |
| 24 | GET | `/contradictions` | HandleContradictions | server/contradiction | Yes | ✅ |
| 25 | GET | `/connected-components` | HandleConnectedComponents | server/graph | Yes | ✅ |
| 26 | GET | `/communities` | HandleCommunities | server/graph | Yes | ✅ |
| 27 | GET | `/graph/verify` | HandleGraphVerify | server/graph | Yes | ✅ |
| 28 | GET | `/db/migrate` | HandleMigrationStatus | server/migration | Yes | ✅ |
| 29 | POST | `/db/rollback` | HandleMigrationRollback | server/migration | Yes | ✅ |
| 30 | GET | `/db/verify` | HandleMigrationVerify | server/migration | Yes | ✅ |
| 31 | GET | `/db/schema` | HandleSchemaFingerprint | server/migration | Yes | ✅ |
| 32 | POST | `/admin/retention/run` | HandleRun | server/retention | Yes | ✅ |
| 33 | POST | `/admin/re-embed` | HandleReEmbed | server/reembed | Yes | ✅ |

## Infrastructure Routes (registered directly in server.go)

| # | Method | Path | Handler | Package | Spec? |
|---|--------|------|---------|---------|-------|
| 34 | * | `/metrics` | MetricsHandler() | server | ✅ |
| 35 | * | `/health` | HandleHealth | server/health | ✅ |
| 36 | * | `/health/live` | HandleHealthLive | server/health | ✅ |
| 37 | * | `/health/ready` | HandleHealthReady | server/health | ✅ |
| 38 | * | `/health/startup` | HandleHealthStartup | server/health | ✅ |

## OpenAPI Spec Routes (registered in server.go via api handler)

| # | Method | Path | Handler | Package | Served? |
|---|--------|------|---------|---------|---------|
| 39 | GET | `/openapi.json` | handleJSON | api | ✅ |
| 40 | GET | `/openapi.yaml` | handleYAML | api | ✅ |

## Debug Routes (opt-in via HERMEM_PPROF_ENABLED=1)

| # | Method | Path | Handler | Package |
|---|--------|------|---------|---------|
| 41 | * | `/debug/pprof/` | pprof.Index | server/pprof |
| 42 | * | `/debug/pprof/cmdline` | pprof.Cmdline | server/pprof |
| 43 | * | `/debug/pprof/profile` | pprof.Profile | server/pprof |
| 44 | * | `/debug/pprof/symbol` | pprof.Symbol | server/pprof |
| 45 | * | `/debug/pprof/trace` | pprof.Trace | server/pprof |

## Discrepancies Found

All discrepancies resolved. No remaining mismatches between server and spec.

Generated: 2026-07-02
