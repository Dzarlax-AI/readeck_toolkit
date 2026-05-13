// Command readeck-mcp is a Streamable HTTP MCP server that exposes Readeck
// as a set of tools. Each connecting client supplies its own Readeck API
// token via the X-API-Key header, and tool calls run under that token.
//
// Configuration is minimal:
//
//	READECK_BASE_URL    (required) public URL of the Readeck instance
//	MCP_HTTP_ADDR       bind address, default ":8080"
//	MCP_ENDPOINT        URL path the MCP endpoint is exposed under.
//	                    Defaults to "/mcp". For path-prefixed reverse
//	                    proxies (traefik PathPrefix(`/readeck`)),
//	                    use "/readeck/mcp" — both sides must agree.
//
// Or pass -config /path/to/config.toml and the base URL is read from
// [readeck].base_url. Tokens never live on the server.
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
)

func main() {
	cfgPath := flag.String("config", "", "optional config.toml — only [readeck].base_url is read")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	baseURL, addr, endpoint, err := resolve(*cfgPath)
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}

	s := mcp.New(baseURL)
	streamable := server.NewStreamableHTTPServer(s,
		server.WithEndpointPath(endpoint),
		server.WithHTTPContextFunc(mcp.ExtractTokenFromHTTP),
		// Stateless: every POST stands on its own, no server-side sessions.
		// Auth is per-request via X-API-Key, so we don't need session state.
		server.WithStateLess(true),
	)

	log.Info("mcp server starting", "addr", addr, "base_url", baseURL, "endpoint", endpoint)
	if err := http.ListenAndServe(addr, streamable); err != nil {
		log.Error("server", "err", err)
		os.Exit(1)
	}
}

func resolve(cfgPath string) (baseURL, addr, endpoint string, err error) {
	addr = ":8080"
	endpoint = "/mcp"
	if cfgPath != "" {
		cfg, lerr := bot.Load(cfgPath)
		if lerr != nil {
			return "", "", "", lerr
		}
		baseURL = cfg.Readeck.BaseURL
	}
	if v := os.Getenv("READECK_BASE_URL"); v != "" {
		baseURL = v
	}
	if v := os.Getenv("MCP_HTTP_ADDR"); v != "" {
		addr = v
	}
	if v := os.Getenv("MCP_ENDPOINT"); v != "" {
		endpoint = v
	}
	if baseURL == "" {
		return "", "", "", fmt.Errorf("READECK_BASE_URL or [readeck].base_url is required")
	}
	return baseURL, addr, endpoint, nil
}
