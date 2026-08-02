# Changelog

All notable changes to Rill are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/), and the project aims to follow
[Semantic Versioning](https://semver.org/).

## [0.4.0] — MCP surface trim

Tool-surface cleanup after an in-anger usage review: keep tools that are the
only way to do something; cut tools that overlap a better one.

### Removed
- **`discover` / `load` meta-tools** — rill's own progressive-disclosure layer
  predated harnesses doing deferred tool loading natively; now it's a second
  layer of indirection nothing invokes. `tools/list` always returns full
  definitions (standard MCP). The `RILL_COMPACT_TOOLS` modes are gone with it —
  a compact list with no `load` would strand clients.
- **`set_version`** — pure sugar over `add_edge` with the `version_is`
  predicate, which is exclusive and already supersedes correctly. The store
  function, REST surface, and remember-inline `version` field are unchanged.
- **`edit_notes`** — hand_notes is the human-curated voice; the web UI (and
  REST/CLI) is the right pen for it, not an agent tool.

## [0.3.0] — Orient v2

Four additions to `orient`, rill's boot-context render, aimed at making it a
sharper "what's going on" briefing instead of a static dump.

### Added
- **Open loops** — memories can be flagged `open` (via `remember(open:true)`
  or `edit_memory(open:...)`), and orient now renders a "## Open loops"
  section listing every active open memory, oldest concern first, with an
  `(opened YYYY-MM-DD)` marker. Closing a loop (`open:false`) retains its
  `opened_at` for the record.
- **Per-caller delta** — orient now resolves the calling identity and renders
  a "## Since last orient (Nd ago)" section right after the header: new
  memories, entities touched, edges opened/closed, and new rules since that
  caller's last orient. First-time callers get `_first orient for this
  caller_` instead. The delta is computed fresh on every call and spliced
  into the cached render — it never lives in the orient cache blob itself.
- **Map** — global orient gains a final "## Map" section: dormant/unsurfaced
  projects, every document title (with type), entity counts by type plus the
  top 10 by mention count, and a closing pointer to `get_entity` / `doc_get`
  / `recall` / `orient(project=...)`. Deliberately generous — it's the index
  of everything reachable but not rendered above the fold.
- **Focus** — `orient(project=X)` now assembles a focused subgraph instead of
  just filtering the global render: rules, the owner's identity card, the
  scoped delta, the project entity's full (untruncated) card, all of its
  1-hop edges with neighbor summaries, project-scoped document titles, open
  loops, and recent memories — plus a pointer back to the global map.

### Changed
- Schema: `memory.open` (bool) and `memory.opened_at` (option\<datetime\>)
  fields, applied idempotently; existing rows read as closed (`NONE` == false).

## [0.1.0] — Initial public release

The first public release of Rill: a fast, MCP-native memory server backed by a
single Go binary and one SurrealDB instance.

### Memory & graph
- Store, search, recall, and edit memories over MCP, REST, and the CLI
- Entities extracted from each memory and linked into a knowledge graph
- Hybrid retrieval — vector similarity fused with full-text search
- Entity-anchored search that traverses graph relationships
- Background curation: clustering, dedup, and aging of stale memories
- Document storage with markdown/PDF export

### Interfaces
- **Web UI** (SvelteKit) — memory browser with search, filters, and card/dense
  views; inline edit and delete; an Explore graph view; a dashboard with stats
  and growth charts; a Cmd-K command palette; full keyboard navigation
- **CLI** — single authenticated binary at full parity with MCP/REST
  (`orient`, `recall`, `remember`, `entities`, `merge-entity`, `set-version`,
  `doc-put`, `doc-delete`, …), cross-compiled for macOS arm64 and Linux amd64
- **MCP server** — meta-MCP `discover`/`load` pattern; works with Claude
  Desktop, LM Studio, and other MCP clients

### Theming
- Dark-first design with a runtime OKLCH theme engine — curated presets plus
  live tuning sliders, applied site-wide

### Auth & deployment
- Bearer-token (PAT) auth for agents and the CLI; create/list/revoke in the web UI
- Human auth via built-in local login or a trusted reverse proxy (SSO)
- Docker Compose deployment; reverse-proxy + TLS supported
- CI: lint, test, SAST, dependency and image vulnerability scanning
