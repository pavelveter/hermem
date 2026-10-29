# `hermem view` — local knowledge graph viewer

## Intent

Give an operator a way to *see* the memory graph, not just query it.
`hermem graph components` / `communities` answer structural questions in
text; nothing shows the shape of a DB at a glance. The reference is the
codebase-memory-mcp "graph" tab: a dark canvas of nodes and typed edges,
filterable by node type and relation, with a detail view per node.

## What must be true

1. `hermem view` starts a local HTTP server on `127.0.0.1` (default port
   `8421`, `--port/-p` overrides it), prints the URL, and blocks until
   Ctrl-C. Exit code 0 on Ctrl-C.
2. The browser opens automatically unless `--no-open` is passed; a
   failure to launch a browser is a warning, never a command failure.
3. The page renders the entities of the active `hermem.ini` database as
   an interactive node/link canvas: node colour = category, node radius
   grows with degree, edges drawn as lines with the relation type
   available on hover.
4. Interaction: drag a node to pin it, drag the background to pan, wheel
   to zoom, click a node for its content/degree/relations, Escape or a
   close control dismisses the panel. Category and relation filters
   toggle visibility. A Refresh control re-reads the database.
5. The snapshot is bounded: at most `NodeCap` (500) entities, chosen by
   highest degree, and only edges with both endpoints inside that set.
   The page states how many nodes it is showing out of the total.
6. Empty databases render an empty canvas plus "0 nodes" — no error and
   no crash.
7. `GET /api/graph` returns the snapshot as JSON with `nodes`, `edges`,
   `total_nodes`, `total_edges`, `truncated`; slices are `[]`, never
   `null`. Cache headers forbid caching so Refresh observes new writes.
8. Nothing leaves the machine: the server binds loopback only, has no
   external asset references (page is one embedded file), and needs no
   JS build step or new Go dependency.

## Non-goals

- Not a replacement for `hermem serve`; no auth, no metrics, no writes.
- No 3D/galaxy rendering, no embeddings, no community clustering overlay.
- No editing of entities or edges from the page.
- No new route on the main `hermem serve` mux — the viewer is its own
  short-lived process.

## Approaches considered

1. **Extend `hermem serve` with a `/graph` page** — reuses the running
   server, but requires the full DI graph (embedder ping, vector index)
   just to look at the graph, and mixes a debug UI into the shipped API.
   Rejected.
2. **Emit a standalone HTML file with the data inlined** (`--out`) — no
   server at all, but no refresh, no way to read a DB bigger than the
   inline budget, and the file leaks memory content to disk. Rejected.
3. **Small dedicated loopback server + embedded single-file page**
   (chosen) — one cobra command, one embedded asset, one read-only query,
   live refresh, no build step, no new dependency.

## Decisions taken

- Read path is four read-only SQL statements (two counts, one node page,
  one edge scan filtered to the page) in a new `src/internal/view`
  package; the CLI command only owns flags, listener, browser launch and
  shutdown.
- Node cap is a package constant, not a flag or config key: the ceiling
  exists because the page runs an O(n²) force simulation, and the
  upgrade path is documented next to the constant.
- Layout, filtering and detail panel are vanilla canvas + DOM; no
  framework, matching the repo's zero-frontend-dependency stance.