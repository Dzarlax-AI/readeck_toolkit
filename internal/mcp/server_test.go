package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSaveToolReturnsReadBackIDAndPermalink(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.Method + " " + r.URL.Path {
		case "POST /api/bookmarks":
			w.Header().Set("bookmark-id", "saved-123")
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprint(w, `{"status":202,"message":"Link submited"}`)
		case "GET /api/bookmarks/saved-123":
			fmt.Fprint(w, `{"id":"saved-123","title":"Saved title","url":"https://example.com/story"}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ctx := context.WithValue(context.Background(), tokenKey{}, "test-token")
	response := New(srv.URL).HandleMessage(ctx, []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"readeck_save","arguments":{"url":"https://example.com/story"}}}`))
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var rpc struct {
		Error  any `json:"error"`
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(encoded, &rpc); err != nil {
		t.Fatal(err)
	}
	if rpc.Error != nil || rpc.Result.IsError || len(rpc.Result.Content) != 1 {
		t.Fatalf("MCP response = %s", encoded)
	}
	var saved struct {
		ID        string `json:"id"`
		Permalink string `json:"permalink"`
		Title     string `json:"title"`
	}
	if err := json.Unmarshal([]byte(rpc.Result.Content[0].Text), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.ID != "saved-123" || saved.Permalink != srv.URL+"/bookmarks/saved-123" || saved.Title != "Saved title" {
		t.Fatalf("saved = %+v", saved)
	}
}

func TestFindByURLToolDoesNotUseFullTextSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("site") != "" || r.URL.Query().Get("search") != "" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		fmt.Fprint(w, `[{"id":"other","url":"https://example.com/other"},{"id":"match","title":"Match","url":"https://example.com/story"}]`)
	}))
	defer srv.Close()
	ctx := context.WithValue(context.Background(), tokenKey{}, "test-token")
	response := New(srv.URL).HandleMessage(ctx, []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"readeck_find_by_url","arguments":{"url":"https://EXAMPLE.com/story#fragment"}}}`))
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), "[id=match]") || strings.Contains(string(encoded), "[id=other]") {
		t.Fatalf("MCP response = %s", encoded)
	}
}
