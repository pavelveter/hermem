# TODO.md — реестр технического долга из out.txt

> Сгенерировано из `out.txt` (4157 строк, ~20 раундов ревью).
> Отмечайте ☐ → ☑ по мере выполнения. Каждый пункт: проблема → затронутые файлы → предлагаемый код.
>
> **Приоритеты (по критичности):**
> - 🔴 P0 — приводит к crash / data corruption / OOM в проде
> - 🟠 P1 — race / data race / deadlock
> - 🟡 P2 — деградация перформанса / нестабильность UX
> - 🟢 P3 — code-hygiene / архитектурные улучшения

---

## 1. 🔴 HTTP / DoS-устойчивость

### ☑ 1.1 [P0] Безлимитный JSON-жор + OOM через /store, /memory/ingest
**Проблема:** `json.NewDecoder(r.Body).Decode(&req)` без лимита размера тела → атакующий может лить гигабайты в сокет, пока OOM-killer не убьёт процесс. Плюс `DisallowUnknownFields()` не выставлен → опечатки в JSON молча игнорируются ({"querey": "..."} даёт пустой query).
**Где:** `src/internal/server/server.go`, `src/internal/server/requests.go`.

```go
import (
    "encoding/json"
    "net/http"
)

const MaxRequestPayloadSize = 1 << 20 // 1 MiB

// SafeJSONDecode — DoS-safe + schema-strict декодирование
func SafeJSONDecode(w http.ResponseWriter, r *http.Request, dst interface{}) error {
    r.Body = http.MaxBytesReader(w, r.Body, MaxRequestPayloadSize)
    decoder := json.NewDecoder(r.Body)
    decoder.DisallowUnknownFields()
    if err := decoder.Decode(dst); err != nil {
        // MaxBytesReader возвращает *http.MaxBytesError при превышении
        return err
    }
    return nil
}
```

### ☑ 1.2 [P0] `time.Tick` утечка памяти в планировщике
**Проблема:** `time.Tick` создаёт тикер, который **невозможно остановить или заставить GC собрать** → пожизненная утечка памяти + ticker живёт даже после возврата функции.
**Где:** любой долгоживущий планировщик / GC-цикл /retention tick.

```go
// Было:
for range time.Tick(interval) { ... }

// Нужно:
ticker := time.NewTicker(interval)
defer ticker.Stop()
for {
    select {
    case <-ctx.Done():
        return ctx.Err()
    case <-ticker.C:
        // работа
    }
}
```

---

## 2. 🔴 Миграции / атомарность схемы

### ☑ 2.1 [P0] Не-атомарные миграции → дубли таблиц при concurrent startup
**Проблема:** Миграции запускаются без транзакции. Два инстанса стартуют одновременно → оба пытаются создать таблицы → второй фейлит или, хуже, создаёт дубликаты (если без `IF NOT EXISTS`).
**Где:** `src/internal/store/migration.go`.

```go
func RunMigrationsSafe(db *sql.DB) error {
    // Нужно: атомарное обновление user_version вместе с DDL
    tx, err := db.BeginTx(context.Background(), nil)
    if err != nil {
        return fmt.Errorf("begin tx: %w", err)
    }
    defer tx.Rollback() // безопасен после Commit

    var currentVersion int
    if err := tx.QueryRow(`PRAGMA user_version`).Scan(&currentVersion); err != nil {
        return err
    }

    for _, m := range allMigrations {
        if m.version <= currentVersion {
            continue
        }
        if _, err := tx.Exec(m.ddl); err != nil { // ddl содержит IF NOT EXISTS
            return fmt.Errorf("migration %d: %w", m.version, err)
        }
    }

    if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, latestVersion)); err != nil {
        return err
    }
    return tx.Commit()
}
```

### ☑ 2.2 [P0] TOCTOU race при создании ребра (`AddEdge`)
**Проблема:** Между `SELECT COUNT(*)` (проверка существования) и `INSERT` нет транзакции → две горутины могут вставить одно и то же ребро дважды, либо вставить ребро после удаления родителя.
**Где:** `src/internal/store/edge.go`.

```go
func AddEdge(ctx context.Context, db *sql.DB, e Edge) error {
    // BEGIN IMMEDIATE — захватывает write-lock сразу, исключая TOCTOU
    tx, err := db.BeginTx(ctx, &sql.TxOptions{
        Isolation: sql.LevelSerializable,
    })
    if err != nil {
        return err
    }
    defer tx.Rollback()

    var exists int
    err = tx.QueryRowContext(ctx,
        `SELECT COUNT(*) FROM edges WHERE src = ? AND dst = ? AND relation = ?`,
        e.Src, e.Dst, e.Relation,
    ).Scan(&exists)
    if err != nil {
        return err
    }
    if exists > 0 {
        return ErrEdgeExists
    }

    if _, err := tx.ExecContext(ctx,
        `INSERT INTO edges (src, dst, relation, weight, created_at)
         VALUES (?, ?, ?, ?, ?)`,
        e.Src, e.Dst, e.Relation, e.Weight, time.Now().UTC().Unix(),
    ); err != nil {
        return err
    }
    return tx.Commit()
}
```

