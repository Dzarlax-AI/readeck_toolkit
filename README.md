# readeck_toolkit

A Telegram bot and an MCP server for [Readeck](https://readeck.org/), in one Go module.

- **Telegram bot** — multi-tenant. Forward a URL to the bot, it saves to Readeck under the right user. Append `#tags` to set labels.
- **MCP server** — credential-less. Exposes Readeck as tools (`readeck_save`, `readeck_search`, `readeck_list_recent`) to any MCP client (Claude Desktop, Claude Code, Cursor, etc.). Each client passes its own Readeck token over the wire — the server stores no secrets.

Both share one Go module and one Docker image. The included `docker-compose.yml` defines both services — start whichever you need:

```bash
docker compose up -d              # both
docker compose up -d mcp          # only MCP server
docker compose up -d bot          # only Telegram bot
docker compose stop bot           # turn one off later
```

Each service ignores config sections it doesn't use, so you can have an MCP-only deploy without filling in `[telegram]` / `[[tenants]]`, and vice versa.

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

The MCP server only needs to know the base URL of your Readeck instance. Tokens are not stored on the server — they're passed by each MCP client when connecting.

If you're already running the bot, MCP reads `[readeck].base_url` from the same `config.toml`. Otherwise set `READECK_BASE_URL` env.

```bash
docker compose up -d mcp
```

Then in your MCP client config (Claude Code / Claude Desktop / Cursor / etc.):

```json
{
  "mcpServers": {
    "readeck": {
      "transport": "sse",
      "url": "https://your-mcp-host/sse",
      "headers": {
        "X-API-Key": "rk_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
      }
    }
  }
}
```

The token is generated in Readeck → **Settings → API tokens**. Each user of the MCP server uses their own token; the server hands the call straight to Readeck which scopes it to that user.

`Authorization: Bearer <token>` is also accepted as a fallback for MCP clients that only know how to send that header.

## Configuration

- **Bot**: `config.toml`. Env overrides: `TELEGRAM_TOKEN`, `READECK_BASE_URL`.
- **MCP**: just needs `READECK_BASE_URL` (env or `[readeck].base_url` in TOML). Optional: `MCP_HTTP_ADDR` to change the bind address from `:8080`.

## Build from source

```bash
go build ./cmd/bot ./cmd/mcp
```

## License

MIT.
