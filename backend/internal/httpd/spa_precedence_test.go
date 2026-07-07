package httpd

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
)

// newSPATestRouter builds a router with a real SPA mounted so precedence
// can be verified end-to-end. Returns the server URL.
func newSPATestRouter(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html><body>SPA</body></html>"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatalf("mkdir assets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "index.js"), []byte("// js"), 0o644); err != nil {
		t.Fatalf("write js: %v", err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Config{Host: "127.0.0.1", Port: 0, WebUIDir: dir}
	router := NewRouterWithControl(cfg, log, nil, APIDeps{}, ControlDeps{})
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return srv.URL
}

// TestSPA_PrecedenceHealthz: API health probes still respond when SPA is mounted.
func TestSPA_PrecedenceHealthz(t *testing.T) {
	base := newSPATestRouter(t)
	resp, err := http.Get(base + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/healthz = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct == "" {
		t.Error("/healthz missing Content-Type")
	}
}

// TestSPA_PrecedenceAPIRoute: /api/v1/agents is mounted before SPA and must
// still hit its own handler. With APIDeps{} (no catalog), the controller
// returns 501 — the important thing is that it's NOT the SPA fallback.
func TestSPA_PrecedenceAPIRoute(t *testing.T) {
	base := newSPATestRouter(t)
	resp, err := http.Get(base + "/api/v1/agents")
	if err != nil {
		t.Fatalf("GET /api/v1/agents: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("/api/v1/agents = %d, want 501 (controller present, no catalog)", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct == "" {
		t.Error("/api/v1/agents missing Content-Type")
	}
}

// TestSPA_ServesIndexAtRootMounted: with SPA mounted, GET / returns the
// bundle's index.html (200, text/html).
func TestSPA_ServesIndexAtRootMounted(t *testing.T) {
	base := newSPATestRouter(t)
	resp, err := http.Get(base + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET / = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "<html><body>SPA</body></html>" {
		t.Errorf("body = %q, want SPA index", string(body))
	}
}

// TestSPA_ServesDeepLinkAsIndex: deep-link routes return the SPA bundle
// so client-side routing can take over.
func TestSPA_ServesDeepLinkAsIndex(t *testing.T) {
	base := newSPATestRouter(t)
	resp, err := http.Get(base + "/projects/abc/sessions/xyz")
	if err != nil {
		t.Fatalf("GET /projects/abc/sessions/xyz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("deep-link status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "<html><body>SPA</body></html>" {
		t.Errorf("deep-link body = %q, want SPA fallback", string(body))
	}
}

// TestSPA_NotConfiguredIsPassThrough: when WebUIDir is empty, GET / falls
// through to chi's not-found handler (404 JSON envelope).
func TestSPA_NotConfiguredIsPassThrough(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := NewRouterWithControl(config.Config{Host: "127.0.0.1", Port: 0}, log, nil, APIDeps{}, ControlDeps{})
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET / = %d, want 404 (SPA not configured)", resp.StatusCode)
	}
}