---

## 3. 🔴 SQLite под нагрузкой (конкурентность)

### ☑ 3.1 [P0] Дефолтный SQLite сыплет `database is locked`
**Проблема:** Без `PRAGMA journal_mode = WAL`, без `_busy_timeout`, без ограничения пула коннектов — параллельные HTTP-запросы ловят `SQLITE_BUSY` и 5xx-ят клиента.
**Где:** `src/internal/store/store.go` / место, где открывается `sql.DB`.

```go
db, err := sql.Open("sqlite3", dsn+"?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL")
if err != nil { return nil, err }

// Один писатель — иначе всё равно ловим lock-ы на фоне WAL
db.SetMaxOpenConns(1)
db.SetMaxIdleConns(1)
db.SetConnMaxLifetime(0)
```

### ☑ 3.2 [P0] `UpsertEntity` не ретраит `SQLITE_BUSY`
**Проблема:** Transient `SQLITE_BUSY` под нагрузкой → 5xx без шанса на recovery.
**Где:** `src/internal/ingestion/worker.go`.

```go
func (w *IngestionWorker) UpsertEntityWithRetry(ctx context.Context, e Entity) error {
    const maxAttempts = 5
    backoff := 50 * time.Millisecond
    for attempt := 1; attempt <= maxAttempts; attempt++ {
        err := w.upsertOnce(ctx, e)
        if err == nil {
            return nil
        }
        if !errors.Is(err, sqlite3.ErrBusy) && !strings.Contains(err.Error(), "database is locked") {
            return err
        }
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-time.After(backoff):
        }
        backoff *= 2
    }
    return fmt.Errorf("upsert: exhausted retries")
}
```

---

## 4. 🔴 Archival / GC

### ☑ 4.1 [P0] `nil` panic в gc.go — не проверен `db.BeginTx`
**Проблема:** `tx, _ := db.BeginTx(...)` — при `database is locked` `tx == nil`, а дальше `tx.QueryRow(...)` → `nil pointer dereference` → SIGSEGV неуловимый `defer recover()` (потому что recover в другой горутине).
**Где:** `src/internal/gc.go`.

```go
func GarbageCollector(ctx context.Context, db *sql.DB, vi VectorIndex, policy RetentionPolicy) {
    ticker := time.NewTicker(policy.RunInterval)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
        }
        if err := archiveBatch(ctx, db, vi, policy); err != nil {
            log.Printf("gc batch failed: %v", err)
        }
    }
}

func archiveBatch(ctx context.Context, db *sql.DB, vi VectorIndex, p RetentionPolicy) error {
    tx, err := db.BeginTx(ctx, nil)
    if err != nil {
        return fmt.Errorf("begin tx: %w", err)
    }
    defer tx.Rollback() // безопасен после Commit

    cutoff := time.Now().UTC().Add(-p.ObservationTTL).Unix()
    rows, err := tx.QueryContext(ctx,
        `SELECT id FROM observations WHERE updated_at < ? AND kind = 'observation' LIMIT ?`,
        cutoff, p.DeleteBatchSize,
    )
    if err != nil {
        return err
    }
    var ids []string
    for rows.Next() {
        var id string
        if err := rows.Scan(&id); err != nil {
            rows.Close()
            return err
        }
        ids = append(ids, id)
    }
    rows.Close()
    if err := rows.Err(); err != nil {
        return err
    }
    if len(ids) == 0 {
        return tx.Commit()
    }
    if _, err := tx.ExecContext(ctx,
        `UPDATE observations SET archived = 1 WHERE id IN (`+placeholders(len(ids))+`)`,
        idsToArgs(ids)...,
    ); err != nil {
        return err
    }
    if err := tx.Commit(); err != nil {
        return err // ничего не удалено из vi
    }
    // Только теперь синхронизируем индекс — никаких призраков
    for _, id := range ids {
        _ = vi.Remove(ctx, id)
    }
    return nil
}
```

### ☑ 4.2 [P0] Foreign-key не enforced → висячие рёбра при purge
**Проблема:** Удаление наблюдений оставляет edges указывающими на несуществующие сущности, ORDefK-violations.
**Где:** `src/internal/store/migration.go` + `gc.go`.

```go
// В RunMigrationsSafe — первой строкой DDL каждого подключения:
if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil { ... }

// А в GC — сначала edges, потом entities (или RESTRICT FK уже не пустит).
if _, err := tx.ExecContext(ctx,
    `DELETE FROM edges WHERE dst IN (`+placeholders(len(ids))+`)`,
    idsToArgs(ids)...,
); err != nil { return err }
```

---

## 5. 🟠 AI-слой (embedders / extractors / reranker)

### ☐ 5.1 [P0] Сетевые вызовы игнорируют `context.Context`
**Проблема:** `http.NewRequest(...)` без `WithContext` → таймаут конфига не применяется к in-flight запросу, отмена клиента не прерывает LLM stream.
**Где:** `src/embedder.go`, `src/extractor.go`, любые места создания `http.NewRequest`.

