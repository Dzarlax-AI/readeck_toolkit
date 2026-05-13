// Command readeck-mcp is an MCP HTTP/SSE server that exposes Readeck as a set
// of tools to any MCP-compatible client. Single-tenant by design.
package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/mark3labs/mcp-go/server"

	"github.com/dzarlax/readeck_toolkit/internal/mcp"
	"github.com/dzarlax/readeck_toolkit/internal/readeck"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	baseURL := os.Getenv("READECK_BASE_URL")
	token := os.Getenv("READECK_API_TOKEN")
	if baseURL == "" || token == "" {
		log.Error("READECK_BASE_URL and READECK_API_TOKEN are required")
		os.Exit(1)
	}
	addr := os.Getenv("MCP_HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	bearer := os.Getenv("MCP_BEARER_TOKEN") // optional auth on the HTTP endpoint

	client := readeck.NewClient(baseURL, token)
	s := mcp.New(client, baseURL)

	sse := server.NewSSEServer(s)
	var handler http.Handler = sse
	if bearer != "" {
		handler = withBearer(handler, bearer)
	}

	log.Info("mcp server starting", "addr", addr, "bearer_auth", bearer != "")
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Error("server", "err", err)
		os.Exit(1)
	}
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
