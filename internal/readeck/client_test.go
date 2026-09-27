package readeck

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCreateBookmarkReadBack covers the POST identity headers and GET result.
func TestCreateBookmarkReadBack(t *testing.T) {
	tests := []struct {
		name    string
		headers http.Header
		wantID  string
		wantErr string
	}{
		{
			name:    "bookmark-id takes precedence",
			headers: http.Header{"Bookmark-Id": {"from-header"}, "Location": {"/api/bookmarks/from-location"}},
			wantID:  "from-header",
		},
		{
			name:    "Location fallback",
			headers: http.Header{"Location": {"https://read.example/api/bookmarks/from-location?view=full"}},
			wantID:  "from-location",
		},
		{
			name:    "Location fallback outside API path",
			headers: http.Header{"Location": {"https://read.example/bookmarks/from-public-location"}},
			wantID:  "from-public-location",
		},
		{
			name:    "missing ID headers",
			headers: http.Header{},
			wantErr: "missing bookmark-id and usable Location headers",
		},
		{
			name:    "Location without ID",
			headers: http.Header{"Location": {"/api/bookmarks/"}},
			wantErr: "missing bookmark-id and usable Location headers",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			getCount := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test-token" {
					t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
				}
				switch r.Method + " " + r.URL.Path {
				case "POST /api/bookmarks":
					if r.Header.Get("Content-Type") != "application/json" {
						t.Errorf("Content-Type = %q", r.Header.Get("Content-Type"))
					}
					for key, values := range tc.headers {
						w.Header()[key] = values
					}
					w.WriteHeader(http.StatusAccepted)
					fmt.Fprint(w, `{"status":202,"message":"Link submited"}`)
				case "GET /api/bookmarks/" + tc.wantID:
					getCount++
					fmt.Fprintf(w, `{"id":%q,"title":"Read back title","url":"https://example.com/story"}`, tc.wantID)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()

			bm, err := NewClient(srv.URL, "test-token").CreateBookmark(context.Background(), CreateInput{URL: "https://example.com/story"})
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) || !strings.Contains(err.Error(), "202") || !strings.Contains(err.Error(), "Link submited") {
					t.Fatalf("error = %v, want status, body, and %q", err, tc.wantErr)
				}
				if getCount != 0 {
					t.Fatalf("GET count = %d, want 0", getCount)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if bm.ID != tc.wantID || bm.Title != "Read back title" || getCount != 1 {
				t.Fatalf("bookmark = %+v; GET count = %d", bm, getCount)
			}
		})
	}
}

// TestCreateBookmarkErrorsPreserveStatusAndBody retains useful POST failure details.
func TestCreateBookmarkErrorsPreserveStatusAndBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		fmt.Fprint(w, `{"message":"invalid link"}`)
	}))
	defer srv.Close()
	_, err := NewClient(srv.URL, "token").CreateBookmark(context.Background(), CreateInput{URL: "https://example.com"})
	if err == nil || !strings.Contains(err.Error(), "422") || !strings.Contains(err.Error(), "invalid link") {
		t.Fatalf("error = %v, want status and body", err)
	}
}

// TestCreateBookmarkReadBackFailureReturnsCreatedID marks a committed save as successful.
func TestCreateBookmarkReadBackFailureReturnsCreatedID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.Header().Set("bookmark-id", "created-id")
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprint(w, `{"status":202,"message":"Link submited"}`)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"message":"temporarily unavailable"}`)
	}))
	defer srv.Close()

	bm, err := NewClient(srv.URL, "token").CreateBookmark(context.Background(), CreateInput{URL: "https://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if bm.ID != "created-id" || bm.ReadBackWarning == "" || !strings.Contains(bm.ReadBackWarning, "503") || !strings.Contains(bm.ReadBackWarning, "temporarily unavailable") {
		t.Fatalf("bookmark = %+v", bm)
	}
}

// TestNormalizeBookmarkURL checks the equivalences used for exact lookup.
func TestNormalizeBookmarkURL(t *testing.T) {
	a, err := normalizeBookmarkURL("  HTTPS://EXAMPLE.COM:443/story?x=1#section  ")
	if err != nil || a != "https://example.com/story?x=1" {
		t.Fatalf("normalized = %q, error = %v", a, err)
	}
	root, err := normalizeBookmarkURL("https://example.com")
	if err != nil || root != "https://example.com/" {
		t.Fatalf("root = %q, error = %v", root, err)
	}
	for _, raw := range []string{"example.com/story", "ftp://example.com/story", "https:///story"} {
		if _, err := normalizeBookmarkURL(raw); err == nil {
			t.Errorf("expected invalid URL: %q", raw)
		}
	}
}

// TestFindByURLMatchesFullURLAcrossPages preserves pagination and exact matching.
func TestFindByURLMatchesFullURLAcrossPages(t *testing.T) {
	var offsets []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/bookmarks" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("site") != "" || q.Get("search") != "" || q.Get("limit") != "100" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		offsets = append(offsets, q.Get("offset"))
		switch q.Get("offset") {
		case "":
			fmt.Fprint(w, `[{"id":"wrong-path","url":"https://example.com/other"},{"id":"wrong-query","url":"https://example.com/story?x=2"}]`)
		case "2":
			fmt.Fprint(w, `[{"id":"match","title":"Found","url":"https://EXAMPLE.com:443/story?x=1#fragment"}]`)
		default:
			fmt.Fprint(w, `[]`)
		}
	}))
	defer srv.Close()
	bm, err := NewClient(srv.URL, "token").FindByURL(context.Background(), "https://example.com/story?x=1")
	if err != nil || bm == nil || bm.ID != "match" {
		t.Fatalf("bookmark = %+v, error = %v", bm, err)
	}
	if strings.Join(offsets, ",") != ",2" {
		t.Fatalf("offsets = %v", offsets)
	}
}

// TestFindByURLNotFound returns nil when the collection has no URL match.
func TestFindByURLNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()
	bm, err := NewClient(srv.URL, "token").FindByURL(context.Background(), "https://example.com/missing")
	if err != nil || bm != nil {
		t.Fatalf("bookmark = %+v, error = %v", bm, err)
	}
}

// TestFindByURLStopsOnRepeatedPages prevents unbounded requests if offsets are ignored.
func TestFindByURLStopsOnRepeatedPages(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		fmt.Fprint(w, `[{"id":"same-page","url":"https://example.com/other"}]`)
	}))
	defer srv.Close()

	bm, err := NewClient(srv.URL, "token").FindByURL(context.Background(), "https://example.com/missing")
	if err != nil || bm != nil {
		t.Fatalf("bookmark = %+v, error = %v", bm, err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
}