```go
req, err := http.NewRequestWithContext(ctx, "POST", url, body)
if err != nil { return err }
// ... дальше req, как обычно
```

### ☐ 5.2 [P1] `OpenAIReranker` — плацебо, не парсит ответ
**Проблема:** Возвращает исходные данные без обращения к API или возвращает их как есть после вызова API — реранкер не реранкрит ничего.
**Где:** `src/internal/ai/reranker.go` (если появится) — либо в текущем коде OpenAI.

```go
type openaiRerankResponse struct {
    Results []struct {
        Index    int     `json:"index"`
        Score    float64 `json:"relevance_score"`
    } `json:"results"`
}

func (r *OpenAIReranker) Rerank(ctx context.Context, q string, docs []Doc) ([]Doc, error) {
    // ... POST запрос с контекстом ...
    var resp openaiRerankResponse
    if err := json.NewDecoder(resp.Body).Decode(&resp); err != nil {
        return nil, err
    }
    sorted := make([]Doc, len(resp.Results))
    for _, item := range resp.Results {
        if item.Index < 0 || item.Index >= len(docs) {
            continue
        }
        sorted = append(sorted, docs[item.Index]) // осторожно: плотная индексация
    }
    return sorted, nil
}
```

### ☐ 5.3 [P1] Нет retry на 503/429 в OllamaEmbedder
**Проблема:** Ollama при горячей загрузке модели возвращает 503 — без retry запрос падает.
**Где:** `src/embedder.go`.

```go
func (e *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
    const maxAttempts = 5
    backoff := 200 * time.Millisecond
    var lastErr error
    for attempt := 1; attempt <= maxAttempts; attempt++ {
        body, err := e.embedOnce(ctx, text)
        if err == nil {
            return body, nil
        }
        lastErr = err
        if !isRetryable(err) {
            return nil, err
        }
        select {
        case <-ctx.Done():
            return nil, ctx.Err()
        case <-time.After(backoff):
        }
        backoff *= 2
    }
    return nil, fmt.Errorf("ollama embed: %w (after %d attempts)", lastErr, maxAttempts)
}

func isRetryable(err error) bool {
    if err == nil { return false }
    s := err.Error()
    return strings.Contains(s, "503") || strings.Contains(s, "429") ||
           errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ETIMEDOUT)
}
```

### ☐ 5.4 [P1] `ResilientClient` для AI-слоя — единая retry-обвязка
**Где:** `src/internal/ai/embedder.go` / новый `client.go`.

```go
type ResilientClient struct {
    Inner    *http.Client
    Backoffs []time.Duration
}

func (c *ResilientClient) DoWithRetry(ctx context.Context, req *http.Request) (*http.Response, error) {
    if len(c.Backoffs) == 0 {
        c.Backoffs = []time.Duration{
            200 * time.Millisecond, 500 * time.Millisecond, 1 * time.Second, 2 * time.Second,
        }
    }
    req = req.WithContext(ctx)
    var lastErr error
    for i, backoff := range append([]time.Duration{0}, c.Backoffs...) {
        if backoff > 0 {
            select {
            case <-ctx.Done():
                return nil, ctx.Err()
            case <-time.After(backoff):
            }
        }
        resp, err := c.Inner.Do(req.Clone(ctx))
        if err == nil && resp.StatusCode < 500 && resp.StatusCode != 429 {
            return resp, nil
        }
        if err != nil { lastErr = err } else { lastErr = fmt.Errorf("status %d", resp.StatusCode); resp.Body.Close() }
        _ = i
    }
    return nil, fmt.Errorf("resilient: exhausted retries: %w", lastErr)
}
```

---

## 6. 🟠 Vector index (concurrency)

### ☐ 6.1 [P1] Data race в `InMemoryVectorIndex.Append` + `Search`
**Проблема:** `append(matrix, vec...)` вызывает переаллокацию + копирование; параллельный `Search` читает старую/мусорную память → `runtime error: index out of range` или data corruption.
**Где:** `src/internal/vector/vector.go`, `src/vector_inmemory.go`.

```go
type InMemoryVectorIndex struct {
    mu         sync.RWMutex
    entries    []vectorEntry
    flatMatrix []float32
    cols       int
    byID       map[string]int
}

func (idx *InMemoryVectorIndex) Store(ctx context.Context, id string, vec []float32) error {
    idx.mu.Lock()
    defer idx.mu.Unlock()
    // Копируем, чтобы вызывающий код мог мутировать свой слайс
    safe := make([]float32, len(vec))
    copy(safe, vec)
    entry := vectorEntry{id: id, vec: safe, norm: vectorNorm(safe)}
    if i, ok := idx.byID[id]; ok {
        idx.entries[i] = entry
        copy(idx.flatMatrix[i*idx.cols:(i+1)*idx.cols], safe)
        return nil
    }
    idx.entries = append(idx.entries, entry)
    idx.byID[id] = len(idx.entries) - 1
    idx.flatMatrix = append(idx.flatMatrix, safe...)
    return nil
}

func (idx *InMemoryVectorIndex) Search(ctx context.Context, q []float32, k int) ([]SearchHit, error) {
    idx.mu.RLock()
    n, cols := len(idx.entries), idx.cols
    if n == 0 {
        idx.mu.RUnlock()
        return nil, nil
    }
    flat := idx.flatMatrix // снимок указателя; сам массив не мутируется пока RLock держится
    ents := idx.entries
    idx.mu.RUnlock()

    dots := make([]float32, n)
    BatchDotProducts(q, flat, n, cols, dots)
    // ... ранжирование ...
}
```

