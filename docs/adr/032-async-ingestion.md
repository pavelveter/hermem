# ADR-032: Asynchronous Ingestion Pipeline

## Status

Proposed (scale trigger: >100 dialogs/min sustained, LLM latency variance, multi-agent write loads)

## Context

The ingestion pipeline is **synchronous in the request path** and
hardcoded as a linear sequence
(`ingestion/dialog.go: ProcessDialogWithProvenance`):

```
LLM Extract (up to 5 min timeout)
  → Embed each entity — SEQUENTIALLY, one HTTP call per entity
  → SearchBatch (dedup candidates)
  → per item: contradiction check → merge/create in its own SQLite tx
      → post-commit vector-index ops (drift logged as "vi_drift")
```

Characteristics observed:

- A 20-entity dialog = 1 extraction + **20 sequential embed round-trips**
  + 20 individual transactions, all inside the HTTP request.
- `SQLITE_BUSY` retries with exponential backoff (`processOneItem`) —
  lock contention handled by sleeping, not by queueing.
- HTTP write timeout is 120 s; extraction timeout alone is 300 s — the
  client can time out while the server keeps working (zombie writes).
- The only durability artifact is the MCP `pending.jsonl` drain file —
  an ad-hoc, single-consumer spool; HTTP /ingest has none.
- Contradiction detection runs inline, coupling ingest latency to
  detector latency.

## Why the current design breaks

1. **Throughput ceiling.** Sequential per-entity network calls and
   per-entity transactions put a hard ceiling of roughly
   `(entities/dialog) / (embed RTT + tx time)` dialogs per second —
   with a 50 ms embed RTT, one dialog with 10 entities ≥ 500 ms of pure
   network time. Multi-agent fleets (the target market) generate
   hundreds of dialogs per minute.
2. **No backpressure.** Under load, requests pile up holding goroutines
   and HTTP connections until the 120 s timeout; there is no 202/queue
   semantics, no shed, no priority.
3. **Partial failure = silent loss.** A mid-pipeline crash leaves some
   entities stored and the rest gone; the client already got its 200 (or
   a timeout — it can't tell which).
4. **Latency coupling.** Inline contradiction detection and dedup mean
   ingest p99 is dominated by the slowest sub-system; none of it is
   retryable independently.
5. **Unbatchable.** Embedders with batch APIs (OpenAI: 2048 inputs/call)
   are used one string at a time — 10–50× cost/latency waste.

## Decision

Introduce a **queued, staged ingestion pipeline**:

1. **Write path:** `POST /ingest` validates + enqueues a durable job
   (SQLite `ingest_jobs` table for embedded mode; pluggable broker
   interface for Kafka/NATS in server mode) and returns `202` with a
   `job_id`. Sync mode remains as an explicit option
   (`?sync=true`) for the CLI and tests.
2. **Worker pool** (N goroutines, sized by config) drains jobs through
   explicit stages: `extract → embed(batch) → dedup → resolve → commit`.
   Each stage has its own timeout, retry policy (exponential + jitter,
   per ADR-011), and dead-letter queue after N attempts.
3. **Batch embedding:** the embed stage groups up to B contents per
   provider call; the `Embedder` SPI gains `EmbedBatch` (ADR-031) with
   a default loop fallback.
4. **Idempotency keys:** jobs carry `conversation_id+message_id` or a
   client key; re-delivery is deduplicated at commit time.
5. **Status surface:** `GET /ingest/{job_id}` →
   queued/running/done/failed + per-entity outcomes; MCP tools expose
   the same.
6. **Contradiction detection** moves to an async post-commit stage that
   emits `contradicts` edges; ingest latency decouples from detector
   latency.

## Alternatives considered

1. **Keep sync, add goroutine fan-out inside the request.** Rejected:
   improves embed latency but keeps zero durability, no backpressure,
   and zombie-write semantics.
2. **External broker only (Kafka/NATS required).** Rejected: violates
   the single-binary identity; broker must be optional, not required.
3. **Channel-based in-memory queue only.** Rejected: process crash =
   lost dialogs; the `pending.jsonl` lesson already taught this.
4. **Full event-sourced write model.** Deferred: powerful but a
   rewrite of read paths; the job table gives 80% of durability with
   5% of the complexity.

## Tradeoffs

- **Read-your-writes changes:** clients must poll job status or accept
  eventual consistency (target <2 s p99 empty-queue). Mitigation:
  `?sync=true` escape hatch + SDK helpers that poll.
- **Operational surface:** one more table + worker metrics + DLQ
  tooling (`hermem ingest dlq list/replay`).
- **Ordering:** dialogs from one conversation are processed in submit
  order via per-conversation key partitioning; cross-conversation
  parallelism is explicit, not accidental.
- **Duplicate suppression** adds a unique index on idempotency keys —
  small storage cost per job (jobs are pruned after TTL).

## Consequences

- Ingest throughput becomes worker-count × batch-size, not RTT-bound.
- LLM/embedder outages degrade to queue growth, not 5xx storms.
- Retry/DLQ makes poison dialogs visible instead of silently dropped.
- Enables priority lanes (interactive MCP writes > bulk import).
- The pipeline stages become the plugin seams for custom
  extractors/enrichers (ADR-029).
