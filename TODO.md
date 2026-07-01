# Project Hardening & Engineering Roadmap

Goal: bring this project to the level of a production-grade open-source Go project.

## General rules

- Every task MUST be implemented in a separate commit.
- Every task MUST be fully verified before committing.
- Never combine multiple roadmap items into one commit.
- Every change MUST include appropriate tests.
- Existing tests must continue to pass.
- New functionality MUST include unit tests.
- Public API changes MUST include regression tests.
- Performance-sensitive changes SHOULD include benchmarks.
- Serialization/parsing changes SHOULD include fuzz tests.
- CI must stay green after every commit.
- Run all relevant linters before committing.
- Do NOT push anything to the remote repository.
- Stop after each completed task and create exactly one local git commit.
- Wait for my approval before starting the next task.
- Never squash commits.
- Never push unless I explicitly instruct you to do so.

---

## C — Critical

^- [x] **C1 — Fix OpenAPI spec discrepancies**
  Five routes are mismatched between the live server and the OpenAPI spec —
  `/task/claim-next`, `/ingest/jobs`, `/admin/retention/run` are served but
  missing from the spec; `/query/temporal` is in the spec but has no handler
  registered. Every discrepancy is a correctness bug visible to API consumers.
  - [x] Add `/task/claim-next` to OpenAPI spec.
  - [x] Add `/ingest/jobs` to OpenAPI spec.
  - [x] Add `/admin/retention/run` to OpenAPI spec.
  - [x] Either implement `handleQueryTemporal` HTTP handler or remove
    `/query/temporal` from the spec.
  - [x] Update `docs/generated/ROUTES.md` after fixing all discrepancies.
  - [x] Verify with `go test -run 'Test(Spec|Paths|Schemas|Snapshot)' ./api/...`
  - [x] Commit separately.

^- [x] **C2 — Implement missing `/query/temporal` HTTP handler**
  The OpenAPI spec defines `POST /query/temporal` and the CLI `hermem time temporal`
  works, but no HTTP handler is registered in the retrieval HTTPService shell.
  - [ ] Add `handleQueryTemporal` to `server/retrieval/retrieval_service.go`
  - [ ] Register the route in `Routes()`.
  - [ ] Add end-to-end HTTP test in `tests/e2e/http/`.
  - [ ] Commit separately.

^- [x] **C3 — Fix hardcoded Docker image version**
  `docker-compose.yml` pins `hermem:0.2.0` as the image tag. Every release
  requires a manual bump. Use an env var (`HERMEM_VERSION`) or generated
  `.env` file so `docker compose up` always runs the version matching the tag.
  - [ ] Replace hardcoded `image: hermem:0.2.0` with `image: hermem:${HERMEM_VERSION:-latest}`.
  - [ ] Document `HERMEM_VERSION` in README or docker-compose comments.
  - [ ] Commit separately.

^- [x] **C4 — Add benchmark baseline file**
  CI benchmark regression gate (`bench.yml`) depends on `bench/baseline/baseline.txt`
  but the file does not exist — only `README.md` in that directory. Without a
  baseline, there is no regression detection. Run benchmarks on current `main`
  and commit the baseline.
  - [ ] Run `go test -bench=. -benchmem -count=6 -run='^$' ./src/... > bench/baseline/baseline.txt`
  - [ ] Verify baseline file is non-empty.
  - [ ] Commit separately.

- [x] **C5 — Add rate limiting middleware**
  The HTTP server has no rate limiting. A single client can saturate the
  SQLite write path or exhaust LLM provider quotas via `/ingest`. Production
  deployments need at least a simple token-bucket or sliding-window limiter.
  - [x] Design a `core.RateLimiter` interface (token bucket or sliding window).
  - [x] Implement an in-memory rate limiter (per-key or per-IP).
  - [x] Add `[server] rate_limit` config section (`requests_per_second`, `burst`).
  - [x] Wire rate-limit middleware into the server middleware chain.
  - [x] Return `429 Too Many Requests` with `Retry-After` header.
  - [x] Add unit tests for the limiter and middleware.
  - [x] Commit separately.