### ☐ 6.2 [P2] «Зомби-векторы» при фоновом reembed
**Проблема:** Фоновый reembed-воркер записывает новые векторы в SQLite, но `InMemoryVectorIndex` в живом сервере **не знает** об изменениях → поиск выдаёт старые float'ы, пока сервер не рестартанёт.
**Где:** `src/internal/algo/reembed.go` + `VectorIndex` интерфейс.

```go
type VectorIndex interface {
    UpdateVector(ctx context.Context, id string, vec []float32) error
    Commit(ctx context.Context) error  // пересборка матриц для read-only бэкендов
    // ... существующие методы ...
}

func (rw *ReembedWorker) ProcessReembedBatch(ctx context.Context, tasks []ReembedTask,
    embedFn func(string) ([]float32, error)) error {
    for _, task := range tasks {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }
        vec, err := embedFn(task.NewText)
        if err != nil {
            return fmt.Errorf("reembed %d: %w", task.EntityID, err)
        }
        if err := rw.Store.UpdateVector(ctx, task.EntityID, vec); err != nil {
            return err
        }
    }
    return rw.Index.Commit(ctx) // батч-коммит для sqlite-vec / faiss-rebuild
}
```

### ☑ 6.3 [P0] Divide-by-zero в `QuantizeFloat32ToInt8` на плоских векторах
**Проблема:** `scale = (max - min) / 255.0` → 0 при `max == min`. Деление даёт `+Inf`, каст в int8 → `-128` мусор, индекс разрушен.
**Где:** `src/internal/vector/quantize.go`.

```go
func QuantizeFloat32ToInt8(vec []float32) ([]int8, float32, float32) {
    if len(vec) == 0 {
        return nil, 0, 0
    }
    minVal, maxVal := vec[0], vec[0]
    for _, v := range vec {
        if v < minVal { minVal = v }
        if v > maxVal { maxVal = v }
    }
    valRange := maxVal - minVal
    var scale float32
    if math.Abs(float64(valRange)) < 1e-6 {
        scale = 1.0 // Избегаем деления на 0; результат будет стабильно "minVal → 0"
    } else {
        scale = valRange / 255.0
    }
    out := make([]int8, len(vec))
    for i, v := range vec {
        scaled := (v - minVal) / scale
        out[i] = int8(math.Round(float64(scaled))) - 128
    }
    return out, minVal, scale
}
```

### ☐ 6.4 [P2] `sync.Pool` для zero-alloc quantization
**Где:** `src/internal/vector/quantize.go`.

```go
var quantBufPool = sync.Pool{
    New: func() interface{} { b := make([]int8, 0, 1024); return &b },
}

func QuantizeSQ8PreAlloc(vec []float32, minV, scale float32) []int8 {
    bufPtr := quantBufPool.Get().(*[]int8)
    buf := (*bufPtr)[:0]
    if cap(buf) < len(vec) {
        buf = make([]int8, len(vec))
    } else {
        buf = buf[:len(vec)]
    }
    for i, v := range vec {
        buf[i] = int8(math.Round(float64((v-minV)/scale))) - 128
    }
    quantBufPool.Put(bufPtr)
    return buf
}
```

---

## 7. 🟠 Retrieval / walking graph

### ☐ 7.1 [P0] Рекурсивный CTE зацикливается на циклическом графе
**Проблема:** `WITH RECURSIVE walk(id, depth) AS (...)` без `path`-трекинга уходит в бесконечность, если граф содержит цикл (a→b→a).
**Где:** `src/internal/retrieval/walk.go`.

```go
func WalkGraphSafe(ctx context.Context, db *sql.DB, seeds []string, maxDepth int) error {
    if maxDepth < 0 { maxDepth = 0 }
    if maxDepth > 5 { maxDepth = 5 } // hard ceiling — защита от runaway запросов
    placeholders := strings.Repeat("?,", len(seeds))
    placeholders = placeholders[:len(placeholders)-1]
    args := make([]interface{}, len(seeds))
    for i, s := range seeds { args[i] = s }

    q := fmt.Sprintf(`
        WITH RECURSIVE walk(id, depth, path) AS (
            SELECT id, 0, ',' || id || ','
              FROM nodes WHERE id IN (%s)
            UNION ALL
            SELECT e.dst, w.depth + 1, w.path || e.dst || ','
              FROM walk w
              JOIN edges e ON e.src = w.id
             WHERE w.depth < ?
               AND w.path NOT LIKE ('%,' || e.dst || ',%')  -- cycle break
        )
        SELECT DISTINCT id, depth FROM walk ORDER BY depth
    `, placeholders)
    args = append(args, maxDepth)
    // ... rows execute ...
}
```

