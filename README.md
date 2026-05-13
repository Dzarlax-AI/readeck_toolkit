# readeck_toolkit

A Telegram bot and an MCP server for [Readeck](https://readeck.org/), in one Go module.

- **Telegram bot** — multi-tenant. Forward a URL to the bot, it saves to Readeck under the right user. Append `#tags` to set labels.
- **MCP server** — single-tenant. Exposes Readeck as tools (`readeck_save`, `readeck_search`, `readeck_list_recent`) to any MCP client (Claude Desktop, Claude Code, Cursor, etc.).

Both share one Go module and one Docker image — pick which binary to run via the container `command:`.

## Why multi-tenant on the bot?

Readeck's REST API authenticates each call with a per-user API token. The bot keeps a map of `telegram_id → readeck_token` in `config.toml` and uses the right token per incoming message. Unknown Telegram senders are silently ignored. This lets one bot instance serve a household, a team, or a friend group without anyone seeing anyone else's data.

The MCP server doesn't multi-tenant because an MCP client is, by definition, one user. Run one MCP instance per user if you need to.

## Quick start

### Bot

1. Create a bot with [@BotFather](https://t.me/BotFather), grab the token.
2. `cp config.example.toml config.toml`, paste the bot token, set `base_url`.
3. Add yourself to `[[tenants]]`:
   - in Readeck → **Settings → API tokens** → create a token, paste as `readeck_token`
   - run the bot once and DM it `/whoami` to get your `telegram_id`
4. `docker compose up -d bot`

#### Adding more users later

Repeat for each new user:

1. The user logs into Readeck and creates their own API token (Settings → API tokens).
2. They start the bot and send `/whoami` — the bot replies with their numeric Telegram id (works for non-tenants too — that's the onboarding hook).
3. Admin appends a new `[[tenants]]` block to `config.toml` with the two values, then `docker compose restart bot`.

Unknown senders are silently ignored, so the bot is safe to leave running while users onboard themselves.

### MCP

If you're already running the bot, the MCP reads the **same** `config.toml`. Add an `[mcp]` block:

```toml
[mcp]
tenant = "alice"   # matches one of the [[tenants]].note values
```

Then `docker compose up -d mcp` and point an MCP client at `http://localhost:8080/sse`.

If you'd rather skip the TOML entirely (e.g. running just the MCP, no Telegram bot), copy `.env.example` to `.env` and use the env-only fallback — same image, just drop the `-config` flag in compose.

## Configuration

- **Bot**: `config.toml`. Env overrides: `TELEGRAM_TOKEN`, `READECK_BASE_URL`.
- **MCP**: either `-config config.toml` (uses `[mcp]` + `[[tenants]]`) **or** env vars (`READECK_BASE_URL`, `READECK_API_TOKEN`, `MCP_HTTP_ADDR`, `MCP_BEARER_TOKEN`). Env always wins where set, so a TOML deploy can still patch individual fields from the environment.

If `mcp.bearer_token` / `MCP_BEARER_TOKEN` is set, the SSE endpoint requires `Authorization: Bearer <token>`. Leave empty when fronted by an authenticating reverse proxy.

## Build from source

```bash
go build ./cmd/bot ./cmd/mcp
```

## License

MIT.
