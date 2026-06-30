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
| 8 | POST | `/response` | HandleResponse | server/retrieval | Yes | ✅ |
| 9 | GET | `/provenance` | HandleProvenance | server/retrieval | No (bespoke) | ✅ |
| 10 | POST | `/task/status` | HandleTaskStatus | server/task | No (bespoke) | ✅ |
| 11 | POST | `/task/executable` | HandleTaskExecutable | server/task | Yes | ✅ |
| 12 | POST | `/task/next` | HandleTaskExecutable (alias) | server/task | Yes | ✅ |
| 13 | POST | `/task/claim-next` | HandleTaskClaimNext | server/task | Yes | ❌ |
| 14 | POST | `/task/list` | HandleTaskList | server/task | Yes | ✅ |
| 15 | POST | `/task/show` | HandleTaskShow | server/task | Yes | ✅ |
| 16 | POST | `/task/dep` | HandleTaskDep | server/task | Yes | ✅ |
| 17 | POST | `/task/tree` | HandleTaskTree | server/task | Yes | ✅ |
| 18 | POST | `/task/create` | HandleTaskCreate | server/task | Yes | ✅ |
| 19 | POST | `/task/rollback` | HandleTaskRollback | server/task | Yes | ✅ |
| 20 | GET | `/recovery-plan` | HandleRecoveryPlan | server/task | Yes | ✅ |
| 21 | POST | `/ingest` | HandleIngest | server/ingest | Yes | ✅ |
| 22 | GET | `/ingest/jobs` | HandleJobs | server/ingest | Yes | ❌ |
| 23 | GET | `/contradictions` | HandleContradictions | server/contradiction | Yes | ✅ |
| 24 | GET | `/connected-components` | HandleConnectedComponents | server/graph | Yes | ✅ |
| 25 | GET | `/communities` | HandleCommunities | server/graph | Yes | ✅ |
| 26 | GET | `/graph/verify` | HandleGraphVerify | server/graph | Yes | ✅ |
| 27 | GET | `/db/migrate` | HandleMigrationStatus | server/migration | Yes | ✅ |
| 28 | POST | `/db/rollback` | HandleMigrationRollback | server/migration | Yes | ✅ |
| 29 | GET | `/db/verify` | HandleMigrationVerify | server/migration | Yes | ✅ |
| 30 | GET | `/db/schema` | HandleSchemaFingerprint | server/migration | Yes | ✅ |
| 31 | POST | `/admin/retention/run` | HandleRun | server/retention | Yes | ❌ |
| 32 | POST | `/admin/re-embed` | HandleReEmbed | server/reembed | Yes | ✅ |

## Infrastructure Routes (registered directly in server.go)

| # | Method | Path | Handler | Package | Spec? |
|---|--------|------|---------|---------|-------|
| 33 | * | `/metrics` | MetricsHandler() | server | ✅ |
| 34 | * | `/health` | HandleHealth | server/health | ✅ |
| 35 | * | `/health/live` | HandleHealthLive | server/health | ✅ |
| 36 | * | `/health/ready` | HandleHealthReady | server/health | ✅ |
| 37 | * | `/health/startup` | HandleHealthStartup | server/health | ✅ |

## OpenAPI Spec Routes (registered in server.go via api handler)

| # | Method | Path | Handler | Package | Served? |
|---|--------|------|---------|---------|---------|
| 38 | GET | `/openapi.json` | handleJSON | api | ✅ |
| 39 | GET | `/openapi.yaml` | handleYAML | api | ✅ |

## Debug Routes (opt-in via HERMEM_PPROF_ENABLED=1)

| # | Method | Path | Handler | Package |
|---|--------|------|---------|---------|
| 40 | * | `/debug/pprof/` | pprof.Index | server/pprof |
| 41 | * | `/debug/pprof/cmdline` | pprof.Cmdline | server/pprof |
| 42 | * | `/debug/pprof/profile` | pprof.Profile | server/pprof |
| 43 | * | `/debug/pprof/symbol` | pprof.Symbol | server/pprof |
| 44 | * | `/debug/pprof/trace` | pprof.Trace | server/pprof |

## Spec-Only Routes (in OpenAPI spec but NOT served)

| Path | Method | OperationID | Note |
|------|--------|-------------|------|
| `/query/temporal` | POST | queryTemporal | Spec defines it; no handler registered |

## Discrepancies Found

| Route | In Server | In Spec | Action |
|-------|-----------|---------|--------|
| `/task/claim-next` | ✅ | ❌ | Add to spec |
| `/ingest/jobs` | ✅ | ❌ | Add to spec |
| `/admin/retention/run` | ✅ | ❌ | Add to spec |
| `/query/temporal` | ❌ | ✅ | Remove from spec (dead route) |

Generated: 2026-06-30
