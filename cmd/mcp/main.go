// Command readeck-mcp is an MCP HTTP/SSE server that exposes Readeck as a set
// of tools to any MCP-compatible client. Single-tenant by design.
//
// Configuration:
//   - Preferred: -config /path/to/config.toml (same file the bot reads).
//     The [mcp] section names which [[tenants]] entry to use.
//   - Fallback: env-only mode (READECK_BASE_URL + READECK_API_TOKEN).
//     Convenient when you're running just the MCP without the bot.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/mark3labs/mcp-go/server"

	"github.com/dzarlax/readeck_toolkit/internal/bot"
	"github.com/dzarlax/readeck_toolkit/internal/mcp"
	"github.com/dzarlax/readeck_toolkit/internal/readeck"
)

func main() {
	cfgPath := flag.String("config", "", "optional path to shared config.toml (bot+mcp). When empty, fall back to env vars.")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	baseURL, token, addr, bearer, err := resolve(*cfgPath)
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}

	client := readeck.NewClient(baseURL, token)
	s := mcp.New(client, baseURL)

	sse := server.NewSSEServer(s)
	var handler http.Handler = sse
	if bearer != "" {
		handler = withBearer(handler, bearer)
	}

	log.Info("mcp server starting", "addr", addr, "bearer_auth", bearer != "", "base_url", baseURL)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Error("server", "err", err)
		os.Exit(1)
	}
}

// resolve unifies config-file and env-only modes. Env vars always win where
// set, so a docker-compose deploy that reads from TOML can still patch
// individual fields with env overrides.
func resolve(cfgPath string) (baseURL, token, addr, bearer string, err error) {
	addr = ":8080"

	if cfgPath != "" {
		cfg, lerr := bot.Load(cfgPath)
		if lerr != nil {
			return "", "", "", "", lerr
		}
		baseURL = cfg.Readeck.BaseURL
		if cfg.MCP.Listen != "" {
			addr = cfg.MCP.Listen
		}
		bearer = cfg.MCP.BearerToken
		if cfg.MCP.Tenant == "" {
			return "", "", "", "", fmt.Errorf("mcp.tenant is required when using -config (names a [[tenants]].note)")
		}
		t, ok := cfg.TenantByNote(cfg.MCP.Tenant)
		if !ok {
			return "", "", "", "", fmt.Errorf("[[tenants]] with note=%q not found", cfg.MCP.Tenant)
		}
		token = t.ReadeckToken
	}

	// env overrides / fallback for env-only mode
	if v := os.Getenv("READECK_BASE_URL"); v != "" {
		baseURL = v
	}
	if v := os.Getenv("READECK_API_TOKEN"); v != "" {
		token = v
	}
	if v := os.Getenv("MCP_HTTP_ADDR"); v != "" {
		addr = v
	}
	if v := os.Getenv("MCP_BEARER_TOKEN"); v != "" {
		bearer = v
	}

	if baseURL == "" || token == "" {
		return "", "", "", "", fmt.Errorf("READECK_BASE_URL and READECK_API_TOKEN are required (or pass -config)")
	}
	return baseURL, token, addr, bearer, nil
}

// withBearer rejects requests that don't present the configured bearer token.
// When MCP_BEARER_TOKEN is empty, this wrapper isn't installed and the server
// is open — intended for stdio-style local-only use behind a reverse proxy
// that authenticates externally.
func withBearer(next http.Handler, expected string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+expected {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
