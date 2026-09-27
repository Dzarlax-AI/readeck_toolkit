// Package readeck is a thin client for the Readeck REST API.
//
// It is intentionally small: one client struct, one token, the handful of
// endpoints used by the bot and the MCP server. No retry, no caching.
package readeck

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	md "github.com/JohannesKaufmann/html-to-markdown"
)

const userAgent = "readeck_toolkit/0.1"

// Client talks to a single Readeck instance as a single user (one token).
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewClient returns a Client bound to baseURL ("https://read.example.com") and
// an API token generated in Readeck's Settings → API tokens.
func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Bookmark is a subset of the Readeck bookmark payload — enough for the bot
// reply and MCP listing output.
type Bookmark struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	URL         string   `json:"url"`
	SiteName    string   `json:"site_name,omitempty"`
	Description string   `json:"description,omitempty"`
	Created     string   `json:"created,omitempty"`
	IsArchived  bool     `json:"is_archived"`
	IsMarked    bool     `json:"is_marked"`
	Labels      []string `json:"labels"`
	Href        string   `json:"href,omitempty"`
}

// CreateInput is the request body for POST /api/bookmarks.
type CreateInput struct {
	URL    string   `json:"url"`
	Title  string   `json:"title,omitempty"`
	Labels []string `json:"labels,omitempty"`
}

