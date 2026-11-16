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
| `documentation_interface` | Met | README and docs/ document the CLI commands and the HTTP API — https://github.com/pavelveter/hermem/tree/main/docs |

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
| `report_archive` | Met | Public searchable archive of reports and responses: https://github.com/pavelveter/hermem/issues (URL required by the criterion) |
| `vulnerability_report_process` | Met | SECURITY.md: report to security@hermem.dev, no public issues for vulns — https://github.com/pavelveter/hermem/blob/main/SECURITY.md |
| `vulnerability_report_private` | Met | Private reporting via GitHub security advisories https://github.com/pavelveter/hermem/security/advisories and email security@hermem.dev per https://github.com/pavelveter/hermem/blob/main/SECURITY.md (URL required by the criterion) |
| `vulnerability_report_response` | N/A | No vulnerability reports received in the last 6 months |

## Quality

| Criterion | Answer | Justification |
|---|---|---|
| `test` | Met | Go test suite (unit + E2E) run in CI: `go test -race -count=1 -coverprofile=... ./src/...` plus E2E — https://github.com/pavelveter/hermem/blob/main/.github/workflows/ci.yml ; how to run documented in CONTRIBUTING.md and Makefile (`make test`) |
| `build_floss_tools` | Met | Built with the Go toolchain and GNU Make (both FLOSS); CI runs on GitHub Actions Ubuntu runners |
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

- Fastest path: open the four automation-proposal URLs below while logged
  in. Each pre-fills the Passing form fields (🤖 highlighted) with the
  answers from this sheet; review and "Save and exit" each. The first URL
  also forces `release_notes` to Met (the stale "Unmet" predates the move
  of CHANGELOG.md to the repo root; Chief will keep it Met on re-crawl).
- Criteria not covered by any URL (CONTRIBUTING.md, LICENSE, SECURITY.md,
  Makefile, CI, root CHANGELOG.md) are auto-detected by the crawler.
- After saving, the badge at
  https://www.bestpractices.dev/projects/16106/badge flips to "passing".
- If the site still shows `release_notes` unmet, paste the URL
  https://github.com/pavelveter/hermem/blob/main/CHANGELOG.md directly
  into the criterion.

<details>
<summary>Automation-proposal URLs (click to expand)</summary>

1. Basics + Change Control (19 criteria, forces `release_notes`):

```
https://www.bestpractices.dev/en/projects/16106/passing/edit?release_notes_status=Met&release_notes_justification=Root%20CHANGELOG.md%20at%20https%3A//github.com/pavelveter/hermem/blob/main/CHANGELOG.md%20%28kept%20current%20per%20Keep-a-Changelog%29&description_good_status=Met&description_good_justification=Readme%20lead%3A%20%22Persistent%20graph%20memory%20for%20LLM%20agents.%20SQLite.%20Embeddings.%20Graph%20traversal.%20One%20binary.%22%20%E2%80%94%20https%3A//github.com/pavelveter/hermem%23readme&interact_status=Met&interact_justification=README%20documents%20install%20%28install.sh%2C%20make%20build%29%2C%20issues%20at%20https%3A//github.com/pavelveter/hermem/issues%2C%20and%20contribution%20via%20https%3A//github.com/pavelveter/hermem/blob/main/CONTRIBUTING.md&contribution_requirements_status=Met&contribution_requirements_justification=CONTRIBUTING.md%20lists%20prerequisites%20%28Go%201.26%2B%2C%20golangci-lint%2C%20govulncheck%29%2C%20build/lint/test%20commands%2C%20and%20required%20CI%20checks%20%E2%80%94%20https%3A//github.com/pavelveter/hermem/blob/main/CONTRIBUTING.md&english_status=Met&english_justification=All%20docs%2C%20issue%20tracker%2C%20and%20commit%20messages%20are%20in%20English&maintained_status=Met&maintained_justification=Active%20development%3A%20recent%20commits%2C%20releases%20through%20v0.4.5%2C%20CI%20runs%20on%20every%20push&repo_interim_status=Met&repo_interim_justification=Public%20git%20history%20on%20GitHub%20includes%20interim%20commits%20between%20releases&version_unique_status=Met&version_unique_justification=Releases%20are%20tagged%20vX.Y.Z%20%28SemVer%29%2C%20e.g.%20v0.4.5%20%E2%80%94%20https%3A//github.com/pavelveter/hermem/tags&version_semver_status=Met&version_semver_justification=SemVer%20per%20RELEASE.md%3B%200.x%20breaking%20changes%20ship%20as%20MINOR%20bumps%20with%20migration%20guides%20%28docs/MIGRATION-v0.4.0.md%29&version_tags_status=Met&version_tags_justification=Every%20release%20is%20an%20annotated%20git%20tag%20%28v0.2.0%20through%20v0.4.5%29&release_notes_vulns_status=N/A&release_notes_vulns_justification=No%20publicly%20known%20CVEs%20against%20hermem%20releases%20to%20date&report_tracker_status=Met&report_tracker_justification=GitHub%20Issues%3A%20https%3A//github.com/pavelveter/hermem/issues&report_responses_status=Met&report_responses_justification=Sole-maintainer%20project%3B%20all%20reported%20issues%20to%20date%20have%20been%20addressed&enhancement_responses_status=Met&enhancement_responses_justification=Enhancement%20requests%20are%20discussed%20and%20triaged%20in%20GitHub%20Issues&report_archive_status=Met&report_archive_justification=Public%20searchable%20archive%20of%20reports%20and%20responses%3A%20https%3A//github.com/pavelveter/hermem/issues&vulnerability_report_process_status=Met&vulnerability_report_process_justification=SECURITY.md%3A%20report%20to%20security%40hermem.dev%2C%20no%20public%20issues%20for%20vulns%20%E2%80%94%20https%3A//github.com/pavelveter/hermem/blob/main/SECURITY.md&vulnerability_report_private_status=Met&vulnerability_report_private_justification=Private%20reporting%20via%20GitHub%20security%20advisories%20https%3A//github.com/pavelveter/hermem/security/advisories%20and%20email%20security%40hermem.dev%20per%20https%3A//github.com/pavelveter/hermem/blob/main/SECURITY.md&vulnerability_report_response_status=N/A&vulnerability_report_response_justification=No%20vulnerability%20reports%20received%20in%20the%20last%206%20months&documentation_interface_status=Met&documentation_interface_justification=README%20and%20docs/%20document%20the%20CLI%20commands%20and%20the%20HTTP%20API%20for%20interacting%20with%20the%20service%20%E2%80%94%20https%3A//github.com/pavelveter/hermem/tree/main/docs&overrides=release_notes*
```

