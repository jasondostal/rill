# Deploying Rill

## Quick Start (docker-compose)

```bash
docker compose up -d
# First boot creates no users — visit /setup to create admin
# An initial admin token is also written to the data volume
```

Open `http://<host>:8080/setup`, create username + password. Then log in at `/login`.

## Auth Modes

Set via `RILL_AUTH_MODE`:

### `local` (default)

Built-in login with sessions. Users create account at `/setup` (one-shot). MCP agents use bearer tokens.

### `sso`

Trusted reverse proxy passes identity via a header. The proxy handles auth; Rill trusts the header when the request comes from a trusted IP.

```env
RILL_AUTH_MODE=sso
RILL_TRUSTED_PROXY_IPS=10.0.0.0/8,172.16.0.0/12
RILL_AUTH_PROXY_HEADER=X-Forwarded-User
```

Supported proxies: oauth2-proxy, Authentik, Caddy `forward_auth`, Traefik `forwardAuth`, Cloudflare Access.

## Migrating from `RILL_NO_AUTH=1`

`RILL_NO_AUTH` is removed. Set `RILL_AUTH_MODE=local`, restart, create admin via the setup screen.

## First-Run Setup

1. Visit `http://<host>:8080/setup`
2. Create username + password (8+ chars)
3. Log in at `/login`
4. Setup is one-shot — once a user exists, the endpoint is locked

## Initial Admin Token

On first boot, Rill writes an initial admin token to `${RILL_DATA_DIR}/initial-admin-token` (mode 0600).

**Mount `RILL_DATA_DIR` as a Docker volume or the file is lost on container rebuild:**

```yaml
rill:
  environment:
    - RILL_DATA_DIR=/var/lib/rill
  volumes:
    - rill_data:/var/lib/rill
```

Read the token, save it to your password manager, then delete the file.

## Token Management

```bash
rill admin token create "my-laptop"            # permanent
rill admin token create "ci-runner" --ttl 90d  # expires in 90 days
rill admin token create "readonly" --scopes read
rill admin token list
rill admin token revoke <id>
rill admin token rotate <id>
```

## Rate Limits

| Endpoint | Rate | Burst |
|----------|------|-------|
| `/mcp` | 10 req/s | 30 |
| Login, Setup | 1 req/s | 5 |

Tunable in `internal/server/ratelimit.go`.

## Audit Log

Query via SurrealDB CLI:

```bash
surreal sql --conn ws://localhost:8000 --user root --pass root --ns rill --db rill \
  -e "SELECT * FROM auth_audit ORDER BY at DESC LIMIT 50;"
```

## Recovery

If you lose the admin token and can't reach the UI:

```bash
surreal sql --conn ws://localhost:8000 --user root --pass root --ns rill --db rill
> SELECT id, name FROM auth_token;
> INSERT INTO auth_token { name: "recovery", token_hash: "...", scopes: ["read", "write"] };
```

Generate token hash: `rill admin token create` writes the raw token to stdout before hashing.
