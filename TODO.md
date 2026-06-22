# TODO: Review-Driven Hardening Batch

Scope: 7 items from the recent review (all approved by the user) EXCEPT
`SmartCosineSimilarity` (#4, deferred — needs M-series SIMD benchmarks
before committing to a hybrid threshold).

| #  | Item                              | Domain          | Source                  |
|----|-----------------------------------|-----------------|-------------------------|
| 13 | LLM JSON ```fence``` stripping    | Correctness     | `src/extractor.go`      |
| 10 | Ingestion race fix                | Concurrency     | `src/ingestion.go` + `src/vector.go` + `src/vector_inmemory.go` |
|  7 | CTE cycle handling                | SQL correctness | `src/retrieval.go`      |
|  6 | `createEdges` → bulk INSERT       | Performance     | `src/ingestion.go`      |
|  2 | `validCategories`/`validRelationTypes` → Config | Extensibility | `src/config.go` + `src/extractor.go` |
|  3 | `filterRelations` pre-count       | Micro-opt       | `src/extractor.go`      |
|  1 | DBPath symlink `slog.Debug`       | Polish          | `src/config.go`         |

(Excluded: #4 SmartCosineSimilarity, #5 BEGIN IMMEDIATE (theoretical),
 #8 Lost-in-the-Middle (would need composite-score rewrite), #9 tick
 stacking (false positive — Go `time.Ticker` drops excess ticks),
 #11/#12/#14 already addressed, #15 architectural observation only.)

---

## #13 LLM JSON cleanup — HIGH

**File:** `src/extractor.go`, tests `src/extractor_test.go`.

**Risk:** Without stripping, Ollama responses that wrap JSON in
` ```json ... ` ``` (very common) survive TrimSpace but fail
`json.Unmarshal`. Production run on Ollama without `format=json`
hardening sees ≥50% parse errors. OpenAI clients with
`response_format=json_object` are immune, but still drop defensive
coverage if a future model stops honoring it.

**Approach:** New helper `stripMarkdownCodeFence(s string) string`
that:
1. Iterates the string looking for ` ```json ` or generic ` ``` ` openings.
2. Captures up to the closing ` ``` ` (or end-of-string).
3. Returns the inner content (trimmed of leading/trailing whitespace).
- Apply in both `OllamaLLMExtractor.ExtractEntities` and
  `OpenAILLMExtractor.ExtractEntities` AFTER `TrimSpace`, BEFORE
  `json.Unmarshal`.
- Idempotent: bare JSON passes through unchanged.

**Test:** feed ` ```json\n{"entities":[]}\n` ` ` and plain JSON,
assert both produce equivalent `ExtractionResult`.

---

## #10 Ingestion race fix — HIGH

**Files:** `src/ingestion.go`, `src/vector.go`, `src/vector_inmemory.go`,
tests `src/ingestion_test.go`.

**Risk:** Today `StoreEntityWithEmbedding` writes SQLite first, then
updates `vi.Store` under a `Lock`. Two parallel ingesters can
commit SQLite in order A→B but update the in-memory index in order
B→A, corrupting byID→entries mapping for spot searches until the
DB is rebuilt.

**Approach:** Reverse the order:
1. Call `vi.Store(id, vec)` first — this acquires `idx.mu.Lock()`
   and inserts the row.
2. Perform SQLite INSERT.
3. If SQLite INSERT fails, call `vi.Remove([]string{id})` to roll
   back the index change. The window between "index has it" and
   "DB has it" is now bounded by SQLite commit latency (sub-ms);
   the inverse window is gone.
- Doc the new ordering prominently on `StoreEntityWithEmbedding` so
  future contributors don't re-introduce the bug.

**Test:** Race-detector test in `-race` mode. Spawn 10 goroutines,
each ingesting 100 distinct entities. After all complete, assert:
   - `len(idx.entries)` equals `SELECT COUNT(*) FROM entities`.
   - For every entry in `idx.entries`, `idx.byID[id] == position`.
   - All goroutines pass without race-detector warnings.

---

## #7 CTE cycle handling — MEDIUM

**File:** `src/retrieval.go`, doc test in `src/retrieval_test.go`.

**Risk:** The current recursive CTE uses `UNION ALL` and bounds depth
with `gw.depth < effectiveDepth`. Cycles A→B→A walk to depth
`DepthCeiling`, multiplying intermediate rows. Above 5 hops, the
intermediate temp-table can OOM SQLite. The Go-side `seenIDs` dedups
the FINAL result set, masking the bug.

**Approach:** Add an explicit path-visit guard inside the CTE itself.
Append a `path TEXT` column to the CTE; recursive branch refuses
to expand a row whose `target_id` is already in `gw.path`
(separator-based LIKE check). This bounds the work SQLite does
regardless of how Go later dedups the result.
- Existing `seenIDs`/`seenContents` Go-side dedups stay (defence in
  depth + faster because there's no SQL-level dedup step).

**Test:** Existing `TestRetrieveContextCycleGuard` keeps passing.
Add `TestRetrieveContextCycleComplex`: a 5-node graph with two
interleaving cycles, depth limit 8, asserts `num intermediate rows
<= ~20` and final result matches hand-computed unique set.

---

## #6 `createEdges` → bulk INSERT — MEDIUM

**File:** `src/ingestion.go`.

**Risk:** The current `createEdges` does ONE INSERT per relation. A
dialog producing 50 edges hits SQLite 50 times; each call is a
separate prepared statement and a write transaction. High-volume
ingest pipelines get throttled to SQLite commit IOPS.

**Approach:** Replace the per-relation loop with a single
multi-VALUES INSERT chunked through the `execInChunks` helper
introduced in 63e11f2. Skip duplicate target_ids within the same
batch (INSERT OR IGNORE handles DB-side dedup, but excluding
duplicates in app code prevents redundant args-list allocation).
- Inputs: same `(entityID, relations)` shape.
- Output: single `db.ExecContext` per chunk (no prepare/repeat).

**Test:** Add a unit test that calls `createEdges` for 50 relations
and asserts exactly 50 rows land in `edges`. Add a separate
test for cross-batch partitioning (e.g., 700 relations → two
EXEC calls).

---

## #2 Categories/relations → Config — MEDIUM-EXT

**Files:** `src/config.go`, `src/extractor.go`, `src/extractor_test.go`.

**Risk:** New operators who want a `meta` category or a `supports`
relation today have to fork the binary. The current lists in
`extractor.go` are package-level vars with no override mechanism.

**Approach:** Add `ExtraCategories []string` and
`ExtraRelationTypes []string` to `Config` (ini keys
`extraction.extra_categories` / `extraction.extra_relation_types`,
CSV-parsed).
- `NewOllamaLLMExtractor` / `NewOpenAILLMExtractor` accept two extra
  slices as parameters (after the existing 4). Wire the merged
  sets into the package-level maps at extractor construction
  (caller-owned lifetime — refusing to mutate the package-level
  map directly from concurrent ingests avoids race).
- `filterEntities` / `filterRelations` look up the merged set.
- Validation: every `Extra*` value passes a regex check
  (`^[a-z][a-z0-9_]*$`); invalid values are dropped at config-load
  with a `slog.Warn` so a typo doesn't quietly widen the schema.

**Test:** `TestLoadConfigParsesExtractionExtensibility` validates
the ini parsing. `TestFilterValidatesExtraCategories` extends
filterEntities with a custom category and asserts the entity
passes.

---

## #3 `filterRelations` pre-count — MICRO-OPT

**File:** `src/extractor.go`.

**Risk:** Tiny perf concern; minor code change. Cosmetic.

**Approach:** Pre-count valid relations in a single pass; allocate
`make([]Relation, 0, validCount)` exactly. For `filterEntities`,
same pattern — pre-count valid entities by category check, allocate
exactly. Skip the "if validCount == len(in) return in" fast path
because callers don't depend on identity preservation.

**Test:** No new test — existing filterEntities coverage already
asserts the result; this PR preserves behavior, only changes
allocation strategy. Re-run existing tests.

---

## #1 DBPath symlink logging — POLISH

**File:** `src/config.go`.

**Risk:** Cosmetic — operators sometimes can't find the DB file when
the binary is symlinked through `/usr/local/bin/`. Today the path
resolution is silent. Adding a debug log makes stray DB files
detectable without code changes.

**Approach:** When resolving DBPath from the binary's directory,
call `filepath.EvalSymlinks`; if the resolved path differs from
the raw path, emit `slog.Debug("db_path_symlink_resolved", "raw",
rawDir, "resolved", realDir, "db_path", dbPath)`. Keeps behavior
identical, just adds observability.

**Test:** No new test — existing `TestLoadConfigFromDir_*` exercises
the resolver. Manual verification: deploy via symlink, run with
`slog.LevelDebug`, confirm the event appears.

---

## Execution order

1. #13 LLM JSON cleanup (isolated, smallest blast radius).
2. #3 `filterRelations` pre-count (same file, no risk).
3. #6 bulk edges (uses existing `execInChunks` from 63e11f2).
4. #10 ingestion race fix (largest architectural change; needs
   race-detector test).
5. #7 CTE cycle handling (SQL surgery; sensitive).
6. #2 categories/relations → Config (touches extractor constructors
   and existing test calls).
7. #1 symlink logging (final polish).

Group by commit theme:
- **Round 6 — correctness / perf:**
  #13, #10, #6, #7.
- **Round 6 — extensibility / polish:**
  #2, #3, #1.

Two commits, each vet- and race-clean.

## Validation plan

After all 7 items:
- `gofmt -w src/*.go`
- `go vet ./src/...`
- `go test -count=1 -race -timeout 120s ./src/...` (race-detector,
  catches #10 fixes)
- Targeted runs for new tests:
   - `TestStripMarkdownCodeFence`
   - `TestIngestionRaceNoIndexMismatch`
   - `TestRetrieveContextCycleComplex`
   - `TestLoadConfigParsesExtractionExtensibility`
   - `TestFilterValidatesExtraCategories`

## Out of scope (deferred for later PRs)

- #4 SmartCosineSimilarity — needs M-series benchmarks; the 512-dim
  heuristic the reviewer suggested is a guess without measurement.
- #5 BEGIN IMMEDIATE — code doesn't currently use any transaction
  wrappers.
- #8 "Lost in the Middle" — composite-score re-order refactor;
  requires benchmark + design discussion.
- #9 tick stacking — false positive.
- #15 InMemory vs sqlite-vec — architectural; revisit when N>100k.
