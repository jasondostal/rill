# Changelog

All notable changes to Rill are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/), and the project aims to follow
[Semantic Versioning](https://semver.org/).

## [1.0.0] — Initial public release

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