### ☐ 7.2 [P2] `WalkGraphFromSeeds` без лимита глубины → SQLite stack overflow / OOM
**Проблема:** Depth из конфига без потолка может прилететь как 10⁶ → SQLite жрёт RAM.
**Где:** `src/internal/retrieval/walk.go`.

```go
// Hard-cap depth перед запросом
if maxDepth > WalkDepthCeiling {
    maxDepth = WalkDepthCeiling
}
// А внутри самого CTE: WHERE 0 < 0 + ? (тот же maxDepth)
```

### ☐ 7.3 [P2] Louvain рандомизирован → плавающие communities
**Проблема:** `for nodeID := range g.Nodes` — Go рандомизирует map iteration → алгоритм сходится к разным кластерам при каждом запуске на тех же данных. GraphRAG non-deterministic.
**Где:** `src/internal/algo/community.go`.

```go
func (g *Graph) OptimizeModularityDeterministic() {
    if len(g.Nodes) == 0 { return }
    nodeIDs := make([]int64, 0, len(g.Nodes))
    for id := range g.Nodes {
        nodeIDs = append(nodeIDs, id)
    }
    sort.Slice(nodeIDs, func(i, j int) bool { return nodeIDs[i] < nodeIDs[j] })
    for _, nodeID := range nodeIDs {
        n := g.Nodes[nodeID]
        _ = n // ΔQ, move(...)
    }
}
```

---

## 8. 🟡 Retrieval: scoring / contradictions / response

### ☐ 8.1 [P2] `BuildLLMContext` thrashing-конкатенация строк
**Проблема:** `s += f.Content + "\n"` в цикле → на 100k фактов это аллокация аллокации, GC-давление убивает latency.
**Где:** `src/internal/retrieval/response.go`.

```go
func BuildLLMContext(facts []Fact, budgetTokens int) string {
    var sb strings.Builder
    sb.Grow(len(facts) * 256) // pre-alloc по эвристике
    tokens := 0
    for _, f := range facts {
        if tokens+f.EstTokens > budgetTokens { break }
        sb.WriteString("- ")
        sb.WriteString(f.Content)
        sb.WriteByte('\n')
        tokens += f.EstTokens
    }
    return sb.String()
}
```

### ☐ 8.2 [P1] `SortFactsByScore` паникует на NaN/Inf
**Проблема:** Деление на ноль в формуле скоринга → `rank = math.NaN()`. `sort.Slice` с NaN — поведение undefined, на некоторых реализациях — runtime panic.
**Где:** `src/internal/retrieval/scoring.go`.

```go
func SanitizeScore(s float64) float64 {
    if math.IsNaN(s) || math.IsInf(s, 0) {
        return -1.0 // самый нижний ранг
    }
    if s < -1e9 { return -1e9 }
    if s >  1e9 { return  1e9 }
    return s
}

func SortFactsByScoreSafe(facts []Fact) {
    for i := range facts {
        facts[i].Score = SanitizeScore(facts[i].Score)
    }
    sort.SliceStable(facts, func(i, j int) bool {
        if facts[i].Score != facts[j].Score {
            return facts[i].Score > facts[j].Score
        }
        return facts[i].ID < facts[j].ID // tie-break детерминирован
    })
}
```

### ☐ 8.3 [P2] `ContradictionCheck` — O(N²) LLM-вызовов
**Проблема:** Для каждой пары фактов — отдельный LLM-вызов. На 100 фактах это 4950 запросов = $$$+ latency.
**Где:** `src/internal/retrieval/contradictions.go`.

```go
// Шаг 1: cosine pre-filter (бесплатно)
type Indexed struct { ID string; Vec []float32 }
type Bucket struct { Kind string; Items []Indexed }

buckets := map[string][]Indexed{}
for _, f := range facts { buckets[f.Kind] = append(buckets[f.Kind], f) }

suspicious := []FactPair{}
for _, group := range buckets {
    for i := 0; i < len(group); i++ {
        for j := i+1; j < len(group); j++ {
            if Cosine(group[i].Vec, group[j].Vec) < 0.6 {
                suspicious = append(suspicious, FactPair{group[i], group[j]})
            }
        }
    }
}
// Шаг 2: LLM только по suspicious (а не по всей квадратной матрице).
// Линейно по количеству suspicious, не квадратично по всем фактам.
```

### ☐ 8.4 [P1] `Float32ToBytes` / `BytesToFloat32` без проверок → коррапция
**Проблема:** Битый blob может содержать NaN-биты → embedding с NaN попадает в `flatMatrix` → `BatchDotProducts` возвращает NaN-результаты, поиск ломается.
**Где:** `src/vector.go`, `src/internal/vector/encoding.go`.