---

## H — High

^- [x] **H1 — Add `HERMEM_INI` env-var override and `--config` flag**
  Currently `hermem.ini` is resolved exclusively from the binary directory
  (`os.Executable()`). Operators who need one binary shared across multiple
  deployments (e.g. `/usr/local/bin/hermem` with different DBs) must copy
  the binary. Both the env-var and flag are explicitly noted as TODO items
  in existing comments.
  - [ ] Add `--config <path>` cobra persistent flag.
  - [ ] Add `HERMEM_INI` env-var resolution.
  - [ ] Precedence: flag > env-var > binary-dir-relative.
  - [ ] Update `config.LoadConfig` / `LoadConfigFromBinaryDir` to support.
  - [ ] Add tests for precedence chain.
  - [ ] Commit separately.

- [ ] **H2 — Increase SDK test coverage**
  Go SDK has only `client_test.go` (unit tests for version mismatch, API
  error formatting, client construction). No integration tests that exercise
  actual HTTP round-trips against a real server. Python and TypeScript SDKs
  have minimal tests.
- [ ] Add Go SDK integration tests: store → search → retrieve against a
  running server. (deferred — design decision in the H2 review was to
  use `httptest.NewServer` mocks rather than spin up a real server in
  CI. Rationale: avoids port collisions, CGO embedding requirements,
  and the 2-5s hermem-server startup cost. A follow-up could mount
  the server HTTP handler in-process via a `replace` directive in
  sdk/go/go.mod pointing to ../..; see commit 21d2180 follow-up list.)
- [x] Add Go SDK tests for `memory`, `task`, `graph`, `admin` sub-clients.
  (commit 21d2180. helpers_test.go + 4 sub-client test files; 37 new
  tests, 72.1% line coverage.)
- [x] Evaluate Python SDK test coverage; add missing tests. (commit
  21d2180. conftest.py + 4 sub-client test files; 33 new tests
  including new `test_memory_retrieve`/`test_memory_explain` that
  exposed and fixed a latent NameError in `_parse_retrieval_result`.)
- [x] Evaluate TypeScript SDK test coverage; add missing tests. (commit
  21d2180. helpers.ts + 4 sub-client test files; 35 new tests.)
- [x] Commit separately. (commit 21d2180.)

- [x] **H3 — Add SDK E2E tests to CI**
  SDKs are released as part of the same tag but are not E2E-tested in CI.
  A breaking server change can ship with a broken SDK.
  - [x] Add Go SDK E2E workflow to CI (start server, run SDK tests).
  - [x] Add Python SDK E2E workflow to CI.
  - [x] Add TypeScript SDK E2E workflow to CI.
  - [x] Wire into release workflow as a gate.
  - [x] Commit separately.

^- [x] **H4 — Add reranker to health readiness probe**
  The `/health/ready` endpoint checks database, vector index, embedder, LLM
  extractor, and disk space — but not the reranker. If the reranker is
  configured but unreachable, the server reports healthy while silently
  skipping reranking on every retrieval.
  - [ ] Add `reranker` check to `health/probes.go`.
  - [ ] Wire into the readiness probe with `critical: false` (opt-in dep).
  - [ ] Add test in `health/service_test.go`.
  - [ ] Commit separately.

^- [x] **H5 — Add structured error events to slog for AI provider calls**
  Currently AI provider failures (`ollama_call`, OpenAI API errors) log at
  Debug level. Production operators cannot monitor LLM provider health
  without enabling Debug-level logs (which flood the output). Add
  dedicated ERROR/WARN events with structured fields.
  - [ ] Add `ai_call_failed` slog event at ERROR level with fields:
    `provider`, `model`, `status_code`, `attempts_used`, `latency_ms`.
  - [ ] Add `ai_call_retry` slog event at WARN level during retry loops.
  - [ ] Keep existing Debug-level details; add the new events
    unconditionally.
  - [ ] Commit separately.

