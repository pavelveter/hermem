# Roadmap

This document tracks planned, in-progress, and shipped features. Status indicators:

- ✅ Shipped — available in current release
- 🔄 In Progress — actively being worked on
- 📋 Planned — designed but not yet started
- 💡 Research — under investigation, may not ship

---

## Shipped ✅

- Graph-native memory with SQLite backend
- Vector embeddings (Ollama, OpenAI, local GGUF)
- Semantic search with graph walk and ranking
- Contradiction detection and belief revision
- Episodic memory and timeline
- Task lifecycle with dependency management
- MCP server for AI assistant integration
- OpenAPI 3.1 spec with contract tests
- Go, Python, TypeScript SDKs with version negotiation
- Shell completion (bash, zsh, fish)
- Typed DI container (`app.Application`)
- Typed enums with validation
- Fuzz harnesses and property-based tests
- Benchmark regression gates in CI
- AI factory with test fakes
- `--config` flag and `HERMEM_INI` env-var

---

## In Progress 🔄

- Rate limiting middleware (token bucket)
- SDK E2E tests in CI
- Schema validation decoupled from config
- `config.Config` surface area reduction

---

## Planned 📋

### Short-term (next 1-2 releases)

- Test coverage reporting in CI with threshold gates
- Devcontainer configuration for VS Code
- `go.work` for multi-module workspace
- `hermem config validate` CLI command
- `hermem config show` with defaults
- CodeQL static analysis in CI
- OpenSSF Scorecard
- CHANGELOG generation automation
- Dockerfile linting in CI

### Medium-term (3-6 months)

- CodeQL + SLSA provenance for releases
- `applicationToEnv` adapter elimination
- Shared HTTP client extraction from `ai/http.go`
- Retriever interface standardization across pipeline stages
- Graceful degradation for AI provider timeouts
- Logger fan-in reduction (service-level facades)

### Long-term (6+ months)

- Richer graph scoring algorithms
- Graph summarization
- Semantic compression
- Graph visualization
- Distributed replication
- CRDT-based synchronization
- Native reranker plugins
- Hybrid lexical/vector retrieval
- Graph-aware agent planning
- Incremental embedding updates

---

## Research 💡

- Belief graph architecture
- Memory decay model
- Confidence propagation model
- Autonomous memory cleanup
- Identity memory layer
- Self-reflection memory layer
- Memory-driven planning and reasoning
- Autonomous memory evolution

---

## Version Plan

| Version | Focus |
|---------|-------|
| 0.3.x | Hardening: rate limiting, coverage, CI improvements |
| 0.4.x | Architecture: eliminate adapters, decouple schema |
| 0.5.x | Developer experience: devcontainer, go.work, config tools |
| 1.0 | Stable API, full SDK coverage, production-ready |