```go
func BytesToFloat32Safe(b []byte) ([]float32, error) {
    if len(b)%4 != 0 {
        return nil, fmt.Errorf("blob length %d not multiple of 4", len(b))
    }
    out := make([]float32, len(b)/4)
    for i := range out {
        bits := binary.LittleEndian.Uint32(b[i*4:])
        f := math.Float32frombits(bits)
        if math.IsNaN(float64(f)) || math.IsInf(float64(f), 0) {
            return nil, fmt.Errorf("blob[%d] is NaN/Inf; rejecting", i)
        }
        out[i] = f
    }
    return out, nil
}
```

---

## 9. 🟠 Temporal / Provenance

### ☐ 9.1 [P1] Temporal engine ломается на разных TZ
**Проблема:** Хранит `time.Time` локального сервера / Docker-host. TZ контейнера vs TZ хоста → drift в обновлениях ranking, «факт вчера» отображается как «сегодня».
**Где:** temporal decay / scoring по `updated_at`.

```go
// Всегда UTC Unix seconds (int64) — никаких time.Time с TZ в БД
func NowUTCUnix() int64 { return time.Now().UTC().Unix() }

// Не делаем .Sub() через time.Time в SQL, передаём int64
// WHERE updated_at < ?  -- ? -- это int64 Unix seconds
```

### ☐ 9.2 [P1] Provenance nil-pointer при удалённом source
**Проблема:** `graph/provenance.go` итерирует lineage узлов; source/log удалён → deref nil → SIGSEGV.
**Где:** `src/internal/graph/provenance.go`.

```go
func SafeSourceLabel(s *Source) string {
    if s == nil || s.ID == "" {
        return "unknown_or_deleted_source"
    }
    return s.Label
}

func WalkLineage(nodes []Node) []LineageEntry {
    out := make([]LineageEntry, 0, len(nodes))
    for _, n := range nodes {
        out = append(out, LineageEntry{
            FactID:    n.ID,
            SourceTag: SafeSourceLabel(n.Source),
            CreatedAt: n.CreatedAt,
        })
    }
    return out
}
```

---

## 10. 🟠 Agent loop / CLI

### ☐ 10.1 [P1] Busy-wait + leak в `agent/loop.go`
**Проблема:** Цикл без ticker'а или с `time.Tick` → busy-spin CPU и mery-leak.
**Где:** `src/internal/agent/loop.go`.

```go
func (a *Agent) RunLoop(ctx context.Context) error {
    ticker := time.NewTicker(a.PollInterval)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-ticker.C:
        }
        stepCtx, cancel := context.WithTimeout(ctx, a.StepTimeout)
        if err := a.RunStep(stepCtx); err != nil {
            cancel()
            a.Logf("step err: %v", err)
            continue
        }
        cancel() // обязательно освобождаем подконтекст
    }
}
```

### ☐ 10.2 [P1] Panic от custom-tool крашит весь процесс
**Проблема:** Любой паник в tool-обработчике вылетает из всей горутины агента → теряются in-flight шаги.
**Где:** `src/internal/agent/loop.go`.

```go
func (a *Agent) RunStep(ctx context.Context) (err error) {
    defer func() {
        if r := recover(); r != nil {
            err = fmt.Errorf("panic recovered: %v\n%s", r, debug.Stack())
        }
    }()
    return a.doStep(ctx)
}
```

### ☐ 10.3 [P2] CLI `os.Stdout` без EPIPE-handling
**Проблема:** `hermem memory query | head -n 1` → head закрывает pipe → `SIGPIPE` → ugly trace в консоль, exit != 0.
**Где:** `src/cmd/...`, `src/internal/cli/cli.go`.

```go
func SafeWriteStdout(p []byte) error {
    _, err := os.Stdout.Write(p)
    if errors.Is(err, syscall.EPIPE) {
        return nil  // downstream закрылось — наша работа закончена, exit 0
    }
    return err
}
```

### ☐ 10.4 [P2] Cobra commands не используют `cmd.Context()`
**Проблема:** Handlers стартуют с `context.Background()` вместо `cmd.Context()` → Ctrl+C / `--timeout` не отменяют текущий запрос.
**Где:** все Cobra `RunE` в `src/cmd`.

```go
// Было:
RunE: func(cmd *cobra.Command, args []string) error {
    return doWork(context.Background(), args)
},
// Нужно:
RunE: func(cmd *cobra.Command, args []string) error {
    return doWork(cmd.Context(), args)
},
```

### ☐ 10.5 [P2] Stdin pipe-detection (защита от зависания в non-TTY)
**Проблема:** `os.Stdin.Read(...)` без проверки pipe → если stdin — канал от CI без данных, агент виснет навечно.
**Где:** `src/cmd/*` интерактивные команды.

