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

- [x] **C1 — Fix OpenAPI spec discrepancies**
  - [x] Add `/task/claim-next` to OpenAPI spec.
  - [x] Add `/ingest/jobs` to OpenAPI spec.
  - [x] Add `/admin/retention/run` to OpenAPI spec.
  - [x] Implement `handleQueryTemporal` HTTP handler.
  - [x] Update `docs/generated/ROUTES.md`.
  - [x] Verify with `go test -run 'Test(Spec|Paths|Schemas|Snapshot)' ./api/...`

- [x] **C2 — Implement missing `/query/temporal` HTTP handler**
  - [x] Add `handleQueryTemporal` to `server/retrieval/retrieval_service.go`.
  - [x] Register the route in `Routes()`.
  - [x] Add end-to-end HTTP test in `tests/e2e/http/`.

- [x] **C3 — Fix hardcoded Docker image version**
  - [x] Replace `image: hermem:0.2.0` with `image: hermem:${HERMEM_VERSION:-latest}`.
  - [x] Document `HERMEM_VERSION` in docker-compose comments.

- [x] **C4 — Add benchmark baseline file**
  - [x] Run benchmarks and commit `bench/baseline/baseline.txt`.
  - [x] Verify baseline file is non-empty.

- [x] **C5 — Add rate limiting middleware**
  - [x] Implement in-memory token-bucket rate limiter.
  - [x] Add `[server] rate_limit` config section.
  - [x] Wire rate-limit middleware into the server middleware chain.
  - [x] Return `429 Too Many Requests` with `Retry-After` header.
  - [x] Add unit tests for the limiter and middleware.

---

## H — High

- [x] **H1 — Add `HERMEM_INI` env-var override and `--config` flag**
  - [x] Add `--config <path>` cobra persistent flag.
  - [x] Add `HERMEM_INI` env-var resolution.
  - [x] Precedence: flag > env-var > binary-dir-relative.
  - [x] Add tests for precedence chain.

- [x] **H2 — Increase SDK test coverage**
  - [x] Add Go SDK tests for `memory`, `task`, `graph`, `admin` sub-clients.
  - [x] Evaluate Python SDK test coverage; add missing tests.
  - [x] Evaluate TypeScript SDK test coverage; add missing tests.

- [x] **H3 — Add SDK E2E tests to CI**
  - [x] Add Go SDK E2E workflow to CI.
  - [x] Add Python SDK E2E workflow to CI.
  - [x] Add TypeScript SDK E2E workflow to CI.

- [x] **H4 — Add reranker to health readiness probe**
  - [x] Add `reranker` check to `health/probes.go`.
  - [x] Wire into readiness probe with `critical: false`.
  - [x] Add test in `health/service_test.go`.

- [x] **H5 — Add structured error events to slog for AI provider calls**
  - [x] Add `ai_call_failed` slog event at ERROR level.
  - [x] Add `ai_call_retry` slog event at WARN level.
  - [x] Add tests for slog events.

- [x] **H6 — Eliminate CLI dependency on deprecated `NewServer()` constructor**
  - [x] Migrate all callers to `NewServerFromDeps()`.
  - [x] Remove `NewServer()`.

- [x] **H7 — Add missing `go.sum` entries for SDK test-only deps**
  - [x] Commit lockfile for TypeScript SDK.
  - [x] Commit lockfile for Python SDK.

---

## M — Medium

- [x] **M1 — Generate and commit shell completions**
  - [x] Generate completions for bash, zsh, fish.
  - [x] Add `completions/` directory.
  - [x] Include in release archive.
  - [x] Add `make install-completions` target.

- [x] **M2 — Add test coverage reporting to CI**
  - [x] Add `-coverprofile=coverage.out` to CI test job.
  - [x] Add coverage threshold gate.
  - [x] Generate coverage badge for README.

- [x] **M3 — Write a proper `docs/ROADMAP.md`**
  - [x] Create `docs/ROADMAP.md` with status indicators.
  - [x] Link from README.

- [x] **M4 — Flush out `docs/VISION.md` beyond a checklist**
  - [x] Add narrative context per pillar.
  - [x] Add inter-pillar dependencies.

- [x] **M5 — Replace `sortStrings` bubble sort with `slices.Sort`**
  - [x] Replace `sortStrings` with `slices.Sort`.
  - [x] Verify `SortedKeys` output is identical.

- [x] **M6 — Add `CODEOWNERS` file**
  - [x] Create `.github/CODEOWNERS`.

- [x] **M7 — Add `.editorconfig`**
  - [x] Create `.editorconfig` with Go-standard settings.

- [x] **M8 — Fix `go.mod` Go version**
  - [x] Verify minimum Go version required.
  - [x] Set `go` directive to correct minimum version.

- [x] **M9 — Audit `AGENTS.md` language**
  - [x] Translate `AGENTS.md` to English.
  - [x] Keep Russian version as `AGENTS.ru.md`.

