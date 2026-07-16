package agentlaunch_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/modernagent/modern-agent/backend/internal/agentlaunch"
)

// TestWriteTempReadAndRemoveRoundTrip pins the contract the launcher trampoline
// depends on: a Spec written to a temp file must come back byte-equivalent
// through ReadAndRemove. Argv must round-trip because the trampoline exec's
// exactly that vector; FallbackArgv is optional and must survive the empty-omits
// JSON transform that the launcher relies on to keep its argv list short.
func TestWriteTempReadAndRemoveRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		spec agentlaunch.Spec
	}{
		{
			name: "argv-only",
			spec: agentlaunch.Spec{WorkspacePath: "/repo", Argv: []string{"claude-code", "--prompt", "fix it"}},
		},
		{
			name: "with-fallback",
			spec: agentlaunch.Spec{
				WorkspacePath: "/repo",
				Argv:          []string{"codex", "exec"},
				FallbackArgv:  []string{"bash", "-lc", "sleep 5 && claude-code"},
			},
		},
		{
			name: "trailing-args-preserve-order",
			spec: agentlaunch.Spec{
				WorkspacePath: "/worktree",
				Argv:          []string{"claude-code", "--model", "sonnet", "--", "fix the bug in auth.go"},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			path, err := agentlaunch.WriteTemp(tt.spec)
			if err != nil {
				t.Fatalf("WriteTemp: %v", err)
			}
			defer func() { _ = os.Remove(path) }()

			got, err := agentlaunch.ReadAndRemove(path)
			if err != nil {
				t.Fatalf("ReadAndRemove: %v", err)
			}
			if !reflect.DeepEqual(got, tt.spec) {
				t.Fatalf("round-trip mismatch:\n got  %#v\n want %#v", got, tt.spec)
			}
			// ReadAndRemove must actually delete the file — leaving it around would
			// leak /tmp until reboot and is part of the API contract.
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("spec file still present after ReadAndRemove: %v", err)
			}
		})
	}
}

// TestReadAndRemoveRequiresArgv pins the safety property: a Spec whose Argv
// is missing or empty must fail loudly at parse time (the trampoline would
// otherwise exec nothing). The error is surfaced to the launcher so the
// spawning session fails to start instead of hanging in an exec loop.
func TestReadAndRemoveRequiresArgv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spec.json")
	if err := os.WriteFile(path, []byte(`{"workspacePath":"/repo"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := agentlaunch.ReadAndRemove(path); err == nil {
		t.Fatal("ReadAndRemove with missing argv: nil error; want error")
	} else if !strings.Contains(err.Error(), "argv") {
		t.Fatalf("error %q must mention argv", err)
	}
}

// TestReadAndRemoveRejectsMalformedJSON pins the parsing contract: a spec file
// that isn't valid JSON must error rather than silently returning a zero Spec
// (which would then fail the argv check with a confusing message).
func TestReadAndRemoveRejectsMalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spec.json")
	if err := os.WriteFile(path, []byte(`not json at all`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := agentlaunch.ReadAndRemove(path)
	if err == nil {
		t.Fatal("ReadAndRemove with malformed JSON: nil error; want error")
	}
}