2. Quality (11 criteria):

```
https://www.bestpractices.dev/en/projects/16106/passing/edit?build_floss_tools_status=Met&build_floss_tools_justification=Built%20with%20the%20Go%20toolchain%20and%20GNU%20Make%20%28both%20FLOSS%29%3B%20CI%20runs%20on%20GitHub%20Actions%20Ubuntu%20runners&test_status=Met&test_justification=Go%20test%20suite%20%28unit%20%2B%20E2E%29%20run%20in%20CI%20with%20-race%20and%20coverage%20%E2%80%94%20https%3A//github.com/pavelveter/hermem/blob/main/.github/workflows/ci.yml%3B%20how%20to%20run%20documented%20in%20CONTRIBUTING.md%20and%20Makefile&test_invocation_status=Met&test_invocation_justification=Standard%20invocation%3A%20make%20test%20/%20go%20test%20./...%3B%20E2E%20via%20go%20test%20./tests/e2e/...&test_most_status=Met&test_most_justification=Coverage%20gate%20at%2050%25%20or%20higher%20enforced%20in%20CI%3B%20property-based%20tests%20and%20fuzz%20harnesses%20cover%20core%20invariants%20%28vector%20math%2C%20codec%20round-trips%2C%20SQL%20splitting%29&test_continuous_integration_status=Met&test_continuous_integration_justification=GitHub%20Actions%20CI%20on%20every%20push/PR%20%E2%80%94%20https%3A//github.com/pavelveter/hermem/blob/main/.github/workflows/ci.yml&test_policy_status=Met&test_policy_justification=Project%20policy%20%28AGENTS.md%20/%20CONTRIBUTING.md%29%3A%20every%20non-trivial%20behavioral%20change%20ships%20with%20a%20test&tests_are_added_status=Met&tests_are_added_justification=Recent%20history%20shows%20tests%20added%20with%20features%2C%20e.g.%20test%3A%20add%20e2e%20keyword%20search%20test%20for%20hybrid%20search&tests_documented_added_status=Met&tests_documented_added_justification=CONTRIBUTING.md%20instructs%20contributors%20to%20add%20tests%20for%20new%20functionality&warnings_status=Met&warnings_justification=go%20vet%20and%20golangci-lint%20run%20as%20required%20CI%20checks&warnings_fixed_status=Met&warnings_fixed_justification=CI%20fails%20on%20any%20vet/gofmt/golangci-lint%20finding%3B%20main%20is%20warning-clean&warnings_strict_status=Met&warnings_strict_justification=gofmt%20check%20is%20exact-format%3B%20lint%20gate%20blocks%20merge%20on%20findings
```

3. Security (15 criteria):

