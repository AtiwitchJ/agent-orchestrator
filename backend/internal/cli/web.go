package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/daemon"
)

type webOptions struct {
	port    int
	dataDir string
	noOpen  bool
	json    bool
}

// webResult is the JSON shape emitted with --json: which port was bound,
// whether the SPA bundle was found, and whether the browser was opened.
type webResult struct {
	Port     int  `json:"port"`
	SPAFound bool `json:"spaFound"`
	Opened   bool `json:"opened"`
}

func newWebCommand(ctx *commandContext) *cobra.Command {
	opts := webOptions{}
	cmd := &cobra.Command{
		Use:   "web",
		Short: "Start the AO daemon and open the web UI in your default browser",
		Long: "Start the AO daemon (in-process) and open the bundled web UI in your\n" +
			"default browser. The web UI is the same renderer the Electron desktop\n" +
			"app uses; it is served by the daemon at http://127.0.0.1:<port>/ when\n" +
			"AO_WEB_UI_DIR points at a built bundle (or one is found beside the\n" +
			"binary or under ~/.ao/web).\n\n" +
			"If no SPA bundle is found the daemon still runs and serves its HTTP\n" +
			"API; only the browser shortcut is suppressed. Use --no-open to start\n" +
			"without spawning a browser.",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return ctx.runWeb(cmd.Context(), cmd, opts)
		},
	}
	cmd.Flags().IntVar(&opts.port, "port", 0, "Override the daemon bind port (default AO_PORT or 3001)")
	cmd.Flags().StringVar(&opts.dataDir, "data-dir", "", "Override AO_DATA_DIR for this run")
	cmd.Flags().BoolVar(&opts.noOpen, "no-open", false, "Start the daemon without opening a browser")
	cmd.Flags().BoolVar(&opts.json, "json", false, "Output the start result as JSON")
	return cmd
}

// runWeb resolves options, prepares env, and invokes the in-process daemon
// via the same daemon.Run path that `ao daemon` uses. We do NOT spawn a
// subprocess — the daemon needs the parent's signal handling so Ctrl-C
// cleanly tears it down. A goroutine polls /healthz and fires the browser
// opener once the daemon binds; the foreground call blocks until the
// daemon exits (graceful shutdown via signals).
func (c *commandContext) runWeb(ctx context.Context, cmd *cobra.Command, opts webOptions) error {
	out := cmd.OutOrStdout()
	res := webResult{}

	// Apply overrides BEFORE the daemon reads env. daemon.Run calls config.Load
	// internally; setting env here is the cleanest way to thread overrides
	// through that path without exposing new flags in the daemon itself.
	if opts.port > 0 {
		if err := os.Setenv("AO_PORT", fmt.Sprintf("%d", opts.port)); err != nil {
			return fmt.Errorf("set AO_PORT: %w", err)
		}
	}
	if opts.dataDir != "" {
		if err := os.Setenv("AO_DATA_DIR", opts.dataDir); err != nil {
			return fmt.Errorf("set AO_DATA_DIR: %w", err)
		}
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	res.Port = cfg.Port
	res.SPAFound = cfg.WebUIDir != ""

	if !opts.noOpen && res.SPAFound {
		// Watch for the daemon to bind so the browser doesn't open before
		// the SPA is actually being served. Polling /healthz is cheaper than
		// parsing the run-file and reuses the existing health endpoint.
		go c.waitAndOpenBrowser(cfg.Addr())
	} else if !opts.noOpen && !res.SPAFound {
		fmt.Fprintln(cmd.ErrOrStderr(),
			"warning: no SPA bundle found at AO_WEB_UI_DIR, beside the binary, or under ~/.ao/web; "+
				"the daemon's HTTP API will still start, but nothing to open in the browser.")
	}

	if opts.json {
		if err := writeJSON(out, res); err != nil {
			return err
		}
	}

	// Block on the in-process daemon until SIGINT/SIGTERM. This is the same
	// path `ao daemon` uses; signals funnel through signal.NotifyContext in
	// daemon.Run so Ctrl-C triggers a graceful shutdown.
	return c.invokeDaemon(ctx)
}

// invokeDaemon is the seam that lets tests swap the daemon entry point.
// Production wires it to the real daemon.Run from cli.Deps.
func (c *commandContext) invokeDaemon(ctx context.Context) error {
	if c.deps.DaemonRun != nil {
		return c.deps.DaemonRun(ctx)
	}
	return daemon.Run()
}

// waitAndOpenBrowser polls /healthz on the loopback daemon and opens the
// browser once the server is healthy. The SPA path is opened directly so
// the user lands on the dashboard instead of the API root.
func (c *commandContext) waitAndOpenBrowser(addr string) {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(5 * time.Second)
	url := "http://" + addr + "/"
	for time.Now().Before(deadline) {
		resp, err := client.Get(url + "healthz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				_ = openBrowser(url)
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Health probe timed out — open the URL anyway so a slow daemon boot
	// still produces a usable experience.
	_ = openBrowser(url)
}

// openBrowser launches the user's default browser. Returns an error only
// when the platform opener itself fails; a missing browser is silent.
func openBrowser(rawURL string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", rawURL).Start()
	case "windows":
		// rundll32 url.dll,FileProtocolHandler is the canonical "open in
		// default browser" command on Windows. It returns immediately.
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL).Start()
	default:
		// Linux/BSD: try xdg-open first, then fall back to common alternatives.
		for _, opener := range []string{"xdg-open", "wslview", "sensible-browser"} {
			if _, err := exec.LookPath(opener); err == nil {
				return exec.Command(opener, rawURL).Start()
			}
		}
		return errors.New("no browser opener found (install xdg-open)")
	}
}
