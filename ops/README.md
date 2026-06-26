# hermem — Ops Artifacts

## Grafana Dashboard

File: `ops/grafana/hermem-dashboard.json`

### Import

1. Grafana UI → **Connections** → **Data sources** → Add Prometheus with UID `prometheus` (or edit the JSON's `datasource.uid` to match your data source).
2. **Dashboards** → **New** → **Import** → paste `hermem-dashboard.json` or upload the file.
3. Confirm UID `hermem-main`; click **Import**.

The dashboard uid is deterministic (`hermem-main`) so you can also import via URL:
`/dashboard/import/?dashboard=%7B%22uid%22%3A%22hermem-main%22%7D`

### Panels

| Panel         | Query                                                                 |
| ------------- | --------------------------------------------------------------------- |
| Ingest        | `histogram_quantile(0.95, …)` by `category` (observation/world/task/edge) |
| Retrieval     | `histogram_quantile(0.95, …)` by `mode` (search/retrieve/query/response/…) |
| Contradiction | `histogram_quantile(0.95, …)` by `detector` (lexical/composite)       |
| Rerank        | `histogram_quantile(0.95, …)` by `strategy` (llm_openai/llm_ollama/noop) |
| Health        | `up{job="hermem"}` stat panel (green UP / red DOWN)                  |

### Notes

- The `_init` sentinel label values are filtered out via regex `=~"^(…)$"` in every histogram query. These are system-emitted zero-presence children and do not represent real operations.
- Default time range: last 1h; refresh: 30s.
- Data source UID: `prometheus` (change in JSON if yours differs).

---

## Prometheus Alert Rules

File: `ops/prometheus/rules.yml`

### Load

#### With promtool (recommended)

```bash
promtool check rules ops/prometheus/rules.yml
```

#### Via Prometheus config

Add to your `prometheus.yml`:

```yaml
rule_files:
  - "/path/to/ops/prometheus/rules.yml"
```

Then reload: `kill -HUP <prometheus-pid>` or POST `/-/reload`.

#### Via Thanos Ruler / Mimir

Upload or reference the file in your ruler configuration as you would any rule file.

### Alerts

| Rule                      | Severity | Condition                                            | For   |
| ------------------------- | -------- | ---------------------------------------------------- | ----- |
| IngestP95Saturation       | warning  | P95 ingest latency > 5s for any category             | 5m    |
| RetrievalP99Regression    | warning  | P99 retrieval latency > 8s for any mode              | 5m    |
| RerankBudgetExceeded      | critical | P95 rerank latency > 60s for any LLM strategy        | 5m    |
| ContradictionDetectorStall| warning  | composite detector rate == 0 over 1h                 | 1h    |
| HermemScrapeTargetDown    | critical | `up{job="hermem"}` == 0 for 2m                       | 2m    |

All alerts carry labels `severity`, `team: platform` and annotations `summary` + `description` with `{{ $labels }}` templates.

### Prometheus metric families

hermem exposes 4 duration histograms and 17 counters under the `hermem_*` prefix.
See `src/internal/metrics/metrics.go` for the canonical list.
