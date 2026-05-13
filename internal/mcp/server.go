// Package mcp wraps the readeck client as a set of MCP tools.
//
// The server is stateless with respect to credentials: each connecting
// MCP client supplies its own Readeck API token via the HTTP X-API-Key
// header. Tool handlers construct a fresh readeck.Client per request
// using that token, so one MCP server can serve any number of users —
// including users it has never seen — as long as they hold a valid
// token for the Readeck instance at baseURL.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/dzarlax/readeck_toolkit/internal/readeck"
)

// APIKeyHeader is the canonical header an MCP client uses to pass its
// Readeck API token. The plain `Authorization: Bearer …` form is also
// accepted as a fallback for clients that hard-wire it.
const APIKeyHeader = "X-API-Key"

// tokenKey is the unexported context key holding the per-request Readeck
// API token. Use ExtractTokenFromHTTP to populate it from incoming requests.
type tokenKey struct{}

// ExtractTokenFromHTTP is intended for server.WithSSEContextFunc /
// server.WithHTTPContextFunc. It reads X-API-Key (preferred) or, as a
// compatibility fallback, `Authorization: Bearer …`, and stashes the bare
// token in the context that tool handlers receive.
func ExtractTokenFromHTTP(ctx context.Context, r *http.Request) context.Context {
	token := strings.TrimSpace(r.Header.Get(APIKeyHeader))
	if token == "" {
		if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			token = strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		}
	}
	if token == "" {
		return ctx
	}
	return context.WithValue(ctx, tokenKey{}, token)
}

func tokenFromContext(ctx context.Context) string {
	s, _ := ctx.Value(tokenKey{}).(string)
	return s
}

// New returns an MCPServer with three Readeck tools. baseURL is the public
// URL of the Readeck instance and is the same for every client; per-client
// authentication lives in the request context.
func New(baseURL string) *server.MCPServer {
	s := server.NewMCPServer("readeck", "0.2.0", server.WithToolCapabilities(true))

	withClient := func(ctx context.Context) (*readeck.Client, *mcpgo.CallToolResult) {
		token := tokenFromContext(ctx)
		if token == "" {
			return nil, mcpgo.NewToolResultError("missing Readeck token — pass it as the X-API-Key header on the MCP connection")
		}
		return readeck.NewClient(baseURL, token), nil
	}

	s.AddTool(mcpgo.NewTool("readeck_save",
		mcpgo.WithDescription("Save a URL to Readeck. Optionally attach labels (tags)."),
		mcpgo.WithString("url", mcpgo.Required(), mcpgo.Description("URL to save")),
		mcpgo.WithString("title", mcpgo.Description("Override the auto-extracted title")),
		mcpgo.WithArray("labels", mcpgo.Description("Labels (tags) to attach"),
			mcpgo.Items(map[string]any{"type": "string"})),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		client, errResult := withClient(ctx)
		if errResult != nil {
			return errResult, nil
		}
		u := req.GetString("url", "")
		title := req.GetString("title", "")
		var labels []string
		if raw, ok := req.GetArguments()["labels"].([]any); ok {
			for _, x := range raw {
				if str, ok := x.(string); ok {
					labels = append(labels, str)
				}
			}
		}
		bm, err := client.CreateBookmark(ctx, readeck.CreateInput{URL: u, Title: title, Labels: labels})
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		out := map[string]any{
			"id":        bm.ID,
			"permalink": readeck.PermalinkOf(baseURL, bm.ID),
			"title":     bm.Title,
		}
		b, _ := json.Marshal(out)
		return mcpgo.NewToolResultText(string(b)), nil
	})

	s.AddTool(mcpgo.NewTool("readeck_search",
		mcpgo.WithDescription("Full-text search across saved bookmarks."),
		mcpgo.WithString("query", mcpgo.Required(), mcpgo.Description("Search query")),
		mcpgo.WithNumber("limit", mcpgo.Description("Max results (default 20)")),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		client, errResult := withClient(ctx)
		if errResult != nil {
			return errResult, nil
		}
		q := req.GetString("query", "")
		limit := req.GetInt("limit", 20)
		items, err := client.ListBookmarks(ctx, readeck.ListOpts{Search: q, Limit: limit})
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return mcpgo.NewToolResultText(formatList(baseURL, items)), nil
	})

	s.AddTool(mcpgo.NewTool("readeck_list_recent",
		mcpgo.WithDescription("List recent bookmarks. Defaults to unread only."),
		mcpgo.WithBoolean("unread_only", mcpgo.Description("If true (default), only unread bookmarks")),
		mcpgo.WithNumber("limit", mcpgo.Description("Max results (default 20)")),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		client, errResult := withClient(ctx)
		if errResult != nil {
			return errResult, nil
		}
		unread := req.GetBool("unread_only", true)
		limit := req.GetInt("limit", 20)
		items, err := client.ListBookmarks(ctx, readeck.ListOpts{Unread: unread, Limit: limit})
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return mcpgo.NewToolResultText(formatList(baseURL, items)), nil
	})

	return s
}

func formatList(baseURL string, items []readeck.Bookmark) string {
	if len(items) == 0 {
		return "(no bookmarks)"
	}
	var sb strings.Builder
	for _, b := range items {
		title := b.Title
		if title == "" {
			title = "(untitled)"
		}
		fmt.Fprintf(&sb, "- %s\n  %s\n  %s\n", title, b.URL, readeck.PermalinkOf(baseURL, b.ID))
		if len(b.Labels) > 0 {
			fmt.Fprintf(&sb, "  labels: %s\n", strings.Join(b.Labels, ", "))
		}
	}
	return sb.String()
}