^- [x] **H6 — Eliminate CLI dependency on deprecated `NewServer()` constructor**
  `server/server.go` still exports the old 14-parameter `NewServer()` alongside
  the newer `NewServerFromDeps(ServerDeps)`. The old constructor is deprecated
  but not removed. Every new server shell addition requires updating both.
  - [ ] Audit all callers of `NewServer()`.
  - [ ] Migrate remaining callers to `NewServerFromDeps()`.
  - [ ] Remove `NewServer()`.
  - [ ] Commit separately.

^- [x] **H7 — Add missing `go.sum` entries for SDK test-only deps**
  `sdk/typescript/package.json` specifies `vitest` but there's no lockfile
  in the repo. SDK CI may have non-deterministic builds. Evaluate if
  lockfiles (`package-lock.json`, `poetry.lock`) should be committed.
  - [x] Check if TypeScript tests pass in CI (currently they do not run).
        (now wired via `npm ci --no-audit --no-fund` + `npx vitest run` in
        `.github/workflows/sdk.yml::typescript-sdk`; locally 10/10 pass.)
  - [x] Commit lockfile for TypeScript SDK. (`sdk/typescript/package-lock.json`,
        lockfileVersion 3, generated via `npm install --package-lock-only`.)
  - [x] Commit lockfile for Python SDK if applicable. (chose `uv.lock` over
        `requirements*.txt` — `uv 0.9` honors `setuptools` build backend
        directly. `uv sync --extra=dev` reads the lockfile deterministically.)
  - [x] Commit separately.

---

## M — Medium

^- [x] **M1 — Generate and commit shell completions**
  The CLI has a `hermem completion [bash|zsh|fish]` command but completions
  are not pre-generated or packaged. Add generated completions to release
  artifacts and optionally install them via `make install`.
  - [x] Generate completions for bash, zsh, fish.
  - [x] Add `completions/` directory with generated files.
  - [ ] Include in release archive (`make install` should install them).
  - [x] Add `--install-completions` flag or `make install-completions` target.
  - [x] Commit separately.

^- [x] **M2 — Add test coverage reporting to CI**
  CI runs all tests but does not collect or report coverage. Add
  `go test -coverprofile` and upload coverage data.
  - [ ] Add `-coverprofile=coverage.out` to CI test job.
  - [ ] Add coverage threshold gate (e.g. ≥70% for new PRs).
  - [ ] Generate coverage badge for README.
  - [ ] Commit separately.

^- [x] **M3 — Write a proper `docs/ROADMAP.md`**
  `README.md` references `ROADMAP.md` but the file does not exist. The
  bottom of the README lists a few roadmap ideas. Extract those into a
  dedicated page with more detail and status tracking.
  - [ ] Create `docs/ROADMAP.md` with detailed feature plans.
  - [ ] Include status indicators (planned / in-progress / shipped).
  - [ ] Link from README.
  - [ ] Commit separately.

^- [x] **M4 — Flush out `docs/VISION.md` beyond a checklist**
  `docs/VISION.md` is a bulleted checklist with no narrative, rationale,
  or prioritization. Expand into a forward-looking design document.
  - [ ] Add narrative context: why each pillar matters.
  - [ ] Add inter-pillar dependencies (e.g. Belief Engine enables
    Contradiction Engine).
  - [ ] Add rough timeline or ordering guidance.
  - [ ] Commit separately.

^- [x] **M5 — Replace `sortStrings` bubble sort with `slices.Sort`**
  `config/ini.go::sortStrings` implements a manual bubble sort. Replace
  with `slices.Sort` (stdlib since Go 1.21) for correctness and clarity.
  - [ ] Replace `sortStrings` with `slices.Sort`.
  - [ ] Verify `SortedKeys` output is identical.
  - [ ] Commit separately.

