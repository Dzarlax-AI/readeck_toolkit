# CLAUDE.md — readeck_toolkit

Telegram bot + MCP server for [Readeck](https://readeck.org/). Single Go module, two binaries, one Docker image.

## Architecture

- `cmd/bot/` — Telegram bot, multi-tenant via `config.toml` (`[[tenants]]` map `telegram_id → readeck_token`).
- `cmd/mcp/` — MCP HTTP/SSE server, single-tenant via env.
- `internal/readeck/` — shared REST client (`CreateBookmark`, `ListBookmarks`).
- `internal/bot/` — TOML loader, tenant lookup, Telegram handlers (URL extraction + hashtag → labels).
- `internal/mcp/` — tool registrations for `mark3labs/mcp-go`.

The Dockerfile is multi-stage with a distroless `nonroot` runtime, building both binaries into one image. Compose selects which to run via `command:`.

## Commands

```bash
go build ./cmd/bot ./cmd/mcp
go vet ./...
go test ./...
go run ./cmd/bot -config config.toml
docker build -t readeck-toolkit .
```

## Environment

- **Bot**: reads `config.toml`. Env overrides: `TELEGRAM_TOKEN`, `READECK_BASE_URL`.
- **MCP**: env only — `READECK_BASE_URL`, `READECK_API_TOKEN`, `MCP_HTTP_ADDR` (default `:8080`), `MCP_BEARER_TOKEN` (optional bearer auth on the HTTP endpoint).

## Deployment

VPS deploy lives in `personal_ai_stack/deploy/readeck-toolkit/`. Image is built by GitHub Actions on push and pushed to `ghcr.io/dzarlax-ai/readeck-toolkit:latest`.
