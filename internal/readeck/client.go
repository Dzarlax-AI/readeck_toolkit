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
	"net/http"
	"net/url"
	"strings"
	"time"
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
	Unread   bool // narrow to is_archived=false
	Archived bool // narrow to is_archived=true
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("readeck %s %s: %d %s", method, path, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if out != nil && resp.StatusCode != http.StatusNoContent {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// CreateBookmark posts a new bookmark. Readeck does content extraction
// asynchronously, so the returned object may have an empty title/description
// — they'll be populated by the time the user opens it in the UI.
func (c *Client) CreateBookmark(ctx context.Context, in CreateInput) (*Bookmark, error) {
	var bm Bookmark
	if err := c.do(ctx, "POST", "/api/bookmarks", in, &bm); err != nil {
		return nil, err
	}
	return &bm, nil
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

// PermalinkOf returns the human-facing Readeck URL for a bookmark.
func PermalinkOf(baseURL, id string) string {
	return strings.TrimRight(baseURL, "/") + "/bookmarks/" + id
}
