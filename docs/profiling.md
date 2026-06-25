# Profiling hermem

Hermem ships two complementary profiling surfaces — both opt-in, both
zero-config when the operator decides to opt out.

## 1. Live HTTP endpoints (`/debug/pprof/*`)

Mounted on the standard `hermem serve` mux when `HERMEM_PPROF_ENABLED=1`.
Off by default so a misconfigured deployment cannot accidentally
expose Go runtime internals on a public listener.

```bash
# 30-second CPU profile, save to file, open in browser.
HERMEM_PPROF_ENABLED=1 hermem serve --port 8420 &
SERVER=$!
sleep 1
curl -s -o /tmp/cpu.pprof 'http://localhost:8420/debug/pprof/profile?seconds=30'
go tool pprof -http=:8081 /tmp/cpu.pprof

# Heap snapshot.
curl -s -o /tmp/heap.pprof http://localhost:8420/debug/pprof/heap
go tool pprof -http=:8081 /tmp/heap.pprof

# Goroutine stacks (text).
curl -s http://localhost:8420/debug/pprof/goroutine?debug=2

# 5-second execution trace.
curl -s -o /tmp/trace.out 'http://localhost:8420/debug/pprof/trace?seconds=5'
go tool trace /tmp/trace.out

# Don't forget to disable.
kill $SERVER
```

### Available endpoints

| Path                              | Profile                          |
| --------------------------------- | -------------------------------- |
| `/debug/pprof/`                   | HTML index listing all profiles  |
| `/debug/pprof/cmdline`            | Process command line             |
| `/debug/pprof/profile?seconds=N`  | CPU (default 30s)                |
| `/debug/pprof/symbol`             | Symbol resolver (POST)           |
| `/debug/pprof/trace?seconds=N`    | Execution trace (default 30s)    |
| `/debug/pprof/heap`               | Heap (binary; append `?debug=1` for text) |
| `/debug/pprof/goroutine`          | Goroutine stacks                 |
| `/debug/pprof/threadcreate`       | OS thread creation sites         |
| `/debug/pprof/block`              | Blocking events (enable with `runtime.SetBlockProfileRate`) |
| `/debug/pprof/mutex`              | Mutex contention (enable with `runtime.SetMutexProfileFraction`) |

### Security

- Env flag is the **only** access control. There is no API-key check
  or bind check. Flip the flag when you need a profile, unset it
  afterwards.
- Endpoints expose memory layout and goroutine state. **Do not** bind
  on a public interface; front with a reverse-proxy allowlist if
  remote profiling is required.

## 2. Ad-hoc CLI profiles (`hermem profile ...`)

The `hermem profile` group runs entirely in-process — no daemon, no
live server, no network surface. Useful for capturing a one-shot
profile of the CLI tool itself or a freshly-bootstrapped server
before it accepts traffic.

```bash
# 10-second CPU profile, protobuf to stdout.
hermem profile cpu > /tmp/cpu.pprof
hermem profile cpu 30 > /tmp/cpu.pprof     # override duration

# Heap snapshot -> /tmp/hermem-heap.pprof
hermem profile heap
go tool pprof /tmp/hermem-heap.pprof

# Goroutine dump -> stdout (text).
hermem profile goroutine | less

# 10-second execution trace -> /tmp/hermem-trace.out
hermem profile trace
go tool trace /tmp/hermem-trace.out
hermem profile trace 5 > /tmp/short.out   # override duration
```

### Subcommand reference

| Subcommand              | Output                                | Default duration |
| ----------------------- | ------------------------------------- | ---------------- |
| `hermem profile cpu [s]`   | stdout (raw protobuf)                 | 10s              |
| `hermem profile heap`      | `/tmp/hermem-heap.pprof`              | n/a              |
| `hermem profile goroutine` | stdout (text)                         | n/a              |
| `hermem profile trace [s]` | `/tmp/hermem-trace.out`               | 10s              |

Pass an integer argument or `--seconds N` to override the default
duration on `cpu` and `trace`.

## 3. Analysing profiles

```bash
# Interactive TUI.
go tool pprof /tmp/cpu.pprof

# Web UI on localhost:8081 (auto-starts browser).
go tool pprof -http=:8081 /tmp/cpu.pprof

# Top 10 hot functions, sorted by cumulative time.
go tool pprof -top -cum /tmp/cpu.pprof

# Compare two profiles.
go tool pprof -base=/tmp/cpu-before.pprof /tmp/cpu-after.pprof
```

Trace files use a separate viewer:

```bash
go tool trace /tmp/hermem-trace.out
```

## 4. When to reach for which surface

- **Server is already running** and you want a one-off profile →
  HTTP endpoints + `HERMEM_PPROF_ENABLED=1`. No restart of the
  hermem binary needed once the daemon is alive (you do need to
  restart it after flipping the env, since the flag is read at
  boot — `Serve` wires `RegisterPprof(mux)` inside `mount()`).
- **Server is healthy but you want a profile without restarting it**
  → not possible without a hot-reload hook (not implemented).
  Either restart with the env flag or use the CLI group against a
  sidecar hermem process that imports the same binary.
- **Bug is reproducible without a live server** → CLI group.

## 5. See also

- [USAGE.md § 12 — Operational notes](USAGE.md) for the surrounding
  operational guidance (logging, graceful shutdown, etc).
- `runtime/pprof` package docs — the library backing both surfaces.