---

## L — Low

- [x] **L1 — Add CodeQL static analysis to CI**
  - [x] Add `.github/workflows/codeql.yml`.

- [x] **L2 — Add OpenSSF Scorecard to CI**
  - [x] Add `.github/workflows/scorecard.yml`.

- [x] **L3 — Add `go.work` for multi-module workspace**
  - [x] Create `go.work` with `use .` and `use ./sdk/go`.

- [x] **L4 — Add devcontainer configuration**
  - [x] Create `.devcontainer/devcontainer.json`.

- [x] **L5 — Fix `AGENTS.md` references to non-existent file**
  - [x] Remove dangling reference to `andrey-karpathy-skills.md`.

- [ ] **L6 — Add CHANGELOG generation automation**
  - [ ] Add `git-cliff` or similar changelog generator config.
  - [ ] Wire into release workflow.
  - [ ] Commit separately.

- [x] **L7 — Add `go.mod` version for SDK**
  - [x] Verify minimum Go version needed by SDK.
  - [x] Document as `go 1.21`.

- [x] **L8 — Add graceful degradation for AI provider timeouts**
  - [x] Implement fallback on reranker failure.
  - [x] Add tests.

- [x] **L9 — Document `go.sum` database**
  - [x] Add note about `GONOSUMCHECK` / `GONOSUMDB` to CONTRIBUTING.md.

---

## A — Architecture

- [ ] **A1 — Eliminate `applicationToEnv` adapter**
  - [ ] Migrate CLI commands to accept `*app.Application`.
  - [ ] Remove `applicationToEnv()` from `main.go`.
  - [ ] Remove `*clienv.Env` as primary CLI dependency.

- [ ] **A2 — Extract shared HTTP client from `ai/http.go`**
  - [ ] Extract `ResilientClient` into `httputil/` or `httpclient/`.
  - [ ] Update all AI client implementations.
  - [ ] Add tests for extracted client.

- [ ] **A3 — Standardize Retriever interface across pipeline stages**
  - [ ] Audit all callers of `retrieval.Service`.
  - [ ] Define `Retriever` interface to match all public methods.
  - [ ] Update `app.Application` accordingly.

- [ ] **A4 — Decouple schema validation from config loading**
  - [ ] Create `src/internal/schema/` package.
  - [ ] Move schema concerns from `config` and `core`.
  - [ ] Update all imports.

- [ ] **A5 — Reduce `config.Config` surface area**
  - [ ] Split into focused sub-configs with dedicated validation.
  - [ ] `Config` composes them.

---

## T — Tooling

- [x] **T1 — Add pre-commit hook (in addition to pre-push)**
  - [x] Create `.githooks/pre-commit` with gofmt + go vet.

- [x] **T2 — Add local development helper (`make dev`)**
  - [x] Add `.air.toml` config.
  - [x] Add `make dev` target.
  - [x] Document in CONTRIBUTING.md.

- [x] **T3 — Add `make fmt` and `make vet` targets**
  - [x] Verify `make fmt` and `make vet` exist in Makefile.

- [x] **T4 — Add `make test-coverage` target**
  - [x] Verify `make test-coverage` exists in Makefile.

- [x] **T5 — Add Dependabot for Go modules**
  - [x] Add `gomod` ecosystem entries to `dependabot.yml`.

- [x] **T6 — Add `Dockerfile` linting to CI**
  - [x] Add hadolint to CI workflow.
  - [x] Fix any existing violations.

- [x] **T7 — Add SLSA provenance generation for releases**
  - [x] Add SLSA provenance job to release workflow.

- [x] **T8 — Add `hermem.ini` validation command**
  - [x] Add `hermem config validate [--path <file>]` CLI command.

- [x] **T9 — Add `hermem config show` with defaults**
  - [x] Add `hermem config show [--path <file>]` CLI command.

- [x] **T10 — Add `mage` or `task` as alternative to `make`**
  - [x] Add `magefile.go` with equivalents of make targets.
  - [x] Document in CONTRIBUTING.md.

---

## Final verification

- [ ] Run `gofmt -s -w .` and verify clean.
- [ ] Run `go vet ./...` and verify clean.
- [ ] Run `golangci-lint run ./...` and fix any issues.
- [ ] Run `go test -race -count=1 -timeout 10m ./src/...` — all pass.
- [ ] Run `go test -p 1 -timeout 5m ./tests/e2e/...` — all pass.
- [ ] Run `go test -bench=. -benchmem -count=3 -run='^$' ./src/...` — no crashes.
- [ ] Run fuzz tests — no panics.
- [ ] Verify OpenAPI spec tests pass.
- [ ] Verify `hermem --help` prints the full command tree without errors.
- [ ] Ensure working tree is clean.
- [ ] Stop and wait for further instructions.
