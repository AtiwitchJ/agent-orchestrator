package httpd

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// SPAHandler serves a static single-page-app bundle at `/`. It serves files
// from Dir directly when they exist and falls back to Index for any
// unmatched path so deep-link routes (e.g. `/projects/abc/sessions/xyz`)
// reach the SPA's client-side router. Path traversal is rejected: any
// request whose cleaned URL path escapes the configured root returns 400.
type SPAHandler struct {
	Dir   http.FileSystem
	Index string
	Log   *slog.Logger
}

const (
	spaIndexContentType = "text/html; charset=utf-8"
	defaultSPAIndex     = "index.html"
)

// NewSPAHandler wraps Dir, defaulting Index to "index.html". Log may be nil.
func NewSPAHandler(dir http.FileSystem, log *slog.Logger) *SPAHandler {
	return &SPAHandler{Dir: dir, Index: defaultSPAIndex, Log: log}
}

func (h *SPAHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.Dir == nil {
		http.Error(w, "SPA directory not configured", http.StatusInternalServerError)
		return
	}
	// Only safe verbs reach the static layer. POST/PUT/DELETE/PATCH against a
	// SPA path are always bugs; the SPA itself only does GETs.
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Strip the query string; URL.Path is what we serve from disk.
	upath := r.URL.Path
	if upath == "" || upath == "/" {
		upath = "/" + h.Index
	}

	// Reject obvious traversal attempts before they touch the filesystem. The
	// deeper guard is filepath.Clean against the root, but cheap rejection
	// up front keeps the logs clean.
	if strings.Contains(upath, "..") {
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}

	// Serve the requested file if it exists. ServeContent sets Content-Type
	// from the extension and supports range requests, which Vite's emitted
	// assets rely on for the SPA's preload graph.
	file, err := h.Dir.Open(path.Clean(upath))
	if err == nil {
		defer file.Close()
		info, statErr := file.Stat()
		if statErr == nil {
			if info.IsDir() {
				// Directory requested without a trailing file → fall back to
				// index.html (matches SPA serving convention).
				h.serveIndex(w, r)
				return
			}
			http.ServeContent(w, r, info.Name(), info.ModTime(), file)
			return
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		h.logf("spa open %s: %v", upath, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// File not found → SPA fallback. This is what makes `/projects/abc`
	// resolve to index.html so React Router can pick it up client-side.
	h.serveIndex(w, r)
}

func (h *SPAHandler) serveIndex(w http.ResponseWriter, r *http.Request) {
	if h.Index == "" {
		h.Index = defaultSPAIndex
	}
	file, err := h.Dir.Open(h.Index)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.Error(w, fmt.Sprintf("SPA index %q not found", h.Index), http.StatusNotFound)
			return
		}
		h.logf("spa index open: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer file.Close()
	info, statErr := file.Stat()
	if statErr != nil {
		h.logf("spa index stat: %v", statErr)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// Always advertise text/html for the SPA entry point regardless of
	// extension sniffing, so a server-side content-type tweak cannot break
	// the SPA boot.
	w.Header().Set("Content-Type", spaIndexContentType)
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

func (h *SPAHandler) logf(format string, args ...any) {
	if h.Log == nil {
		return
	}
	h.Log.Warn(fmt.Sprintf(format, args...))
}

// cleanRoot returns the absolute, cleaned form of the on-disk root for
// traversal checks. Currently unused (the filesystem layer is locked down
// by strings.Contains check + http.Dir's own path normalization), but kept
// available for callers that want to log or audit the resolved root.
func cleanRoot(dir http.FileSystem) string {
	if d, ok := dir.(http.Dir); ok {
		return filepath.Clean(string(d))
	}
	return ""
}