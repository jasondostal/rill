# Roadmap

## Phase 0: Project Scaffold ✅
- [x] Go module, project structure, ARCHITECTURE.md
- [x] Docker Compose with SurrealDB
- [x] Health check endpoint
- [x] CI baseline (lint, test, SAST, vuln scan, Trivy, dependabot)

## Phase 1: Core MCP Server ✅
- [x] MCP server skeleton with meta-MCP pattern (discover + load)
- [x] SurrealDB connection, schema, migrations
- [x] Memory CRUD (store, search, recall, modify)
- [x] Vector embedding integration

## Phase 2: Memory Intelligence & Graph ✅
- [x] Entity extraction — two-pass normalize→extract pipeline
- [x] Memory consolidation (dedup, merge recommendations)
- [x] Memory insights (clustering)
- [x] Dream — autonomous recurring curation
- [x] Boot (session orientation), rules, status
- [x] Document management
- [x] Statement triples with subject/predicate/object and contradiction detection
- [x] Entity-anchored search (graph traversal + RRF backfill)
- [x] Think — structured reasoning sequences
- [x] Post-store relationship extraction

## Phase 3: Frontend ✅
- [x] SvelteKit scaffold, dark theme, OKLCH color system
- [x] Memory browser — search, filters, sort, card/dense views, detail sheet
- [x] Dashboard — memory growth sparkline, stats strip, dream status
- [x] Explore — entity/memory graph visualizer with toggle
- [x] Clusters — labeled group browser with LLM cluster titles
- [x] Document manager UI
- [x] Settings page — PAT management, preferences, environment
- [x] UI/UX refinement — Cmd-K, date grouping, keyboard nav, OKLCH colors
- [x] Context dedup middleware (RILL_DEDUP=true)

## Phase 4: Auth & Production ✅
- [x] Bearer token auth on all MCP endpoints
- [x] Authentik SSO proxy integration for web UI
- [x] PAT management via UI and CLI
- [x] Distill middleware (resolved: rolled own dedup)
- [x] Performance optimization (verified: ~500ms search)
- [x] Live MCP integration test (LM Studio + local model)
- [ ] Bulk import tooling — clean import with entity dedup

## Phase 5: CLI ✅
- [x] Native CLI with all MCP tools
- [x] Bearer token auth via RILL_TOKEN
- [x] Remote server via RILL_HOST
- [x] Cross-compiled for macOS arm64 + Linux amd64

---

**Status: 27/29 tasks complete.** Two remaining: bulk import tooling (with verified entity dedup) and UI audit pass.
