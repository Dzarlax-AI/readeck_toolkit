// Package mcp wraps the readeck client as a set of MCP tools.
//
// The server is single-tenant: it talks to one Readeck instance with one
// token. Multi-user setups should run one MCP server per user.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/dzarlax/readeck_toolkit/internal/readeck"
)

// New returns an MCPServer with three tools registered:
// readeck_save, readeck_search, readeck_list_recent.
func New(client *readeck.Client, baseURL string) *server.MCPServer {
	s := server.NewMCPServer("readeck", "0.1.0", server.WithToolCapabilities(true))

	s.AddTool(mcpgo.NewTool("readeck_save",
		mcpgo.WithDescription("Save a URL to Readeck. Optionally attach labels (tags)."),
		mcpgo.WithString("url", mcpgo.Required(), mcpgo.Description("URL to save")),
		mcpgo.WithString("title", mcpgo.Description("Override the auto-extracted title")),
		mcpgo.WithArray("labels", mcpgo.Description("Labels (tags) to attach"),
			mcpgo.Items(map[string]any{"type": "string"})),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
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