^- [x] **M6 — Add `CODEOWNERS` file**
  No `CODEOWNERS` file in the repo. For an open-source project with
  multiple contributors, this is standard.
  - [ ] Create `.github/CODEOWNERS` with sensible defaults.
  - [ ] Commit separately.

^- [x] **M7 — Add `.editorconfig`**
  No `.editorconfig` file. Ensures consistent indentation, line endings,
  and charset across editors.
  - [ ] Create `.editorconfig` with Go-standard settings.
  - [ ] Commit separately.

^- [x] **M8 — Fix `go.mod` Go version**
  `go.mod` declares `go 1.26.4` — this is likely intentional (the project
  uses Go 1.24+ features) but `1.26.x` has not shipped yet as of project
  start. Double-check and peg to the actual minimum required version.
  - [ ] Verify minimum Go version required by all direct dependencies.
  - [ ] Set `go` directive to the correct minimum version.
  - [ ] Re-run `go mod tidy`.
  - [ ] Commit separately.

^- [x] **M9 — Audit `AGENTS.md` language**
  `AGENTS.md` is written entirely in Russian. For a public open-source
  project, this creates a barrier for international contributors. Either
  translate to English or provide a bilingual version.
  - [x] Translate `AGENTS.md` to English. (commit 82c806a. Title swapped
        from "Bastion Bot Workflow" to "Hermem Contributor Workflow";
        direct/imperative voice preserved; commit-message examples
        translated; code blocks preserved verbatim.)
  - [x] Keep Russian version as `AGENTS.ru.md` if desired. (body
        preserved byte-for-byte from the source; bilingual "Язык /
        Language" preamble points to AGENTS.md as the English
        canonical.)
  - [x] Commit separately. (commit 82c806a.)

  Follow-up (not in this task): add markdown link validation to
  pre-push hook / CI so the hand-rolled `[docs/andrey-karpathy-skills.md]`
  link in AGENTS.md is caught on move/rename.

---

## L — Low

^- [x] **L1 — Add CodeQL static analysis to CI**
  GitHub's CodeQL is free for public repos. It catches security
  vulnerabilities (SQL injection, path traversal, etc.) that `gosec`
  and `golangci-lint` may miss.
  - [ ] Add `.github/workflows/codeql.yml`.
  - [ ] Verify it passes on `main`.
  - [ ] Commit separately.

^- [x] **L2 — Add OpenSSF Scorecard to CI**
  Scorecard evaluates supply-chain security posture (signed releases,
  branch protection, dependency updates, fuzzing, etc.) and produces
  a public score. Good for open-source credibility.
  - [ ] Add `.github/workflows/scorecard.yml`.
  - [ ] Add Scorecard badge to README.
  - [ ] Commit separately.

^- [x] **L3 — Add `go.work` for multi-module workspace**
  The repo has three Go modules: root (`go.mod`) and `sdk/go/go.mod`.
  A `go.work` file enables seamless cross-module development.
  - [ ] Create `go.work` with `use .` and `use ./sdk/go`.
  - [ ] Verify `go test ./...` works across both modules.
  - [ ] Add `go.work.sum` to `.gitignore` (or commit it).
  - [ ] Commit separately.

^- [x] **L4 — Add devcontainer configuration**
  No `.devcontainer/` config. This helps new contributors get started
  instantly via GitHub Codespaces or VS Code Dev Containers.
  - [ ] Create `.devcontainer/devcontainer.json` with Go + SQLite setup.
  - [ ] Include golangci-lint, shell completions.
  - [ ] Commit separately.

^- [x] **L5 — Fix `AGENTS.md` references to non-existent file**
  `AGENTS.md` line 5: "СНАЧАЛА ПРОЧИТАТЬ ПРАВИЛА ИЗ andrey-karpathy-skills.md".
  This file does not exist in the repo and is gitignored. Either add the
  referenced file or remove the reference.
  - [ ] Remove or fix the dangling reference.
  - [ ] Commit separately.

