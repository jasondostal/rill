# Rill

> **A tiny stream, flowing memory.**
>
> Fast, MCP-native memory server. One database for documents, graph relationships, and vector search. Built for multi-agent teams tired of context waste.

[![CI](https://github.com/jasondostal/rill/actions/workflows/ci.yml/badge.svg)](https://github.com/jasondostal/rill/actions/workflows/ci.yml)
[![Go 1.26](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

Rill gives your agents a memory that survives between sessions — one place for the things worth remembering, the entities they're about, and how everything connects. Stand it up with a single Go binary and one database, point any MCP client at it, and browse it all in a fast dark-first web UI.

## Quick Start

```bash
# Start SurrealDB
surreal start --user root --pass root surrealkv://data

# Build and run Rill
git clone https://github.com/jasondostal/rill.git
cd rill
go build -o rill ./cmd/rill
RILL_PORT=8080 ./rill serve

# Frontend (separate terminal, optional)
cd frontend && npm install && npm run dev -- --host
```

Open http://localhost:5173/ — dashboard with memory growth chart, stats, and curation status.

### Docker Compose

```yaml
services:
  surrealdb:
    image: surrealdb/surrealdb:v3.0.5
    ports: ["8000:8000"]
    command: start --user root --pass root surrealkv://data
    volumes: [surreal_data:/data]
    healthcheck:
      test: ["CMD", "/surreal", "is-ready", "--endpoint", "ws://localhost:8000"]
      interval: 5s
      retries: 5

  rill:
    image: ghcr.io/jasondostal/rill:latest
    ports: ["8080:8080"]
    environment:
      - RILL_PORT=8080
      - SURREAL_URL=ws://surrealdb:8000
      - SURREAL_USER=root
      - SURREAL_PASS=root
    depends_on:
      surrealdb:
        condition: service_healthy

volumes:
  surreal_data:
```

```bash
docker compose up -d
# First boot creates a default admin token — check logs:
docker compose logs rill | grep "default token created"
```

## The experience

**Web UI** — browse, search, and filter every memory; open any one to read, edit, or delete it inline. An Explore view renders the entity/memory graph so you can see how things connect. Cmd-K opens a global command palette with inline search, and the whole app is keyboard-first (↑↓/j/k to move, Enter to open, Esc to close). Dark-first with a live-tunable OKLCH theme system.

**CLI** — a single authenticated binary for the terminal:

```bash
go build -o rill ./cmd/rill

export RILL_HOST=https://rill.example.com   # or http://localhost:9090
export RILL_TOKEN=rill_your_token_here

rill boot
rill search "architecture decisions" --project rill
rill store "Deployed Rill to production" --project rill --type progress
rill token create "my-laptop"
```

**MCP server** — drop Rill into any MCP client (Claude Desktop, LM Studio, etc.) and your agents read and write the same memory:

```json
{
  "mcpServers": {
    "rill": {
      "url": "https://rill.example.com/mcp",
      "headers": {
        "Authorization": "Bearer rill_your_token_here"
      }
    }
  }
}
```

A default admin token is printed to server logs on first start. Manage tokens via the web UI at `/settings`. See [DEPLOY.md](docs/DEPLOY.md) for auth modes, proxy setup, and recovery procedures.

## Architecture

| Layer | Choice | Why |
|-------|--------|-----|
| Language | Go 1.26 | Single binary, fast concurrency, small footprint |
| Database | SurrealDB 3.0 | Documents + graph + vectors in one engine |
| Frontend | SvelteKit 2 | Dark theme, OKLCH colors, instant HMR |
| Auth | Bearer tokens + RILL_AUTH_MODE | PATs for agents, local login or SSO for humans |

Under the hood, Rill stores each memory alongside the entities it mentions and links them into a graph; retrieval fuses vector similarity with full-text search, and a background curation pass clusters, dedups, and ages memories over time. You don't have to think about any of it — you store and search; Rill keeps the graph coherent.

## Documentation

- [ARCHITECTURE.md](ARCHITECTURE.md) — package structure, data flows, schema
- [ROADMAP.md](ROADMAP.md) — feature milestones
- [CHANGELOG.md](CHANGELOG.md) — release history

## License

MIT — see [LICENSE](LICENSE).
