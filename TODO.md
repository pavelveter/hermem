# Hermem TODO — Round 4 hardening (complete)

## Done

- [x] OllamaEmbedder timeout — dedicated *http.Client with 30s timeout
- [x] OpenAIEmbedder timeout — same, avoid http.DefaultClient
- [x] INI parser inline comments — strip #; before key=value parsing
- [x] SQLite DSN params — _journal_mode=WAL, _busy_timeout=5000, _sync=NORMAL in DSN
- [x] DecodeVector dimension guard — validate blob size vs expected dim, return error
- [x] MaxBytesReader (1MB) on all POST handlers — /store, /search, /retrieve, /ingest, /query, /edge
- [x] Recovery middleware — panic recover with structured log + 500 JSON response
- [x] CLI stdin safety — non-blocking os.Stdin.Stat() before io.ReadAll
- [x] Config validation — Validate() checks provider, dim, dedup_threshold, URL
- [x] CTE depth guard — verified: WHERE gw.depth < ? already hardcoded in recursive CTE
- [x] InMemory index startup sync — NewInMemoryVectorIndex.load() already reads all entities from SQLite
- [x] expvar counters are thread-safe — expvar.Int uses atomic ops internally

## Pending (low priority / optimisation)

- [x] Batch cosine via cblas_sgemv — replace per-vector CGO call with matrix×vector batch
- [ ] go mod tidy + verify — lock dependency tree