```go
func IsInteractive() bool {
    fi, err := os.Stdin.Stat()
    if err != nil { return false }
    return (fi.Mode() & os.ModeCharDevice) != 0
}
```

---

## 11. 🟡 Конфигурация / lifecycle

### ☐ 11.1 [P1] Hot-reload конфига → race condition
**Проблема:** Глобальный `var CurrentEnv *Env` мутируется под нагрузкой → читатели получают half-mutated snapshot.
**Где:** `src/internal/cli/env/env.go`.

```go
type EnvManager struct {
    current atomic.Pointer[Env]
}

func (m *EnvManager) Get() *Env { return m.current.Load() }
func (m *EnvManager) Reload(ctx context.Context, path string) error {
    cfg, err := LoadConfig(path)
    if err != nil { return err }
    if err := cfg.Validate(); err != nil { return err }
    m.current.Store(&Env{Cfg: cfg, LoadedAt: time.Now().UTC().Unix()})
    return nil
}
```

### ☐ 11.2 [P2] `Config.Validate()` отсутствует
**Проблема:** `VectorDim = 0`, отрицательные таймауты, непарсибельный URL → runtime-паники глубоко в графе.
**Где:** `src/config.go`.

```go
func (c *Config) Validate() error {
    if c.VectorDim <= 0 { return errors.New("vector.dim must be > 0") }
    if c.EmbedderTimeout <= 0 { return errors.New("embedder.timeout must be > 0") }
    if c.ExtractTimeout <= 0 { return errors.New("extraction.timeout must be > 0") }
    if c.URL != "" {
        if _, err := url.Parse(c.URL); err != nil { return fmt.Errorf("embedder.url: %w", err) }
    }
    if c.Retention.ObservationTTL <= 0 { return errors.New("retention.observation_ttl must be > 0") }
    if c.Retention.RunInterval <= 0 { return errors.New("retention.run_interval must be > 0") }
    return nil
}
```

### ☐ 11.3 [P2] `regexp.MustCompile` на user-input → паника при старте
**Проблема:** Если конфиг хранит pattern и компилится через `MustCompile`, невалидный regex паникует на старте сервиса вместо возврата ошибки.
**Где:** любой конфиг-pattern парсер.

```go
re, err := regexp.Compile(pattern)
if err != nil {
    return fmt.Errorf("config regex invalid: %w", err)
}
```

---

## 12. 🟡 Контекстная конфигурация CLI

### ☐ 12.1 [P3] Глобальный singleton `CurrentEnv` → races в тестах
**Проблема:** `var CurrentEnv *Env` — параллельные тесты мутируют один и тот же объект.
**Где:** `src/internal/cli/env/env.go`.

```go
type contextKey struct{}
var envCtxKey = contextKey{}

func SetEnvInContext(parent context.Context, env *Env) context.Context {
    return context.WithValue(parent, envCtxKey, env)
}
func GetEnvFromContext(ctx context.Context) (*Env, error) {
    v := ctx.Value(envCtxKey)
    if v == nil { return nil, errors.New("env missing in context") }
    env, ok := v.(*Env)
    if !ok { return nil, errors.New("env type mismatch") }
    return env, nil
}
```

---

## 13. 🟡 Task Graph Scheduler

### ☐ 13.1 [P1] Stack overflow на циклических задачах
**Проблема:** `func (t *Task) Resolve()` рекурсивно вызывает зависимые → цикл в deps = stack overflow.
**Где:** `src/internal/task/dep.go`.

```go
type DependencyGraph struct { Tasks map[string]*Task }

func (g *DependencyGraph) HasCycle() error {
    WHITE, GRAY, BLACK := 0, 1, 2
    color := map[string]int{}
    var dfs func(string) error
    dfs = func(id string) error {
        switch color[id] {
        case GRAY:  return fmt.Errorf("cycle at task %q", id)
        case BLACK: return nil
        }
        color[id] = GRAY
        for _, dep := range g.Tasks[id].Deps {
            if err := dfs(dep); err != nil { return err }
        }
        color[id] = BLACK
        return nil
    }
    for id := range g.Tasks {
        if err := dfs(id); err != nil { return err }
    }
    return nil
}

func (g *DependencyGraph) SortTasksSafe() ([]string, error) {
    if err := g.HasCycle(); err != nil { return nil, err }
    // topological sort (Kahn) — линеен, не рекурсивен
    inDeg := map[string]int{}
    for id, t := range g.Tasks {
        if _, ok := inDeg[id]; !ok { inDeg[id] = 0 }
        for _, d := range t.Deps { inDeg[d]++ }
    }
    queue := []string{}
    for id, d := range inDeg {
        if d == 0 { queue = append(queue, id) }
    }
    out := []string{}
    for len(queue) > 0 {
        id := queue[0]; queue = queue[1:]
        out = append(out, id)
        for _, dep := range g.Tasks[id].Deps {
            inDeg[dep]--
            if inDeg[dep] == 0 { queue = append(queue, dep) }
        }
    }
    return out, nil
}
```

---

## 14. 🟡 Unbounded concurrency / goroutine hygiene

