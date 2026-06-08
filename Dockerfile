# Multi-stage build producing a single ~20MB alpine image with rill + the
# SvelteKit frontend embedded into the binary via //go:embed. No nginx, no
# separate frontend container, no per-deploy build of the SPA.
#
# All base images pinned by digest so a rebuild can't pick up a different
# upstream image silently. Refresh by re-pulling and updating the digest
# in the same commit.

# Stage 1: build the SvelteKit SPA. Output lands in frontend/build/ as
# static HTML + per-route shells + content-hashed _app/immutable/ chunks.
FROM node:22-bookworm-slim@sha256:689c11043dad91472750cd824c97dd5e2318e9dd6f954e492fe7af0135d33ceb AS frontend-builder
WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# Stage 2: build the Go binary. The SvelteKit build is copied into the
# embed directory (overwriting the committed placeholder) before `go build`,
# so //go:embed all:webui captures the real bundle. -trimpath strips host
# paths from the binary so the image doesn't leak /home/<user>/... into
# stack traces or debug info.
FROM golang:1.26-bookworm@sha256:252599aeb51ad60b83e4d8821802068127c528c707cb7dd7afd93be057c6011c AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN rm -rf internal/server/webui/*
COPY --from=frontend-builder /app/frontend/build/ internal/server/webui/
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o rill ./cmd/rill

# Stage 3: minimal runtime. Alpine for curl-based healthcheck; the binary
# is static so it runs unmodified.
FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11
RUN apk add --no-cache ca-certificates curl
RUN adduser -D -h /app rill
COPY --from=builder /app/rill /usr/local/bin/rill
USER rill
WORKDIR /app
EXPOSE 8080
# Healthcheck honors RILL_PORT so the image works regardless of which port
# the operator binds.
HEALTHCHECK --interval=15s --timeout=3s --retries=3 \
  CMD curl -sf "http://localhost:${RILL_PORT:-8080}/health" || exit 1
ENTRYPOINT ["rill"]
# Default sub-command. Without this the image runs `rill` (help text) and
# exits — fine for one-shot CLI invocations like `docker run rill admin …`
# (compose `command:` overrides), but bad for a service that should serve
# by default. Compose deployments no longer need `command: ["serve"]`.
CMD ["serve"]