// ListOpts is a thin filter set for GET /api/bookmarks.
type ListOpts struct {
	Search   string
	Limit    int
	Offset   int
	Unread   bool // narrow to is_archived=false
	Archived bool // narrow to is_archived=true
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	resp, err := c.request(ctx, method, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode != http.StatusNoContent {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (c *Client) request(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("readeck %s %s: %d %s", method, path, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return resp, nil
}

// CreateBookmark posts a new bookmark. Readeck does content extraction
// asynchronously, so the returned object may have an empty title/description
// — they'll be populated by the time the user opens it in the UI.
func (c *Client) CreateBookmark(ctx context.Context, in CreateInput) (*Bookmark, error) {
	resp, err := c.request(ctx, http.MethodPost, "/api/bookmarks", in)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	id := strings.TrimSpace(resp.Header.Get("bookmark-id"))
	if id == "" {
		id = bookmarkIDFromLocation(resp.Header.Get("Location"))
	}
	if id == "" {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("readeck POST /api/bookmarks: %d: missing bookmark-id and usable Location headers (body: %s)", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	bm, err := c.GetBookmark(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("readeck created bookmark %q but read-back failed: %w", id, err)
	}
	if bm.ID == "" {
		return nil, fmt.Errorf("readeck GET /api/bookmarks/%s returned a bookmark without an id", url.PathEscape(id))
	}
	return bm, nil
}

func bookmarkIDFromLocation(location string) string {
	u, err := url.Parse(strings.TrimSpace(location))
	if err != nil {
		return ""
	}
	path := strings.TrimSuffix(u.Path, "/")
	if path == "" || path == "/api/bookmarks" || path == "/bookmarks" {
		return ""
	}
	return path[strings.LastIndex(path, "/")+1:]
}

// ListBookmarks fetches bookmarks matching opts.
func (c *Client) ListBookmarks(ctx context.Context, opts ListOpts) ([]Bookmark, error) {
	v := url.Values{}
	if opts.Search != "" {
		v.Set("search", opts.Search)
	}
	if opts.Limit > 0 {
		v.Set("limit", fmt.Sprintf("%d", opts.Limit))
	}
	if opts.Offset > 0 {
		v.Set("offset", fmt.Sprintf("%d", opts.Offset))
	}
	if opts.Unread {
		v.Set("is_archived", "false")
	}
	if opts.Archived {
		v.Set("is_archived", "true")
	}
	path := "/api/bookmarks"
	if q := v.Encode(); q != "" {
		path += "?" + q
	}
	var out []Bookmark
	if err := c.do(ctx, "GET", path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// FindByURL scans paginated bookmarks and returns a full-URL match.
// Readeck's search parameter uses full-text search, which does not index URLs.
// A nil bookmark means the URL was not found.
func (c *Client) FindByURL(ctx context.Context, rawURL string) (*Bookmark, error) {
	want, err := normalizeBookmarkURL(rawURL)
	if err != nil {
		return nil, err
	}
	const pageSize = 100
	for offset := 0; ; {
		items, err := c.ListBookmarks(ctx, ListOpts{Limit: pageSize, Offset: offset})
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			return nil, nil
		}
		for i := range items {
			got, err := normalizeBookmarkURL(items[i].URL)
			if err == nil && got == want {
				return &items[i], nil
			}
		}
		offset += len(items)
	}
}

// normalizeBookmarkURL ignores spelling differences that do not identify a
// different HTTP resource. Paths and query strings remain exact.
func normalizeBookmarkURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || (!strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https")) || u.Hostname() == "" {
		return "", fmt.Errorf("invalid bookmark URL %q: expected an absolute http(s) URL", raw)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		u.Host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		u.Host = "[" + host + "]"
	} else {
		u.Host = host
	}
	u.Fragment = ""
	u.RawFragment = ""
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String(), nil
}

// PermalinkOf returns the human-facing Readeck URL for a bookmark.
func PermalinkOf(baseURL, id string) string {
	return strings.TrimRight(baseURL, "/") + "/bookmarks/" + id
}

// UpdateInput is the body for PATCH /api/bookmarks/{id}. Pointer bools let
// callers distinguish "leave alone" (nil) from "set to false" (&false).
// Labels, when non-nil, REPLACES the bookmark's labels — to add or remove,
// use Client.AddLabels / Client.RemoveLabels which do the get-and-merge.
type UpdateInput struct {
	IsArchived *bool    `json:"is_archived,omitempty"`
	IsMarked   *bool    `json:"is_marked,omitempty"`
	Labels     []string `json:"labels,omitempty"`
}

// GetBookmark fetches one bookmark by id.
func (c *Client) GetBookmark(ctx context.Context, id string) (*Bookmark, error) {
	var bm Bookmark
	if err := c.do(ctx, "GET", "/api/bookmarks/"+url.PathEscape(id), nil, &bm); err != nil {
		return nil, err
	}
	return &bm, nil
}

// UpdateBookmark sends a PATCH for the given fields.
func (c *Client) UpdateBookmark(ctx context.Context, id string, in UpdateInput) error {
	return c.do(ctx, "PATCH", "/api/bookmarks/"+url.PathEscape(id), in, nil)
}

// DeleteBookmark removes a bookmark.
func (c *Client) DeleteBookmark(ctx context.Context, id string) error {
	return c.do(ctx, "DELETE", "/api/bookmarks/"+url.PathEscape(id), nil, nil)
}

// AddLabels appends labels to a bookmark, deduplicated.
func (c *Client) AddLabels(ctx context.Context, id string, add []string) error {
	bm, err := c.GetBookmark(ctx, id)
	if err != nil {
		return err
	}
	merged := mergeLabels(bm.Labels, add)
	return c.UpdateBookmark(ctx, id, UpdateInput{Labels: merged})
}

// RemoveLabels strips the named labels from a bookmark.
func (c *Client) RemoveLabels(ctx context.Context, id string, remove []string) error {
	bm, err := c.GetBookmark(ctx, id)
	if err != nil {
		return err
	}
	kept := filterLabels(bm.Labels, remove)
	return c.UpdateBookmark(ctx, id, UpdateInput{Labels: kept})
}

// Label is one row from GET /api/bookmarks/labels.
type Label struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// ListLabels returns all labels with their bookmark counts.
func (c *Client) ListLabels(ctx context.Context) ([]Label, error) {
	var out []Label
	if err := c.do(ctx, "GET", "/api/bookmarks/labels", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetArticleHTML returns the extracted HTML body of an article. Readeck
// serves no .md endpoint — see GetArticleMarkdown for a converted form.
func (c *Client) GetArticleHTML(ctx context.Context, id string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/api/bookmarks/"+url.PathEscape(id)+"/article", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("readeck GET article %s: %d %s", id, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// GetArticleMarkdown fetches the article and converts to Markdown so LLM
// callers pay fewer tokens than they would on raw HTML.
func (c *Client) GetArticleMarkdown(ctx context.Context, id string) (string, error) {
	html, err := c.GetArticleHTML(ctx, id)
	if err != nil {
		return "", err
	}
	conv := md.NewConverter("", true, nil)
	return conv.ConvertString(html)
}

func mergeLabels(existing, add []string) []string {
	seen := make(map[string]struct{}, len(existing))
	out := make([]string, 0, len(existing)+len(add))
	for _, l := range existing {
		seen[l] = struct{}{}
		out = append(out, l)
	}
	for _, l := range add {
		if _, ok := seen[l]; ok {
			continue
		}
		seen[l] = struct{}{}
		out = append(out, l)
	}
	return out
}

func filterLabels(existing, remove []string) []string {
	rm := make(map[string]struct{}, len(remove))
	for _, l := range remove {
		rm[l] = struct{}{}
	}
	out := make([]string, 0, len(existing))
	for _, l := range existing {
		if _, drop := rm[l]; drop {
			continue
		}
		out = append(out, l)
	}
	return out
}

// BoolPtr returns a pointer to b — handy for UpdateInput's *bool fields
// in callers that build literal patches.
func BoolPtr(b bool) *bool { return &b }
