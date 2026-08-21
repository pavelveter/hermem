# ADR-030: Multi-Tenancy Model

## Status

Proposed (scale trigger: Fortune 500 contracts, hosted offering, shared deployments)

## Context

The data model has **no tenancy dimension anywhere**:

- `entities`, `edges`, `beliefs`, `evidence`, `episodes`, `events`,
  `sessions`, `metrics_entity_access` — zero `tenant_id` columns.
- Auth is a static API-key list (`auth.Key{Value, Scope, Label}`) with
  three global scopes (read/write/admin). A key sees the whole graph.
- Every SQL query is unscoped (`WHERE archived = 0` is the only filter).
- The vector index holds all vectors in one flat namespace.
- `serverstate` hot-reload swaps one global config for the process.

Today the answer is "one deployment per customer" — viable for on-prem
Fortune 500 installs, but it blocks any shared/managed topology and makes
per-customer isolation, quotas, and data-residency unanswerable inside
one process.

## Why the current design breaks

1. **Retrofit cost is total.** Adding `tenant_id` later means: schema
   migration of every table, every hand-written query (~20 packages),
   the recursive-CTE walk, the vector index key space, the MCP tools,
   all 3 SDKs, and the plugin contract. This is the single most
   expensive retrofit identified in the audit.
2. **Compliance.** SOC 2 / ISO 27001 auditors require demonstrated
   tenant isolation; "separate SQLite file per customer on the same
   host" fails when a hosted tier exists.
3. **Noisy-neighbor / quotas.** No per-tenant rate limits, storage
   quotas, or cost attribution — the rate limiter keys by API key but
   quota accounting doesn't exist.
4. **Data lifecycle.** "Delete all of customer X's data" (GDPR/CCPA
   erasure) is a hand-rolled forensic procedure across 9 tables and an
   in-memory index today.

## Decision

Adopt **row-level tenancy with a scoped context**:

1. `tenant_id TEXT NOT NULL DEFAULT 'default'` on every domain table;
   composite indexes lead with `tenant_id`. Single-tenant deployments
   keep working unchanged on the `'default'` tenant.
2. Tenancy is carried in `context.Context` (`auth.Claims` →
   `ctxTenant`), injected by the auth middleware (ADR-036), and
   enforced **inside repository implementations** (ADR-027) — never by
   callers remembering a `WHERE` clause.
3. Vector namespaces (ADR-028) are `tenant + model` — per-tenant ANN
   partitions make filtered search and erasure cheap.
4. Per-tenant config overlays: rate limits, retention TTLs, ranking
   weights can override globals; `serverstate` gains a tenant-scoped
   overlay map instead of one global snapshot.
5. Tenant admin surface: `POST /admin/tenants`, per-tenant usage
   metrics, per-tenant export + hard delete (erasure job).

## Alternatives considered

1. **Schema-per-tenant / DB-per-tenant.** Strongest isolation, trivial
   erasure (drop schema). Rejected as the *only* mode: 5000 tenants ×
   per-schema migrations is operationally untenable, and the
   single-connection SQLite model can't pool across DBs. Kept as an
   option for regulated tiers via the same repo seam.
2. **Stay single-tenant forever.** Rejected: forfeits the hosted tier
   and any shared enterprise deployment; the retrofit only gets worse.
3. **Application-level filtering (no DB enforcement).** Rejected: one
   missed `WHERE` in one hand-written query is a cross-tenant leak —
   with raw SQL in 20 packages, it's when, not if.

## Tradeoffs

- **Every query gains a predicate and every write a column** — small
  latency/index cost per op; accepted, bounded by composite indexes.
- **Context-plumbing discipline** required everywhere; enforced by repo
  interfaces that refuse a missing tenant in non-default mode (fail
  closed).
- **Vector RAM grows per tenant** (per-tenant partitions); mitigated by
  quantization and tenant-tiered backends (small tenants share a
  filtered partition).
- **Migration of existing single-tenant DBs** is mechanical
  (`tenant_id='default'` backfill) but must be rehearsed; covered by
  ADR-033's data-migration support.

## Consequences

- Hosted/multi-customer topology becomes possible; Fortune 500 isolation
  questionnaires have a concrete answer.
- Erasure, export, and quota features have a single choke point.
- Hard dependency on ADR-027 (repo seam) — without it this ADR is a
  20-package rewrite instead of an implementation change.
- Auth must become claims-bearing (ADR-036); static keys map to tenants
  during the transition.