```
https://www.bestpractices.dev/en/projects/16106/passing/edit?know_secure_design_status=Met&know_secure_design_justification=Architecture%20uses%20fail-safe%20defaults%20%28deny-by-default%20API%20auth%29%2C%20least%20privilege%20%28pinned%20read-only%20CI%20tokens%29%2C%20and%20input%20validation%20with%20allowlists%20%28strict%20config%20validation%2C%20SQL%20parameterization%29&know_common_errors_status=Met&know_common_errors_justification=SQL%20injection%20prevented%20via%20parameterized%20queries%20plus%20fuzzed%20SQL%20splitter%20%28FuzzSplitSQL%29%3B%20integer%20overflow%20in%20vector%20math%20covered%20by%20property-based%20tests%3B%20govulncheck%20runs%20in%20CI&crypto_published_status=N/A&crypto_published_justification=hermem%20implements%20no%20cryptographic%20protocols%20or%20algorithms%20of%20its%20own%3B%20TLS%20is%20delegated%20to%20deployment%20and%20Go%20stdlib%20where%20used&crypto_call_status=Met&crypto_call_justification=No%20cryptography%20is%20re-implemented%3B%20any%20crypto%20needs%20call%20Go%20standard%20library%20%28crypto/%2A%29%20or%20the%20platform&crypto_floss_status=N/A&crypto_floss_justification=No%20crypto%20functionality%20bundled%3B%20where%20stdlib%20crypto%20is%20used%20it%20is%20FLOSS%20%28Go%2C%20BSD-style%29&crypto_keylength_status=N/A&crypto_keylength_justification=No%20key%20material%20managed%20by%20the%20project&crypto_working_status=N/A&crypto_working_justification=No%20bundled%20crypto%20algorithms%3B%20no%20broken%20algorithms%20used&crypto_weaknesses_status=N/A&crypto_weaknesses_justification=No%20bundled%20crypto%20algorithms&crypto_pfs_status=N/A&crypto_pfs_justification=No%20key-agreement%20protocol%20implemented%20by%20the%20project&crypto_password_storage_status=N/A&crypto_password_storage_justification=The%20software%20does%20not%20store%20passwords%20for%20inbound%20user%20authentication&crypto_random_status=Met&crypto_random_justification=Randomness%20uses%20Go%20crypto/rand%3B%20non-security%20randomness%20uses%20math/rand%20explicitly%20separated&delivery_unsigned_status=Met&delivery_unsigned_justification=No%20hashes%20distributed%20over%20plain%20http%3B%20all%20distribution%20%28GitHub%20releases%2C%20container%20registries%29%20is%20HTTPS-only&vulnerabilities_fixed_60_days_status=Met&vulnerabilities_fixed_60_days_justification=No%20known%20unpatched%20medium%2B%20vulnerabilities%3B%20govulncheck%20and%20OpenSSF%20Scorecard%20run%20in%20CI&vulnerabilities_critical_fixed_status=Met&vulnerabilities_critical_fixed_justification=No%20critical%20vulnerabilities%20reported%20to%20date&no_leaked_credentials_status=Met&no_leaked_credentials_justification=No%20credentials%20in%20the%20repository%3B%20secrets%20supplied%20via%20environment/config%20at%20runtime%3B%20test%20fixtures%20use%20sample%20data%20only
```

4. Analysis (8 criteria):

```
https://www.bestpractices.dev/en/projects/16106/passing/edit?static_analysis_status=Met&static_analysis_justification=golangci-lint%20%2B%20go%20vet%20on%20every%20PR%2C%20plus%20CodeQL%20%28https%3A//github.com/pavelveter/hermem/blob/main/.github/workflows/codeql.yml%29%20and%20govulncheck&static_analysis_common_vulnerabilities_status=Met&static_analysis_common_vulnerabilities_justification=CodeQL%20includes%20security%20queries%20%28OWASP/CWE%29%3B%20govulncheck%20checks%20vulnerable%20dependencies&static_analysis_fixed_status=Met&static_analysis_fixed_justification=Findings%20gate%20CI%3B%20none%20outstanding%20%28gosec%20informational%20findings%20reviewed%20and%20accepted%29&static_analysis_often_status=Met&static_analysis_often_justification=Static%20analysis%20runs%20on%20every%20push/PR%20in%20GitHub%20Actions&dynamic_analysis_status=Met&dynamic_analysis_justification=Go%20fuzzing%20targets%20run%20in%20CI%20%28FuzzSplitSQL%2C%20FuzzCosineSimilarity%2C%20codec%20round-trip%20fuzzers%29%20%E2%80%94%20https%3A//github.com/pavelveter/hermem/blob/main/.github/workflows/ci.yml&dynamic_analysis_unsafe_status=N/A&dynamic_analysis_unsafe_justification=Implementation%20language%20is%20Go%20%28memory-safe%29%3B%20no%20C/C%2B%2B%20shipped&dynamic_analysis_enable_assertions_status=Met&dynamic_analysis_enable_assertions_justification=Go%20enables%20runtime%20bounds%20checks/panics%20by%20default%3B%20-race%20used%20in%20CI%20test%20runs&dynamic_analysis_fixed_status=N/A&dynamic_analysis_fixed_justification=No%20vulnerabilities%20discovered%20via%20dynamic%20analysis%20to%20date
```

</details>
