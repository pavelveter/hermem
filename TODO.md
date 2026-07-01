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

# Engineering checklist

## Release Engineering

- [ ] Add GoReleaser configuration.
- [ ] Verify release generation locally (`goreleaser check`).
- [ ] Add release workflow to GitHub Actions.
- [ ] Commit separately.

---

## CI

- [ ] Add golangci-lint with a well-balanced configuration.
- [ ] Ensure lint passes.
- [ ] Commit separately.

- [ ] Add govulncheck to CI.
- [ ] Verify it passes.
- [ ] Commit separately.

- [ ] Add dependency update automation (Dependabot or Renovate).
- [ ] Verify configuration.
- [ ] Commit separately.

---

## OpenAPI

- [ ] Add automatic OpenAPI validation in CI.
- [ ] Ensure generated specification is valid.
- [ ] Add regression test preventing invalid specs.
- [ ] Commit separately.

---

## Testing

- [ ] Add missing unit tests where coverage is weak.
- [ ] Verify all tests pass.
- [ ] Commit separately.

- [ ] Add fuzz tests for serialization, parsing, and critical APIs.
- [ ] Execute fuzzing to verify stability.
- [ ] Commit separately.

- [ ] Add benchmarks for performance-critical code paths.
- [ ] Verify benchmarks execute successfully.
- [ ] Commit separately.

---

## API Stability

- [ ] Add exported API compatibility checking.
- [ ] Ensure no unintended breaking API changes.
- [ ] Commit separately.

---

## Documentation

- [ ] Add CONTRIBUTING.md.
- [ ] Verify instructions are accurate.
- [ ] Commit separately.

- [ ] Add SECURITY.md.
- [ ] Commit separately.

- [ ] Add RELEASE.md.
- [ ] Commit separately.

- [ ] Review README for completeness and consistency.
- [ ] Commit separately.

---

## CLI

- [ ] Generate shell completions.
- [ ] Verify generated completions.
- [ ] Commit separately.

- [ ] Generate CLI documentation and/or man pages.
- [ ] Verify generated output.
- [ ] Commit separately.

---

## Quality

- [ ] Remove duplicated logic where appropriate.
- [ ] Ensure behavior is unchanged with tests.
- [ ] Commit separately.

- [ ] Improve compile-time safety by replacing string literals with typed enums/constants where appropriate.
- [ ] Verify behavior with tests.
- [ ] Commit separately.

- [ ] Review global mutable state.
- [ ] Eliminate unnecessary globals where possible.
- [ ] Verify thread safety.
- [ ] Commit separately.

- [ ] Reduce unnecessary allocations in hot paths.
- [ ] Verify using benchmarks.
- [ ] Commit separately.

- [ ] Review concurrency for race conditions.
- [ ] Verify with `go test -race`.
- [ ] Commit separately.

---

## Architecture

- [ ] Review package boundaries.
- [ ] Split oversized packages/functions where appropriate.
- [ ] Verify public API remains stable.
- [ ] Commit separately.

- [ ] Reduce coupling between packages where possible.
- [ ] Verify functionality.
- [ ] Commit separately.

- [ ] Improve dependency direction where needed.
- [ ] Verify tests.
- [ ] Commit separately.

---

## Final verification

- [ ] Run gofmt.
- [ ] Run go vet.
- [ ] Run golangci-lint.
- [ ] Run all unit tests.
- [ ] Run race detector.
- [ ] Run benchmarks.
- [ ] Run fuzz tests (where applicable).
- [ ] Verify OpenAPI generation.
- [ ] Verify release generation.
- [ ] Ensure working tree is clean.
- [ ] Stop and wait for further instructions.