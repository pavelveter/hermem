# ADR-014: Recursion and Depth Guards

## Status
Accepted

## Context
Static analysis flagged `store.recovery.CascadeRollback` as recursive with `transitive_loop_depth=5` and `recursion-in-loop=true`. On inspection, the recursion was genuine (self-call inside a for-range loop over dependents). Long dep-chains or adversarial task graphs (>10k nodes) would blow the stack.

Other recursion hits (`Logger.Error`, `serverstate.Load/Store`, `middleware.Write/WriteHeader`, `task.ClaimNextTask`, `evaluation.report.Format`, `tracing.context.StartSpan`) were audited and confirmed false positives — either one-liner forwarders or non-recursive wrappers.

## Decision
1. **Iterative BFS** — `CascadeRollback` rewritten from recursive DFS to iterative BFS using an explicit queue. The visited-set cycle guard is preserved.
2. **Hard depth cap** — `SchemaConfig.CascadeLimit` (default 4096) limits total tasks processed per invocation. Returns `ErrCascadeLimit` with the partial result when exceeded.
3. **Preserved semantics** — `[]core.Task, error` return signature unchanged. Partial failure (first error) + already-rolled-back skip behavior preserved exactly.
4. **ADR audit** — All other static-analysis recursion hits documented as false positives.

## Consequences
- No stack overflow risk regardless of task graph size
- Configurable limit allows tuning per deployment
- Iterative BFS processes nodes in breadth-first order (vs DFS) — may change rollback ordering for deep chains, but the contract only guarantees "all reachable dependents are rolled back"
