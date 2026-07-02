# Contributing to Hermem

Thank you for your interest in contributing to Hermem! This document provides guidelines and instructions for contributing.

## Getting Started

### Prerequisites

- Go 1.26+
- SQLite3
- golangci-lint (for linting)
- govulncheck (for vulnerability scanning)

### Development Setup

1. Clone the repository:
   ```bash
   git clone https://github.com/pavelveter/hermem.git
   cd hermem
   ```

2. Install dependencies:
   ```bash
   go mod download
   ```

3. Build the project:
   ```bash
   make build
   ```

4. Run tests:
   ```bash
   make test
   ```

### Hot reload (optional)

For faster iteration, install [air](https://github.com/air-verse/air) and
use `make dev`. The target runs `air -c .air.toml`, which:

- Rebuilds `./hermem` whenever any `*.go` under `src/` changes (tests,
  docs, completion scripts, `*.db`, `*.ini` are excluded to keep
  rebuilds fast and to avoid spurious restarts).
- Runs the embed-placeholder pre-command so `go:embed` always sees the
  binary it expects.
- Restarts `./hermem serve` after a 1-second delay with `SIGINT`, giving
  SQLite WAL a chance to checkpoint cleanly.

```bash
go install github.com/air-verse/air@latest
make dev
```

If `air` is not on `$PATH`, the target prints the install command
above and exits 1 (it does NOT auto-install, to avoid silently mutating
the developer's `$GOBIN`).

### Mage (cross-platform build runner)

On Windows, BSD, or any environment without GNU make, you can drive the
same targets through [mage](https://magefile.org/). `magefile.go` is a
drop-in alternative that mirrors `Makefile` targets one-for-one and is
kept in lock-step with it.

```bash
go install github.com/magefile/mage@latest
mage                      # default: Build + Completions
mage Build && mage Test
mage Install INSTALL_DIR=/usr/local/bin
mage Completions
mage InstallCompletions
mage Lint Vet Fmt
mage Dev                  # requires air (see above)
```

The file is gated by `//go:build mage` so it is excluded from normal
`go build ./...` and cannot accidentally end up in the production
binary. Targets that are inherently platform-specific (e.g. `sign` /
`routes`) are intentionally not mirrored; everything else has a
matching mage function.

### Code Style

- Follow standard Go conventions (`gofmt`, `go vet`)
- All exported functions must have doc comments
- Keep functions focused and small (cognitive complexity < 15)
- Use dependency injection, not global state

### Testing

- All new code must include unit tests
- Run `make test` before committing
- Run `make test-e2e` for integration tests
- Fuzz tests are required for serialization/parsing code
- Benchmarks are required for performance-sensitive code

### Go Checksum Database

`go.sum` is committed to ensure reproducible builds. The Go checksum
database (`sum.golang.org`) verifies module authenticity. If you
encounter checksum mismatches in CI:

- Run `go mod tidy` to regenerate `go.sum`
- Ensure `GONOSUMCHECK` is not set unless targeting private modules
- For private modules, set `GONOSUMDB` and configure `GOPRIVATE`
- Never edit `go.sum` manually — always regenerate via `go mod tidy`

### Commit Messages

Follow Conventional Commits:
- `feat(scope): description` for new features
- `fix(scope): description` for bug fixes
- `test(scope): description` for tests
- `docs(scope): description` for documentation
- `refactor(scope): description` for refactoring

### Pull Requests

1. Create a feature branch from `main`
2. Make your changes with tests
3. Ensure all CI checks pass
4. Submit a PR with a clear description

## Architecture

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the project architecture.

## ADRs

Architectural decisions are documented in [docs/adr/](docs/adr/).

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
