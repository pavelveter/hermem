# OpenSSF Best Practices Badge — Answer Sheet

Project entry: <https://www.bestpractices.dev/en/projects/16106>
Entry owner: pavelveter. **The form can only be filled while logged in as
the owner** — this sheet gives the exact answer and justification text for
every unanswered criterion so the click-through is mechanical.

Rules of the badge: all MUST criteria Met (or justified Unmet), all SHOULD
and SUGGESTED criteria Met **or answered** (Unmet/N/A with justification).
An unanswered "?" blocks the badge; an honest "N/A" does not.

## Repo changes that unblock the crawler

- `CHANGELOG.md` moved to repo root (was `docs/CHANGELOG.md`) → the
  `release_notes` criterion auto-detects.
- OpenSSF badge added to the README badge row.

Everything below is answered from criteria that the crawler could not
detect on its own.

## Basics

| Criterion | Answer | Justification |
|---|---|---|
| `description_good` | Met | README lead: "Persistent graph memory for LLM agents. SQLite. Embeddings. Graph traversal. One binary." — https://github.com/pavelveter/hermem#readme |
| `interact` | Met | README documents install (`install.sh`, `make build`), issues at https://github.com/pavelveter/hermem/issues, and contribution via https://github.com/pavelveter/hermem/blob/main/CONTRIBUTING.md |
| `contribution_requirements` | Met | CONTRIBUTING.md lists prerequisites (Go 1.26+, golangci-lint, govulncheck), build/lint/test commands, and that CI (build, vet, gofmt, golangci-lint, tests, E2E) must pass — https://github.com/pavelveter/hermem/blob/main/CONTRIBUTING.md |
| `english` | Met | All docs, issue tracker, and commit messages are in English |
| `maintained` | Met | Active development: recent commits, releases through v0.4.5, CI runs on every push |

## Change Control

| Criterion | Answer | Justification |
|---|---|---|
| `repo_interim` | Met | Public git history on GitHub includes interim commits between releases |
| `version_unique` | Met | Releases are tagged `vX.Y.Z` (SemVer), e.g. v0.4.5 — https://github.com/pavelveter/hermem/tags |
| `version_semver` | Met | SemVer per RELEASE.md; 0.x breaking changes ship as MINOR bumps with migration guides (docs/MIGRATION-v0.4.0.md) |
| `version_tags` | Met | Every release is an annotated git tag (v0.2.0 … v0.4.5) |
| `release_notes_vulns` | N/A | No publicly known CVEs against hermem releases to date |
| `report_tracker` | Met | GitHub Issues: https://github.com/pavelveter/hermem/issues |
| `report_responses` | Met | Sole-maintainer project; all reported issues to date have been addressed |
| `enhancement_responses` | Met | Enhancement requests are discussed and triaged in GitHub Issues |
| `report_archive` | Met | GitHub Issues provides a searchable, URL-addressable public archive |
| `vulnerability_report_process` | Met | SECURITY.md: report to security@hermem.dev, no public issues for vulns — https://github.com/pavelveter/hermem/blob/main/SECURITY.md |
| `vulnerability_report_private` | Met | Private email channel (security@hermem.dev); consider also enabling GitHub's "privately report a vulnerability" on the repo (Settings → Code security) if not already on |
| `vulnerability_report_response` | N/A | No vulnerability reports received in the last 6 months |

## Quality

| Criterion | Answer | Justification |
|---|---|---|
| `test` | Met | Go test suite (unit + E2E) run in CI: `go test -race -count=1 -coverprofile=... ./src/...` plus E2E — https://github.com/pavelveter/hermem/blob/main/.github/workflows/ci.yml ; how to run documented in CONTRIBUTING.md and Makefile (`make test`) |
| `test_invocation` | Met | Standard invocation: `make test` / `go test ./...`; E2E via `go test ./tests/e2e/...` |
| `test_most` | Met | Coverage gate at ≥50% enforced in CI; property-based tests and fuzz harnesses cover core invariants (vector math, codec round-trips, SQL splitting) |
| `test_continuous_integration` | Met | GitHub Actions CI on every push/PR — https://github.com/pavelveter/hermem/blob/main/.github/workflows/ci.yml |
| `test_policy` | Met | Project policy (AGENTS.md / CONTRIBUTING.md): every non-trivial behavioral change ships with a test |
| `tests_are_added` | Met | Recent history shows tests added with features, e.g. "test: add e2e keyword search test for hybrid search" |
| `tests_documented_added` | Met | CONTRIBUTING.md instructs contributors to add tests for new functionality |
| `warnings` | Met | `go vet` and `golangci-lint` run as required CI checks |
| `warnings_fixed` | Met | CI fails on any vet/gofmt/golangci-lint finding; main is warning-clean |
| `warnings_strict` | Met | gofmt check is exact-format; lint gate blocks merge on findings |

