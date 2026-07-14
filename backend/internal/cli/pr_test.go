package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// prTestServer wires an httptest server that answers a fixed set of routes by
// method+path, mirroring the mux-per-test pattern used where a CLI command
// makes more than one daemon call.
func prTestServer(t *testing.T, routes map[string]string) (*httptest.Server, *[]string) {
	t.Helper()
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.String())
		body, ok := routes[r.Method+" "+r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// containsCall reports whether want is among calls. Every CLI invocation also
// fires a fire-and-forget POST /internal/telemetry/cli-invoked, so exact-count
// assertions on daemon calls would be brittle.
func containsCall(calls []string, want string) bool {
	for _, c := range calls {
		if c == want {
			return true
		}
	}
	return false
}

func TestPRListFlattensPRsAcrossSessions(t *testing.T) {
	cfg := setConfigEnv(t)
	sessionsBody := `{"sessions":[
		{"id":"proj-1","prs":[{"url":"https://github.com/o/r/pull/1","number":1,"state":"open","ci":"passing","review":"none","mergeability":"mergeable","updatedAt":"2026-01-01T00:00:00Z"}]},
		{"id":"proj-2","prs":[{"url":"https://github.com/o/r/pull/2","number":2,"state":"open","ci":"failing","review":"none","mergeability":"unknown","updatedAt":"2026-01-02T00:00:00Z"}]}
	]}`
	srv, calls := prTestServer(t, map[string]string{
		"GET /api/v1/sessions": sessionsBody,
	})
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, aliveDeps(), "pr", "list", "myproj", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	var resp struct {
		PRs []prListEntry `json:"prs"`
	}
	if jsonErr := json.Unmarshal([]byte(out), &resp); jsonErr != nil {
		t.Fatalf("decode: %v\nout=%s", jsonErr, out)
	}
	if len(resp.PRs) != 2 {
		t.Fatalf("PRs = %+v, want 2 entries", resp.PRs)
	}
	// Sorted newest-updated first.
	if resp.PRs[0].SessionID != "proj-2" || resp.PRs[1].SessionID != "proj-1" {
		t.Errorf("PRs = %+v, want proj-2 first (most recently updated)", resp.PRs)
	}
	if !containsCall(*calls, "GET /api/v1/sessions?active=true&project=myproj") {
		t.Errorf("calls = %v", *calls)
	}
}

func TestPRListTextOutput(t *testing.T) {
	cfg := setConfigEnv(t)
	sessionsBody := `{"sessions":[{"id":"proj-1","prs":[{"url":"https://github.com/o/r/pull/1","number":1,"state":"open","ci":"passing","review":"approved","mergeability":"mergeable","updatedAt":"2026-01-01T00:00:00Z"}]}]}`
	srv, _ := prTestServer(t, map[string]string{"GET /api/v1/sessions": sessionsBody})
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, aliveDeps(), "pr", "list", "myproj")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if got := out; got == "" {
		t.Fatal("expected non-empty text output")
	}
}

func TestPRListNoResults(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := prTestServer(t, map[string]string{"GET /api/v1/sessions": `{"sessions":[]}`})
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, aliveDeps(), "pr", "list", "myproj")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if out != "(no open pull requests for myproj)\n" {
		t.Errorf("out = %q", out)
	}
}

func TestPRListMissingProjectIsUsageError(t *testing.T) {
	setConfigEnv(t)
	_, _, err := executeCLI(t, aliveDeps(), "pr", "list", " ")
	if got := ExitCode(err); got != 2 {
		t.Fatalf("exit code = %d, want 2 (usage); err=%v", got, err)
	}
}

func TestPRMerge(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, calls := prTestServer(t, map[string]string{
		"POST /api/v1/prs/42/merge": `{"ok":true,"prNumber":42,"method":"squash"}`,
	})
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, aliveDeps(), "pr", "merge", "42")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if out != "merged PR #42 (squash)\n" {
		t.Errorf("out = %q", out)
	}
	if !containsCall(*calls, "POST /api/v1/prs/42/merge") {
		t.Errorf("calls = %v", *calls)
	}
}

func TestPRMergeMissingIDIsUsageError(t *testing.T) {
	setConfigEnv(t)
	_, _, err := executeCLI(t, aliveDeps(), "pr", "merge")
	if got := ExitCode(err); got != 2 {
		t.Fatalf("exit code = %d, want 2 (usage); err=%v", got, err)
	}
}

func TestPRResolveComments(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, calls := prTestServer(t, map[string]string{
		"POST /api/v1/prs/7/resolve-comments": `{"ok":true,"resolved":3}`,
	})
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, aliveDeps(), "pr", "resolve-comments", "7")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if out != "resolved 3 comment thread(s) on PR 7\n" {
		t.Errorf("out = %q", out)
	}
	if !containsCall(*calls, "POST /api/v1/prs/7/resolve-comments") {
		t.Errorf("calls = %v", *calls)
	}
}

func TestPRResolveCommentsMissingIDIsUsageError(t *testing.T) {
	setConfigEnv(t)
	_, _, err := executeCLI(t, aliveDeps(), "pr", "resolve-comments")
	if got := ExitCode(err); got != 2 {
		t.Fatalf("exit code = %d, want 2 (usage); err=%v", got, err)
	}
}
