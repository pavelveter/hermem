# AGENTS.md — Hermem Contributor Workflow

> **Language**: This is the canonical English version. The original
> Russian text is preserved as `AGENTS.ru.md` for reference.

This document defines the rules for working with the code. All
contributors — humans and AI agents alike — MUST follow them.

---

**FIRST READ THE RULES FROM
[docs/andrey-karpathy-skills.md](docs/andrey-karpathy-skills.md) —
TOP PRIORITY.**

## 1. Git Flow

We follow **GitHub Flow**:

```
main  ← feature/xxx  ← (work happens here)
  ↓
PR → review → merge → deploy
```

### 1.1 Branches

- **`main`** — always stable, ready to deploy. Nothing is pushed
  directly into it.
- **`feature/<name>`** — every new feature or change. Branched from
  `main`.
- **`fix/<name>`** — for bug fixes.
- **`refactor/<name>`** — for refactoring that does not change
  behaviour.

### 1.2 Commits

- **Frequent** — commit every logically complete change.
- **Meaningful messages** — in Russian or English:
  - Bad: `fix`, `update`, `changes`
  - Good: `Add hybrid search (vector + FTS5) with RRF`,
    `fix: handle empty feed at startup`
- **Prefixes** (optional, Conventional Commits):
  - `feat:` — new feature
  - `fix:` — bug fix
  - `refactor:` — refactoring
  - `test:` — tests
  - `docs:` — documentation
  - `chore:` — infrastructure, CI/CD, dependencies

### 1.3 Pull Requests

- Each branch → its own PR into `main`.
- PRs must be small (up to ~300 lines of changes).
- PRs must pass CI (linters, type check, tests) before merge.
- After merge, the branch is deleted.

### 1.4 Pre-push hook

Before every `git push`, `.githooks/pre-push` runs automatically —
all checks must pass, otherwise the push is blocked.

**Enable the hook** (once per clone):

```bash
git config core.hooksPath .githooks
```

**What is checked** (in run order):

1. **Architecture guardrails** — grep for forbidden patterns
   (`ActiveSchema()`, exported mutable state).
2. **gofmt** — formatting must be clean.
3. **go vet** — static analysis of `./src/...`.
4. **go build** — build `./src/...`.
5. **Unit tests** — `go test -race -count=1 -timeout 10m ./src/...`.
6. **E2E tests** — `go test -p 1 -timeout 5m ./tests/e2e/...`
   (CLI + HTTP + scenarios).
7. **golangci-lint** — linter, if installed locally.
8. **AMX guard** — checks for AMX instructions (Darwin + CGO only).

**Bypass** (only in exceptional cases): `git push --no-verify`.

---

## 2. Development Workflow

### 2.1 Step 1: Start a new feature

```bash
git checkout main
git pull origin main
git checkout -b feature/<name>
```

### 2.2 Step 2: Work

- Change only what relates to the feature.
- Document public APIs (docstrings).
- Write tests for new logic.
- **Commit every iteration** — after each logically complete block
  of changes (a feature, a fix, a refactor), commit immediately
  with a meaningful message. Do not accumulate changes until PR
  time. This gives you:
  - **Clear history** — easy to roll back one fix without losing
    the rest.
  - **Fast review** — every commit is small and understandable.
  - **Safety** — changes are recorded even if the next ones
    break the code.