## Security

| Criterion | Answer | Justification |
|---|---|---|
| `know_secure_design` | Met | Primary developer familiar with Saltzer-Schroeder principles; architecture uses fail-safe defaults (deny-by-default API auth), least privilege (pinned, read-only CI tokens), input validation with allowlists (strict config validation, SQL parameterization) |
| `know_common_errors` | Met | Go-specific error classes covered: SQL injection (parameterized queries + fuzzed SQL splitter `FuzzSplitSQL`), integer overflow in vector math (property-based tests), prototype of common errors reviewed against CWE/SANS top 25; govulncheck runs in CI |
| `crypto_published` | N/A | hermem implements no cryptographic protocols or algorithms of its own; TLS is delegated to deployment (reverse proxy) and Go stdlib where used |
| `crypto_call` | Met | No cryptography is re-implemented; any crypto needs call Go's standard library (crypto/*) or the platform |
| `crypto_floss` | N/A | No crypto functionality bundled; where stdlib crypto is used it is FLOSS (Go, BSD-style) |
| `crypto_keylength` | N/A | No key material managed by the project |
| `crypto_working` | N/A | No bundled crypto algorithms; no broken algorithms used |
| `crypto_weaknesses` | N/A | No bundled crypto algorithms |
| `crypto_pfs` | N/A | No key-agreement protocol implemented by the project |
| `crypto_password_storage` | N/A | The software does not store passwords for inbound user authentication |
| `crypto_random` | Met | Any randomness uses Go's crypto/rand; tests/IDs that do not need CSPRNG use math/rand explicitly separated |
| `delivery_unsigned` | Met | No hashes are distributed over plain http; all distribution (GitHub releases, container registries) is HTTPS-only |
| `vulnerabilities_fixed_60_days` | Met | No known unpatched medium+ vulnerabilities; govulncheck and OpenSSF Scorecard run in CI |
| `vulnerabilities_critical_fixed` | Met | No critical vulnerabilities reported to date |
| `no_leaked_credentials` | Met | No credentials in the repository; secrets are supplied via environment/config at runtime; test fixtures use sample data only |

## Analysis

| Criterion | Answer | Justification |
|---|---|---|
| `static_analysis` | Met | golangci-lint + `go vet` on every PR, plus CodeQL (https://github.com/pavelveter/hermem/blob/main/.github/workflows/codeql.yml) and govulncheck |
| `static_analysis_common_vulnerabilities` | Met | CodeQL includes security queries (OWASP/CWE); govulncheck checks vulnerable dependencies |
| `static_analysis_fixed` | Met | Findings gate CI; none outstanding |
| `static_analysis_often` | Met | Static analysis runs on every push/PR in GitHub Actions |
| `dynamic_analysis` | Met | Go fuzzing targets run in CI (`FuzzSplitSQL`, `FuzzCosineSimilarity`, codec round-trip fuzzers) — https://github.com/pavelveter/hermem/blob/main/.github/workflows/ci.yml |
| `dynamic_analysis_unsafe` | N/A | Implementation language is Go (memory-safe); no C/C++ shipped |
| `dynamic_analysis_enable_assertions` | Met | Go enables runtime bounds checks/panics by default in all test and fuzz builds (`-race` used in CI test runs) |
| `dynamic_analysis_fixed` | N/A | No vulnerabilities discovered via dynamic analysis to date |

## Click-through tips

- Log in, open the Passing tab, and answer each "?" using the table
  above. The crawler will re-check repo evidence on save; auto-detected
  criteria (CONTRIBUTING.md, LICENSE, SECURITY.md, Makefile, CI, and now
  root CHANGELOG.md) should flip to Met without justification.
- After saving, the badge at
  https://www.bestpractices.dev/projects/16106/badge flips to "passing".
- If the site still shows `release_notes` unmet, paste the URL
  https://github.com/pavelveter/hermem/blob/main/CHANGELOG.md directly
  into the criterion.