### ☐ 14.1 [P2] `SafeGo` обёртка для фоновых горутин
**Проблема:** Любая паника в горутине убивает процесс (recover() в main горутине не ловит).
**Где:** `src/internal/util/safego/safego.go` (новый пакет).

```go
package safego

import (
    "context"
    "fmt"
    "log"
    "runtime/debug"
)

func Go(ctx context.Context, name string, fn func(context.Context) error) {
    go func() {
        defer func() {
            if r := recover(); r != nil {
                log.Printf("safego[%s] panic: %v\n%s", name, r, debug.Stack())
            }
        }()
        if err := fn(ctx); err != nil {
            log.Printf("safego[%s] exit err: %v", name, err)
        }
    }()
}
```

### ☐ 14.2 [P2] Bounded concurrency для ingestion worker
**Проблема:** `go processChunk(chunk)` на каждый chunk → без лимита → OOM.
**Где:** `src/internal/ingestion/worker.go`.

```go
func (w *IngestionWorker) Run(ctx context.Context, chunks []Chunk) error {
    const maxParallel = 8
    sem := make(chan struct{}, maxParallel)
    errs := make(chan error, len(chunks))
    var wg sync.WaitGroup
    for _, ch := range chunks {
        wg.Add(1)
        sem <- struct{}{}
        go func(c Chunk) {
            defer wg.Done()
            defer func() { <-sem }()
            if err := w.processChunk(ctx, c); err != nil {
                errs <- err
            }
        }(ch)
    }
    wg.Wait()
    close(errs)
    for err := range errs {
        if err != nil { return err }
    }
    return nil
}
```

---

## 15. 🟡 Heavy embedder cancellation

### ☐ 15.1 [P1] Контекст клиента не прерывает in-flight inference
**Проблема:** HTTP-клиент отвалился, но LLM продолжает генерировать → VRAM/RAM утекает на каждом таймауте.
**Где:** `src/internal/server/middleware.go` (embedder invocation).

```go
// В обработчике запроса использовать r.Context(), а НЕ context.Background()
func (h *HeavyEmbedder) Embed(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    vec, err := h.backend.Embed(ctx, r.PostFormValue("text"))
    if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
        // клиент ушёл — нас уже не спрашивают
        return
    }
    // ... write JSON ...
}
```

---

## 16. 🟡 Misc / cross-cutting

### ☐ 16.1 [P3] int64 ID vs float-based math metric
**Проблема:** Скоринг-метрики иногда хранятся как float в одной таблице, а IDs — int64. При JOIN возможны потери точности или неоднозначность тип-кастов.
**Где:** места с `SELECT id, score` — проследить, что score всегда float64 + id всегда int64.

### ☐ 16.2 [P3] LRU cache «zombi-head pointer»
**Проблема:** После eviction указатель на head остаётся живым → двойной free или use-after-free в Go-обёртках.
**Где:** `src/internal/cache/cache.go`.

```go
func (c *LRU) Remove(e *Entry) {
    if e == c.head || e == c.tail { // защита от self-loop
        return
    }
    e.Prev.Next = e.Next
    e.Next.Prev = e.Prev
    e.Prev, e.Next = nil, nil // ликвидируем potential dangling refs
}
```

---

## 17. 🟢 Финальный verification checklist (post-impl)

После прохождения всех P0/P1, прогнать:

- ☐ 17.1 `gofmt -l ./src` — пусто
- ☐ 17.2 `go vet ./src/...` — clean
- ☐ 17.3 `go build -o /tmp/hermem ./src` — OK
- ☐ 17.4 `go test -race -count=1 ./src/...` — green
- ☐ 17.5 `go test -bench=. -benchtime=2s ./src/vector/...` — benchmark delta записан
- ☐ 17.6 `wrk -t4 -c32 -d30s http://localhost:PORT/health` без 5xx на 1k QPS
- ☐ 17.7 Manual soak: 1 час непрерывных ingest+query под `runtime/pprof` — ни одного OOM/panic в логах
- ☐ 17.8 Все Cobra-команды уважают `cmd.Context()` (тест: SIGINT во время долгой команды → exit code 130 без trace)
- ☐ 17.9 Конфиг с `vector.dim = 0` или `timeout = -1s` стартует с конкретной ошибкой валидации, **не** паникует
- ☐ 17.10 `hermem memory query --text "x" | head -1` корректно завершается с exit 0, без EPIPE в stderr

---

## Сводка по приоритетам

| Приоритет | Кол-во | Блокеры прода |
|-----------|--------|---------------|
| 🔴 P0 | 9 | да |
| 🟠 P1 | 11 | частично |
| 🟡 P2 | 12 | нет, но деградирует |
| 🟢 P3 | 4 | нет |
| ИТОГО | **36** | — |

> **Рекомендация:** внедрять строго по приоритетам сверху вниз. Каждый P0-пункт должен идти отдельным коммитом с регрессионным тестом + записью в `CHANGELOG.md` под `[Unreleased]`.
