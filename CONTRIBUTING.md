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
