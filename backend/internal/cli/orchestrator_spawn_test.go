package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestOrchestratorSpawnCommand_RequiresProject asserts `ao orchestrator spawn`
// rejects a missing --project before touching the network.
func TestOrchestratorSpawnCommand_RequiresProject(t *testing.T) {
	var out, errb bytes.Buffer
	root := NewRootCommand(Deps{Out: &out, Err: &errb})
	root.SetArgs([]string{"orchestrator", "spawn"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected an error when --project is missing")
	}
	if !strings.Contains(err.Error(), "--project is required") {
		t.Fatalf("error = %v, want it to mention --project is required", err)
	}
}

func TestOrchestratorSpawnCommand_PostsProjectAndClean(t *testing.T) {
	cfg := setConfigEnv(t)
	log := &sessionRequestLog{}
	var gotBody spawnOrchestratorRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.append(r)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/orchestrators":
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			_, _ = io.WriteString(w, `{"orchestrator":{"id":"demo-orchestrator","projectId":"demo"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "orchestrator", "spawn", "--project", "demo", "--clean")
	if err != nil {
		t.Fatalf("orchestrator spawn failed: %v\nstderr=%s", err, errOut)
	}
	if gotBody.ProjectID != "demo" || !gotBody.Clean {
		t.Fatalf("request body = %#v, want projectId=demo clean=true", gotBody)
	}
	if !strings.Contains(out, "spawned orchestrator demo-orchestrator for project demo") {
		t.Fatalf("output missing spawn confirmation:\n%s", out)
	}
}
