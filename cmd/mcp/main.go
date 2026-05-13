// Command readeck-mcp is an MCP HTTP/SSE server that exposes Readeck as a set
// of tools. The server is stateless with respect to credentials — each
// connecting client supplies its own Readeck API token via the X-API-Key
// header, and tool calls run under that token.
//
// Configuration is minimal:
//
//	READECK_BASE_URL    (required) public URL of the Readeck instance
//	MCP_HTTP_ADDR       bind address, default ":8080"
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

	baseURL, addr, err := resolve(*cfgPath)
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}

	s := mcp.New(baseURL)
	sse := server.NewSSEServer(s, server.WithSSEContextFunc(mcp.ExtractTokenFromHTTP))

	log.Info("mcp server starting", "addr", addr, "base_url", baseURL)
	if err := http.ListenAndServe(addr, sse); err != nil {
		log.Error("server", "err", err)
		os.Exit(1)
	}
}

func resolve(cfgPath string) (baseURL, addr string, err error) {
	addr = ":8080"
	if cfgPath != "" {
		cfg, lerr := bot.Load(cfgPath)
		if lerr != nil {
			return "", "", lerr
		}
		baseURL = cfg.Readeck.BaseURL
	}
	if v := os.Getenv("READECK_BASE_URL"); v != "" {
		baseURL = v
	}
	if v := os.Getenv("MCP_HTTP_ADDR"); v != "" {
		addr = v
	}
	if baseURL == "" {
		return "", "", fmt.Errorf("READECK_BASE_URL or [readeck].base_url is required")
	}
	return baseURL, addr, nil
}
