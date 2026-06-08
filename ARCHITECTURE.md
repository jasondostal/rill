# Rill — Architecture

## Overview

Rill is an MCP-native memory server. A single Go binary serves MCP tools over HTTP. SurrealDB provides document storage, graph relationships, and vector search in one database. A SvelteKit frontend provides a web console.

```
┌─────────────┐     MCP (JSON-RPC/HTTP)    ┌──────────────┐
│  AI Agents   │ ◄───────────────────────► │  Rill (Go)   │
└─────────────┘                            └──────┬───────┘
                                                  │
                                        SurrealQL │ WebSocket
                                                  │
┌─────────────┐                            ┌──────▼───────┐
│  SvelteKit   │ ◄──── /api/mcp proxy ──── │  SurrealDB   │
│  Frontend    │                            │  docs+graph  │
└─────────────┘                            │  +vectors    │
                                           └──────────────┘
```

## Package Structure

```
cmd/rill/              Entry point. Config, server startup, tool registration.
internal/
  server/              HTTP server, health check, MCP endpoint routing.
  mcp/                 MCP protocol handler, tool registry, discover/load pattern.
    tools/             Tool implementations: memory, documents, think, boot, entities, dream.
  db/                  SurrealDB client, connection, schema/migrations.
  memory/              Memory CRUD (store.go), search (keyword+vector+entity-anchored),
                       types, think engine, relationship extraction, consolidation, insights.
  extraction/          Knowledge extraction pipeline.
    prompts.go         Normalization + extraction prompts.
    extractor.go       Two-pass pipeline: normalize → extract → retry.
    resolver.go        Entity matching (two-tier), statement creation, contradiction detection.
    types.go           Entity, statement, extraction result types with validation.
  embedding/           Embedding API client (OpenRouter).
  llm/                 LLM client for extraction (OpenRouter).
  document/            Document CRUD, markdown/PDF export.
  dream/               Autonomous background curation (cluster, dedup, flag stale).
frontend/              SvelteKit application.
  src/lib/api.js       Shared MCP API client.
  src/lib/theme.css    OKLCH dark theme.
  src/routes/          Page routes (dashboard, memories, documents).
```

## Data Flow

### Memory Store (with extraction)
```
Store → SurrealDB INSERT
      → async goroutine:
          normalize(content) → conversational → factual
          extract(normalized) → entities + statements + tags + importance + summary
          resolve(entities) → two-tier matching (0.85 type-scoped, 0.95 agnostic)
          create statements → subject_of/object_of edges, contradiction check
          extract relations → cosine similarity on neighbors, create relates_to edges
          update memory → auto_tags, summary, entities, importance, extraction_status
```

### Entity-Anchored Search
```
search(query)
  → extractQueryEntities: embed capitalized words/phrases → match entity table (cosine > 0.7)
  → findMemoriesByEntities: graph traverse entity→appears_in edges
  → RRF backfill: keyword + vector hybrid search
  → blend results via RRF fusion (k=60)
```

### Think Sequences
```
think(action: "start") → create think_sequence record
think(action: "add")   → create thought record → RELATE sequence→thoughts→thought
think(action: "conclude") → update sequence status, store conclusion
think(action: "get")   → read sequence + traverse →thoughts edges
```

## Database Schema

### Core tables
- `memory` — content, project, memory_type, importance, tags, auto_tags, summary, embedding, entities, extraction_status, author
- `entity` — name, entity_type, name_embedding, attributes, project
- `statement` — fact, aspect, subject, predicate, fact_embedding, memory_id, project, active
- `document` — title, content, doc_type, project, source
- `think_sequence` — project, goal, status, conclusion
- `thought` — content, thought_type, author, branch_name, seq_number

### Graph edges
- `appears_in` — entity → memory
- `authored_by` — memory → entity (Agent)
- `relates_to` — memory → memory (similarity)
- `subject_of` — entity → statement
- `object_of` — statement → entity
- `thoughts` — think_sequence → thought

## Schema Migrations

Current approach: **idempotent DDL with version tracking.** Schema creation runs on every boot using `DEFINE TABLE IF NOT EXISTS` / `DEFINE FIELD IF NOT EXISTS`. This works for solo dev — boot is fast (<200ms), the DDL is read-only if no new tables exist.

A `_migrations` table records the applied schema version (`SchemaVersion = "2026-05-09-v1"`). When non-idempotent changes are needed (e.g., adding a NOT NULL column with a backfill), migration functions can check this table and run only unapplied versions.

The empty `internal/db/migrations/` directory is reserved for ordered migration functions if and when they become necessary. At Rill's current scale (single user, thousands of memories, low write QPS), the idempotent approach is sufficient. If multi-user or frequent schema changes arrive, move to ordered migration functions pinned to `_migrations.version`.

## Security

- Bearer token auth on all MCP endpoints (Phase 4)
- Reverse-proxy SSO via nginx / Caddy / Traefik (Phase 4)
- Docker non-root user, stripped binary, healthcheck
- Panic-recovery middleware on MCP handler (a panicking tool returns 500, doesn't crash the process)
