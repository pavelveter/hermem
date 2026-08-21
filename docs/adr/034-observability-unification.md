# ADR-034: Observability Unification — OpenTelemetry + Audit Log

## Status

Proposed (scale trigger: Fortune 500 compliance, multi-node operations, on-call at scale)

## Context

Observability today is three partial systems plus a missing fourth:

1. **Metrics** (`metrics/metrics.go`, 547 lines): a hand-maintained
   `Metrics` struct with ~20 `atomic.Int64` counters mirrored by ~20
   Prometheus counters. Adding one counter requires **7 coordinated
   edits** (documented in the file's own cheat-sheet), plus a
   hand-written `WriteExposition` text encoder. Label value-sets are
   hardcoded (`knownCategories`, `knownModes`, ...) and drift from the
   *configurable* schema (`ExtraCategories` never appear).
2. **Tracing** (`tracing/`): a custom `Tracer`/`Span` interface with a
   `NoopTracer` default. `TRACING_EXPORTER=otlp` prints a log line and
   **falls back to Noop** (`otlp.go` — the OTLP SDK was never wired).
   No HTTP middleware creates spans; `X-Request-ID` is a raw
   `UnixNano`, not a W3C trace context; no propagation to outgoing AI
   provider calls.
3. **Logging:** global `slog.Default()` everywhere — no request-scoped
   logger, no trace/span IDs in log lines, no per-module levels, no
   sampling for hot paths.
4. **Audit: absent.** Nothing records *who* read/wrote *which* memory
   when. `metrics_entity_access` is a best-effort access counter
   (dropped when the channel is full) — not an audit trail.

## Why the current design breaks

1. **Compliance.** Fortune 500 procurement (SOC 2 Type II, ISO 27001,
   often HIPAA/PCI adjacent) requires tamper-evident audit logs of data
   access. "We drop access events when busy" is an automatic fail.
2. **No distributed tracing = no debugging across boundaries.** At
   5000 deployments, support tickets are "retrieval is slow" — today
   there is no way to see whether time went to the embedder, the walk,
   or SQLite. The Noop OTLP stub means even the existing spans go
   nowhere.
3. **Hand-rolled exposition is a correctness hazard.** A bespoke
   Prometheus text encoder will eventually emit invalid format (label
   escaping, HELP/TYPE duplication) and break scrapes fleet-wide.
4. **Metrics contribution friction.** 7-edit-point counters guarantee
   contributors either skip instrumentation or get it wrong; coverage
   decays as the codebase grows.
5. **Cardinality drift.** Hardcoded label sets silently misrepresent
   deployments using `ExtraCategories`/`ExtraRelationTypes`.

## Decision

1. **Adopt OpenTelemetry Go SDK** as the single instrumentation API:
   - Metrics: OTel Meter with Prometheus exporter (and OTLP push option);
     the hand-written `WriteExposition` is deleted.
   - Traces: real OTLP exporter (env-configured, off by default);
     `tracing.Tracer` becomes a thin alias over `otel.Tracer`.
   - Logs: keep `slog`, but route through a handler that injects
     `trace_id`/`span_id` from context.
2. **Span conventions:** HTTP middleware creates the root span
   (W3C `traceparent` parse/generate; `X-Request-ID` mirrors trace ID);
   spans for `embed`, `extract`, `vector.search`, `graph.walk`,
   `db.tx`, and every outbound provider call (with
   `deployment.environment`, `hermem.version` resource attrs).
3. **Metric helper:** one-line `metrics.Counter("ingest_entities_total",
   "category")` registration API backed by the Meter — the 7-edit
   cheat-sheet is replaced by a call-site.
4. **Audit log (new subsystem):** append-only `audit_events` table
   (WAL-chained hash for tamper evidence: each row stores
   `prev_hash`/`hash`) capturing actor (API key label / OIDC subject),
   action, resource IDs, tenant (ADR-030), timestamp, and request ID.
   Written synchronously on mutation paths, sampled on read paths with
   an operator toggle. Export: `hermem audit export --since ...`
   (JSONL) plus an OTLP log-appender for SIEM forwarding.
5. **Cardinality policy stays**, but label sets derive from the
   *effective* schema config at boot, not hardcoded slices.

## Alternatives considered

1. **Prometheus client only, keep custom tracer.** Rejected: the custom
   tracer is already vaporware; maintaining a private tracing API
   duplicates OTel badly.
2. **Vendor-specific agents (Datadog SDK etc.).** Rejected: OTel is the
   vendor-neutral standard and covers all three signals.
3. **Audit via DB triggers.** Rejected: triggers can't see the actor
   (app-level identity) and are invisible to the migration ledger;
   hash-chaining must live in app code anyway.
4. **Full structured-event bus (every op emits an event).** Deferred:
   powerful for replay/debugging, but a much larger commitment; the
   audit log is the compliance-mandated subset.

## Tradeoffs

- **Dependency weight:** OTel SDK + exporters ≈ +8–10 MB binary and a
  bigger dependency tree — real cost for a CLI; mitigated by build tags
  (`otel` on by default for server builds, off for a `lite` build).
- **Span overhead:** ~1–2 µs/span noop when disabled; sampling (default
  1–5% head-based) keeps hot paths cheap.
- **Audit write amplification:** one extra append per mutation;
  acceptable since the audit append is a single indexed insert —
  negligible next to embedding work.
- **Migration burden:** every existing `m.IncXxx()` call-site moves to
  the helper — mechanical, grep-able, one release with the old struct
  kept as a facade.

## Consequences

- Fleet operators get standard dashboards/traces in any OTel backend
  (the existing `ops/grafana` dashboard continues via the Prometheus
  exporter).
- Compliance questionnaires have concrete answers (audit trail,
  tamper evidence, retention).
- Instrumentation cost per new feature drops from 7 edits to 1 line.
- Support can correlate a slow request to its embedder/walk/DB spans.
