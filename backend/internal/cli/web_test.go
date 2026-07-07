package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/modernagent/modern-agent/backend/internal/config"
)

func TestWebCommand_Flags(t *testing.T) {
	cmd := newWebCommand(&commandContext{deps: DefaultDeps()})
	for _, name := range []string{"port", "data-dir", "no-open", "json"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("flag --%s missing", name)
		}
	}
}

func TestOpenBrowser_DoesNotPanic(t *testing.T) {
	// openBrowser shells out to a platform opener; we cannot assert the
	// browser launches from a CI sandbox. Instead, ensure it does not panic
	// and tolerates unusual URLs.
	if err := openBrowser("http://127.0.0.1:1/"); err != nil {
		// Acceptable errors: missing xdg-open on Linux, etc.
		t.Logf("openBrowser returned (expected on headless): %v", err)
	}
}

// TestRunWeb_DaemonRunCalled asserts that runWeb invokes the DaemonRun
// dependency (or the real daemon.Run fallback) after printing the JSON
// result. We swap DaemonRun with a recording fake so the test never tries
// to bind a real port.
func TestRunWeb_DaemonRunCalled(t *testing.T) {
	deps := DefaultDeps()
	called := false
	deps.DaemonRun = func(_ context.Context) error {
		called = true
		return nil
	}
	ctx := &commandContext{deps: deps}

	cmd := newWebCommand(ctx)
	out := &bytes.Buffer{}
	cmd.SetOut(out)

	// Restore env touched by runWeb.
	prevPort, hadPort := os.LookupEnv("AO_PORT")
	t.Cleanup(func() {
		if hadPort {
			os.Setenv("AO_PORT", prevPort)
		} else {
			os.Unsetenv("AO_PORT")
		}
	})

	opts := webOptions{noOpen: true, json: true}
	if err := ctx.runWeb(context.Background(), cmd, opts); err != nil {
		t.Fatalf("runWeb: %v", err)
	}
	if !called {
		t.Error("DaemonRun dependency was not invoked")
	}
	// JSON shape sanity check.
	var got webResult
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode JSON: %v\nbody=%q", err, out.String())
	}
	if got.Port <= 0 {
		t.Errorf("Port = %d; expected a positive bind port", got.Port)
	}
}

// TestRunWeb_NoSPAEmitsWarning ensures the warning path fires when no SPA
// bundle is found anywhere — we point AO_WEB_UI_DIR at a non-existent
// location and clear the home-fallback markers. DaemonRun is a no-op so
// the test never starts a real daemon. noOpen is false so the warning
// branch (no SPA + browser requested) actually runs.
func TestRunWeb_NoSPAEmitsWarning(t *testing.T) {
	prev, had := os.LookupEnv("AO_WEB_UI_DIR")
	t.Cleanup(func() {
		if had {
			os.Setenv("AO_WEB_UI_DIR", prev)
		} else {
			os.Unsetenv("AO_WEB_UI_DIR")
		}
	})
	os.Setenv("AO_WEB_UI_DIR", "")

	// Make sure ~/.ao/web does not exist so the fallback returns "".
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home: %v", err)
	}
	web := filepath.Join(home, ".ao", "web")
	if _, err := os.Stat(web); err == nil {
		t.Skipf("~/.ao/web already exists; skipping to avoid clobbering")
	}

	deps := DefaultDeps()
	deps.DaemonRun = func(_ context.Context) error { return nil }
	ctx := &commandContext{deps: deps}

	cmd := newWebCommand(ctx)
	errOut := &bytes.Buffer{}
	cmd.SetErr(errOut)

	// noOpen: false so the warning branch runs; openBrowser will be a no-op
	// on a CI host without a browser.
	if err := ctx.runWeb(context.Background(), cmd, webOptions{noOpen: false}); err != nil {
		t.Fatalf("runWeb: %v", err)
	}
	if got := errOut.String(); got == "" || !contains(got, "no SPA bundle") {
		t.Errorf("expected 'no SPA bundle' warning on stderr, got %q", got)
	}
}

func contains(haystack, needle string) bool {
	return bytes.Contains([]byte(haystack), []byte(needle))
}

// TestRunWeb_AppliesOverrides checks that --port and --data-dir translate
// to env overrides BEFORE the daemon reads them. We verify by reading the
// config after env is set, then resetting to defaults so the test does not
// leak state.
func TestRunWeb_AppliesOverrides(t *testing.T) {
	prevPort, hadPort := os.LookupEnv("AO_PORT")
	prevData, hadData := os.LookupEnv("AO_DATA_DIR")
	t.Cleanup(func() {
		if hadPort {
			os.Setenv("AO_PORT", prevPort)
		} else {
			os.Unsetenv("AO_PORT")
		}
		if hadData {
			os.Setenv("AO_DATA_DIR", prevData)
		} else {
			os.Unsetenv("AO_DATA_DIR")
		}
	})
	os.Unsetenv("AO_PORT")
	os.Unsetenv("AO_DATA_DIR")

	deps := DefaultDeps()
	deps.DaemonRun = func(_ context.Context) error { return nil }
	ctx := &commandContext{deps: deps}
	cmd := newWebCommand(ctx)

	tmp := t.TempDir()
	if err := ctx.runWeb(context.Background(), cmd, webOptions{
		noOpen:  true,
		port:    9999,
		dataDir: tmp,
	}); err != nil {
		t.Fatalf("runWeb: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if cfg.Port != 9999 {
		t.Errorf("Port = %d, want 9999", cfg.Port)
	}
	if cfg.DataDir != tmp {
		t.Errorf("DataDir = %q, want %q", cfg.DataDir, tmp)
	}
}

// keep runtime referenced (used by openBrowser switch).
var _ = runtime.GOOS
