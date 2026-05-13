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

// stringSliceArg extracts an []string from a tool argument that arrived as
// MCP's generic []any. Non-string entries are dropped silently.
func stringSliceArg(args map[string]any, key string) []string {
	raw, ok := args[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, x := range raw {
		if s, ok := x.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// New returns an MCPServer registered with all Readeck tools. baseURL is
// the public URL of the Readeck instance, shared by all clients; per-client
// authentication lives in the request context.
func New(baseURL string) *server.MCPServer {
	s := server.NewMCPServer("readeck", "0.3.0", server.WithToolCapabilities(true))

	withClient := func(ctx context.Context) (*readeck.Client, *mcpgo.CallToolResult) {
		token := tokenFromContext(ctx)
		if token == "" {
			return nil, mcpgo.NewToolResultError("missing Readeck token — pass it as the X-API-Key header on the MCP connection")
		}
		return readeck.NewClient(baseURL, token), nil
	}

	// ---------- save ----------
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
		bm, err := client.CreateBookmark(ctx, readeck.CreateInput{
			URL:    req.GetString("url", ""),
			Title:  req.GetString("title", ""),
			Labels: stringSliceArg(req.GetArguments(), "labels"),
		})
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

	// ---------- search ----------
	s.AddTool(mcpgo.NewTool("readeck_search",
		mcpgo.WithDescription("Full-text search across saved bookmarks."),
		mcpgo.WithString("query", mcpgo.Required(), mcpgo.Description("Search query")),
		mcpgo.WithNumber("limit", mcpgo.Description("Max results (default 20)")),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		client, errResult := withClient(ctx)
		if errResult != nil {
			return errResult, nil
		}
		items, err := client.ListBookmarks(ctx, readeck.ListOpts{
			Search: req.GetString("query", ""),
			Limit:  req.GetInt("limit", 20),
		})
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return mcpgo.NewToolResultText(formatList(baseURL, items)), nil
	})

	// ---------- list recent ----------
	s.AddTool(mcpgo.NewTool("readeck_list_recent",
		mcpgo.WithDescription("List recent bookmarks. Defaults to unread only."),
		mcpgo.WithBoolean("unread_only", mcpgo.Description("If true (default), only unread bookmarks")),
		mcpgo.WithNumber("limit", mcpgo.Description("Max results (default 20)")),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		client, errResult := withClient(ctx)
		if errResult != nil {
			return errResult, nil
		}
		items, err := client.ListBookmarks(ctx, readeck.ListOpts{
			Unread: req.GetBool("unread_only", true),
			Limit:  req.GetInt("limit", 20),
		})
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return mcpgo.NewToolResultText(formatList(baseURL, items)), nil
	})

	// ---------- get article ----------
	s.AddTool(mcpgo.NewTool("readeck_get_article",
		mcpgo.WithDescription("Fetch the extracted article body of a bookmark, converted to Markdown. Useful for read-in-chat and summarisation."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Bookmark id")),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		client, errResult := withClient(ctx)
		if errResult != nil {
			return errResult, nil
		}
		text, err := client.GetArticleMarkdown(ctx, req.GetString("id", ""))
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return mcpgo.NewToolResultText(text), nil
	})

	// ---------- mark read ----------
	s.AddTool(mcpgo.NewTool("readeck_mark_read",
		mcpgo.WithDescription("Mark a bookmark as read (archived) or unread. In Readeck, 'read' and 'archived' are the same state."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Bookmark id")),
		mcpgo.WithBoolean("read", mcpgo.Description("True (default) marks as read; false moves it back to unread")),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		client, errResult := withClient(ctx)
		if errResult != nil {
			return errResult, nil
		}
		read := req.GetBool("read", true)
		if err := client.UpdateBookmark(ctx, req.GetString("id", ""), readeck.UpdateInput{IsArchived: readeck.BoolPtr(read)}); err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return mcpgo.NewToolResultText(fmt.Sprintf("ok (is_archived=%v)", read)), nil
	})

	// ---------- add labels ----------
	s.AddTool(mcpgo.NewTool("readeck_add_labels",
		mcpgo.WithDescription("Add labels (tags) to an existing bookmark. Existing labels are preserved."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Bookmark id")),
		mcpgo.WithArray("labels", mcpgo.Required(), mcpgo.Description("Labels to add"),
			mcpgo.Items(map[string]any{"type": "string"})),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		client, errResult := withClient(ctx)
		if errResult != nil {
			return errResult, nil
		}
		labels := stringSliceArg(req.GetArguments(), "labels")
		if len(labels) == 0 {
			return mcpgo.NewToolResultError("labels: at least one is required"), nil
		}
		if err := client.AddLabels(ctx, req.GetString("id", ""), labels); err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return mcpgo.NewToolResultText("ok"), nil
	})

	// ---------- remove labels ----------
	s.AddTool(mcpgo.NewTool("readeck_remove_labels",
		mcpgo.WithDescription("Remove labels (tags) from an existing bookmark. Other labels are preserved."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Bookmark id")),
		mcpgo.WithArray("labels", mcpgo.Required(), mcpgo.Description("Labels to remove"),
			mcpgo.Items(map[string]any{"type": "string"})),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		client, errResult := withClient(ctx)
		if errResult != nil {
			return errResult, nil
		}
		labels := stringSliceArg(req.GetArguments(), "labels")
		if len(labels) == 0 {
			return mcpgo.NewToolResultError("labels: at least one is required"), nil
		}
		if err := client.RemoveLabels(ctx, req.GetString("id", ""), labels); err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return mcpgo.NewToolResultText("ok"), nil
	})

	// ---------- delete ----------
	s.AddTool(mcpgo.NewTool("readeck_delete",
		mcpgo.WithDescription("Delete a bookmark permanently."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Bookmark id")),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		client, errResult := withClient(ctx)
		if errResult != nil {
			return errResult, nil
		}
		if err := client.DeleteBookmark(ctx, req.GetString("id", "")); err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		return mcpgo.NewToolResultText("ok"), nil
	})

	// ---------- list labels ----------
	s.AddTool(mcpgo.NewTool("readeck_list_labels",
		mcpgo.WithDescription("List every label in the user's library, with how many bookmarks each has."),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		client, errResult := withClient(ctx)
		if errResult != nil {
			return errResult, nil
		}
		labels, err := client.ListLabels(ctx)
		if err != nil {
			return mcpgo.NewToolResultError(err.Error()), nil
		}
		if len(labels) == 0 {
			return mcpgo.NewToolResultText("(no labels)"), nil
		}
		var sb strings.Builder
		for _, l := range labels {
			fmt.Fprintf(&sb, "- %s (%d)\n", l.Name, l.Count)
		}
		return mcpgo.NewToolResultText(sb.String()), nil
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
		fmt.Fprintf(&sb, "- [id=%s] %s\n  %s\n  %s\n", b.ID, title, b.URL, readeck.PermalinkOf(baseURL, b.ID))
		if len(b.Labels) > 0 {
			fmt.Fprintf(&sb, "  labels: %s\n", strings.Join(b.Labels, ", "))
		}
	}
	return sb.String()
}