^- [x] **L6 — Add CHANGELOG generation automation**
  Release workflow does not auto-generate `CHANGELOG.md`. Manual
  maintenance is error-prone.
  - [ ] Add `git-cliff` or similar changelog generator config.
  - [ ] Wire into release workflow.
  - [ ] Commit separately.

^- [x] **L7 — Add `go.mod` version for SDK**
  `sdk/go/go.mod` declares `go 1.21` but the main module uses `1.26.4`.
  Consider bumping for consistency or documenting the deliberate minimum.
  - [ ] Verify the minimum Go version actually needed by SDK.
  - [ ] Bump or document.
  - [ ] Commit separately.

^- [x] **L8 — Add graceful degradation for AI provider timeouts**
  When the embedder or extractor times out, the entire request fails.
  For search/retrieval, consider falling back to pure keyword matching
  or returning cached results instead of 5xx.
  - [ ] Design a graceful degradation strategy.
  - [ ] Implement for `/search` and `/query` paths.
  - [ ] Add tests.
  - [ ] Commit separately.

^- [x] **L9 — Document `go.sum` database**
  `go.sum` is committed but the Go checksum database (`sum.golang.org`)
  conventions are not documented. Add a line to `CONTRIBUTING.md`.
  - [ ] Add note about `GONOSUMCHECK` / `GONOSUMDB` if applicable.
  - [ ] Commit separately.

---

## A — Architecture

- [ ] **A1 — Eliminate `applicationToEnv` adapter**
  `main.go` constructs `*app.Application` and immediately converts it to
  `*clienv.Env` via the `applicationToEnv()` adapter. The adapter is marked
  as transitional. All CLI commands still accept `*clienv.Env` instead of
  `*app.Application`. Finish the migration so only one DI container exists.
  - [ ] Migrate CLI commands to accept `*app.Application` (or a narrow
    interface).
  - [ ] Remove `applicationToEnv()` from `main.go`.
  - [ ] Remove `*clienv.Env` as the primary CLI dependency.
  - [ ] Commit separately (over multiple commits, one per command group).

- [ ] **A2 — Extract shared HTTP client from `ai/http.go`**
  `ai/http.go` implements a `ResilientClient` with retry budget, backoff,
  and circuit-breaking logic. This client is useful beyond AI — the SDK
  clients, health probes, and future external integrations could reuse it.
  - [ ] Extract `ResilientClient` into `httputil/` or a new `httpclient/` package.
  - [ ] Update all AI client implementations.
  - [ ] Add tests for extracted client.
  - [ ] Commit separately.

- [ ] **A3 — Standardize Retriever interface across pipeline stages**
  `core.Retriever` is implemented by `retrieval.Service` but is only used
  in `app.Application` for construction. The `retrieval.Service` has methods
  not on the interface (`Query`, `Explain`, `Temporal`). Consider making
  `Retriever` match the full service surface or create sub-interfaces.
  - [ ] Audit all callers of `retrieval.Service`.
  - [ ] Define `Retriever` interface to match all public methods or split
    into `Searcher`/`Retriever`/`Queryer` sub-interfaces.
  - [ ] Update `app.Application` accordingly.
  - [ ] Commit separately.

- [ ] **A4 — Decouple schema validation from config loading**
  `config.SchemaConfig` carries both validation rules and state machine
  config. Schema validation logic is spread across `config.Validate()`,
  `config.ValidateCategory()`, `config.ValidateRelation()`, `config.ValidateState()`,
  and `store/schema.go`. Consider centralizing schema concerns into a
  dedicated `schema` package.
  - [ ] Create `src/internal/schema/` package.
  - [ ] Move `SchemaConfig`, `DefaultSchemaConfig`, `ValidateSchema`,
    `ParseSchemaSection` from `config` and `core`.
  - [ ] Update all imports.
  - [ ] Commit separately.

