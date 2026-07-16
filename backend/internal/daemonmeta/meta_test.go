package daemonmeta_test

import (
	"testing"

	"github.com/modernagent/modern-agent/backend/internal/daemonmeta"
)

// TestServiceNameStable pins the contract the CLI uses with the reported PID
// to avoid signaling an unrelated process when a stale run-file's PID has been
// recycled. The value is part of the public surface (scripts grep for it) and
// must not drift without a coordinated CLI/daemon release.
func TestServiceNameStable(t *testing.T) {
	if got, want := daemonmeta.ServiceName, "modern-agent-daemon"; got != want {
		t.Fatalf("ServiceName = %q; want %q", got, want)
	}
}
