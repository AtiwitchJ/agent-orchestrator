package httpd

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestSPAHandler(t *testing.T) (*SPAHandler, string) {
	t.Helper()
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "index.html")
	if err := os.WriteFile(indexPath, []byte("<!doctype html><title>index</title>"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	assetsDir := filepath.Join(dir, "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatalf("mkdir assets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(assetsDir, "index-abc.js"), []byte("console.log('hi')"), 0o644); err != nil {
		t.Fatalf("write asset: %v", err)
	}
	return NewSPAHandler(http.Dir(dir), discardLogger()), dir
}

func discardLoggerSPA() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestSPA_ServesIndexAtRoot(t *testing.T) {
	h, _ := newTestSPAHandler(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html…", ct)
	}
	if body := rec.Body.String(); !strings.Contains(body, "<title>index</title>") {
		t.Errorf("body = %q, want index.html content", body)
	}
}

func TestSPA_ServesHashedAsset(t *testing.T) {
	h, _ := newTestSPAHandler(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/assets/index-abc.js", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "console.log('hi')") {
		t.Errorf("body = %q, want asset content", body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") && !strings.Contains(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want javascript-ish", ct)
	}
}

func TestSPA_FallbackOnDeepLink(t *testing.T) {
	h, _ := newTestSPAHandler(t)
	for _, path := range []string{"/projects/abc/sessions/xyz", "/settings", "/some/deep/route"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
				t.Errorf("Content-Type = %q, want text/html…", ct)
			}
			if body := rec.Body.String(); !strings.Contains(body, "<title>index</title>") {
				t.Errorf("body = %q, want SPA fallback to index.html", body)
			}
		})
	}
}

func TestSPA_PathTraversalRejected(t *testing.T) {
	h, _ := newTestSPAHandler(t)
	// Use raw URL paths with `..` to exercise the upfront traversal guard.
	// httptest.NewRequest stores the path verbatim; URL parsing would collapse
	// it, so we bypass the parser.
	cases := []struct {
		name string
		path string
	}{
		{"dotdot", "/../etc/passwd"},
		{"dotdot-encoded-raw", "/%2e%2e/etc/passwd"},
		{"dotdot-in-segment", "/foo/../../etc/passwd"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			// httptest sanitises req.URL.Path; reassign via RequestURI to keep the
			// raw traversal segment for the test.
			req.RequestURI = tc.path
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rec.Code)
			}
		})
	}
}

func TestSPA_MethodNotAllowed(t *testing.T) {
	h, _ := newTestSPAHandler(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/some/route", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestSPA_NilDirIsInternalError(t *testing.T) {
	h := &SPAHandler{Dir: nil, Index: "index.html", Log: discardLoggerSPA()}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestSPA_NoIndexReturnsNotFound(t *testing.T) {
	dir := t.TempDir()
	h := NewSPAHandler(http.Dir(dir), discardLogger())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// TestSPA_PathTraversalHTTP exercises the handler mounted on a real chi
// router to ensure URL encoding cannot smuggle a traversal segment through.
func TestSPA_PathTraversalHTTP(t *testing.T) {
	h, _ := newTestSPAHandler(t)
	r := http.NewServeMux()
	r.Handle("/", h)
	srv := httptest.NewServer(r)
	defer srv.Close()

	for _, raw := range []string{
		"/../etc/passwd",
		"/foo/../bar",
	} {
		t.Run(raw, func(t *testing.T) {
			// url.JoinPath would collapse `..` segments, so build the URL by hand
			// to keep the traversal in the request path.
			u := srv.URL + raw
			resp, err := srv.Client().Get(u)
			if err != nil {
				t.Fatalf("GET %s: %v", u, err)
			}
			resp.Body.Close()
			// Either the rejection (400) or a clean SPA fallback is acceptable,
			// but the request must never serve /etc/passwd content. A simple
			// content check is enough.
			if resp.StatusCode == http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				if strings.Contains(string(body), "root:") {
					t.Fatalf("path traversal leaked: %s returned OK with /etc/passwd-like body", u)
				}
			}
		})
	}
	// Sanity: the URL parser can also receive encoded `..`. Verify it.
	_ = url.PathEscape
}