- [ ] **A5 — Reduce `config.Config` surface area**
  `config.Config` carries 40+ fields covering embedder, extraction,
  database, server, vector, ingestion, retrieval, retention, ranking,
  reranker, and schema. It's the "god config object." Split into
  focused sub-configs with dedicated validation.
  - [ ] Split into `EmbedderConfig`, `DatabaseConfig`, `RetrievalConfig`, etc.
  - [ ] Each sub-config gets its own `Validate()` method.
  - [ ] `Config` composes them.
  - [ ] Commit separately over multiple commits.

---

## T — Tooling

^- [x] **T1 — Add pre-commit hook (in addition to pre-push)**
  `.githooks/pre-push` exists but there is no pre-commit hook. Fast checks
  (gofmt, go vet) should run at commit time; slow checks (E2E, golangci-lint)
  at push time. This catches formatting issues before they reach a commit.
  - [ ] Create `.githooks/pre-commit` with `gofmt -d .` and `go vet ./src/...`.
  - [ ] Document in `AGENTS.md` and `CONTRIBUTING.md`.
  - [ ] Commit separately.

^- [x] **T2 — Add local development helper (`make dev`)**
  No `make dev` target. Add a target that starts the server with hot-reload
  (via `air` or `gow`) for local development.
  - [x] Add `.air.toml` or equivalent config. (modern schema:
        `build.entrypoint = ["./hermem", "serve"]`; replaces the deprecated
        `build.bin` + `build.args_bin` pair. `pre_cmd` runs the embed
        placeholder so `go:embed` is satisfied on cold rebuild. Excludes
        `docs/`, `completions/`, `tests/`, `.db`, `*.ini` from rebuild
        triggers; `delay=1000` + `send_interrupt=true` give SQLite WAL a
        clean checkpoint window.)
  - [x] Add `make dev` target. (exits 1 with an install hint if `air`
        is missing on `$PATH`; does NOT auto-install to avoid silently
        mutating the developer's `$GOBIN`.)
  - [x] Document in `CONTRIBUTING.md`.
  - [x] Commit separately.

^- [x] **T3 — Add `make fmt` and `make vet` targets**
  `Makefile` has `make lint` (golangci-lint) but no isolated `make fmt`
  or `make vet` targets. Useful for quick pre-commit checks without the
  full linter.
  - [x] Add `make fmt` (`gofmt -s -w .`). (already in `Makefile`;
        verified idempotent and clean on current `main`.)
  - [x] Add `make vet` (`go vet ./...`). (already in `Makefile` as
        `go vet ./src/...` — narrower than the spec's `./...`; kept
        this scope deliberately so it matches the pre-push hook
        (`go vet ./src/...`), `magefile.go::Vet` (`sh.RunV("go",
        "vet", "./src/...")`), and the T1 pre-commit hook design.
        Switching to `./...` here would create a one-off divergence
        from every other vet invocation in the project.)
  - [x] Commit separately. (T3 was implemented on `main` before the
        M9/T10 work — the implementation commit is not in this
        conversation; this commit just flips the TODO sub-tasks.)

^- [x] **T4 — Add `make test-coverage` target**
  No single target to view test coverage locally.
  - [x] Add `make test-coverage` (`go test -coverprofile=coverage.out
        ./src/... && go tool cover -html=coverage.out`). (already in
        `Makefile`; same `./src/...` scope rationale as T3 above; the
        final `echo "Coverage report: coverage.html"` line points the
        developer at the HTML report, matching the equivalent
        `magefile.go::TestCoverage` target body for cross-runner
        parity.)
  - [x] Commit separately. (T4 implementation predates this
        conversation; this commit just flips the TODO sub-tasks.)

^- [x] **T5 — Add Dependabot/Renovate for Go modules**
  `.github/dependabot.yml` exists but only covers GitHub Actions. Go
  module dependencies are not auto-updated. Add Dependabot config for
  `go.mod` in root and `sdk/go/go.mod`.
  - [ ] Add `gomod` ecosystem entries to `dependabot.yml`.
  - [ ] Set schedule (weekly) and open-pull-requests-limit.
  - [ ] Commit separately.

^- [x] **T6 — Add `Dockerfile` linting to CI**
  `Dockerfile` uses `golang:1.24-alpine` (Go version mismatch with
  `go.mod`'s `1.26.4`). Add hadolint or dockerlint to CI to catch
  version mismatches and best-practice violations.
  - [ ] Add hadolint to CI workflow.
  - [ ] Fix any existing violations.
  - [ ] Commit separately.

- [ ] **T7 — Add `SLSA` provenance generation for releases**
  Release artifacts should include SLSA provenance (build attestation)
  for supply-chain integrity. GitHub's `slsa-github-generator` supports
  Go projects.
  - [ ] Add SLSA provenance job to release workflow.
  - [ ] Verify provenance is attached to release assets.
  - [ ] Commit separately.

^- [x] **T8 — Add `hermem.ini` validation command**
  Operators currently discover config errors only at server startup.
  Add `hermem config validate` / `hermem config test` command that
  parses and validates `hermem.ini` without starting the server.
  - [ ] Add `hermem config validate [--path <file>]` CLI command.
  - [ ] Exit 0 on valid, exit 1 with structured error on invalid.
  - [ ] Commit separately.

^- [x] **T9 — Add `hermem config show` with defaults**
  Operators cannot see the effective config (defaults + overrides)
  without starting the server and reading logs. Add a command that
  prints the resolved config.
  - [ ] Add `hermem config show [--path <file>]` CLI command.
  - [ ] Print effective config in INI format with comments marking
    overridden defaults.
  - [ ] Commit separately.

^- [x] **T10 — Add `mage` or `task` as alternative to `make`**
  `Makefile` works on macOS/Linux but not on Windows natively.
  Consider adding a `magefile.go` (Go-based task runner) for
  cross-platform development.
  - [x] Add `magefile.go` with equivalents of `make build`, `make test`,
    `make lint`. (15 targets: Build, BuildLocal, BuildNoLocal, Test,
    TestE2E, Benchmarks, Lint, Fmt, Vet, TestCoverage, Clean, Install,
    Completions, InstallCompletions, Dev. Gated by `//go:build mage`
    so it never reaches the production binary; `path/filepath.Join`
    used everywhere instead of `string(os.PathSeparator)`; Default
    intentionally unset so bare `mage` errors instead of compiling.)
  - [x] Document in `CONTRIBUTING.md`. (new "Mage (cross-platform
    build runner)" subsection after Hot reload.)
  - [x] Commit separately. (commit 79479c6.)

  Note: `magefile/mage` added to `go.mod` so the `mage` build tag
  compiles the file.


---

## Final verification

- [ ] Run `gofmt -s -w .` and verify clean.
- [ ] Run `go vet ./...` and verify clean.
- [ ] Run `golangci-lint run ./...` and fix any issues.
- [ ] Run `go test -race -count=1 -timeout 10m ./src/...` — all pass.
- [ ] Run `go test -p 1 -timeout 5m ./tests/e2e/...` — all pass.
- [ ] Run `go test -bench=. -benchmem -count=3 -run='^$' ./src/...` — no crashes.
- [ ] Run `go test -fuzz=FuzzSplitSQL -fuzztime=10s ./src/internal/store/...` — no panics.
- [ ] Run `go test -fuzz=FuzzCosineSimilarity -fuzztime=10s ./src/internal/vector/...` — no panics.
- [ ] Verify OpenAPI spec tests pass (`go test -run 'Test(Spec|Paths|Schemas|Snapshot)' ./api/...`).
- [ ] Verify `hermem --help` prints the full command tree without errors.
- [ ] Ensure working tree is clean.
- [ ] Stop and wait for further instructions.
