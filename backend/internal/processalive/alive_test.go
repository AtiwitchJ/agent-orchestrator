package processalive_test

import (
	"os"
	"testing"

	"github.com/modernagent/modern-agent/backend/internal/processalive"
)

// TestAliveSelfIsAlive pins the canonical positive case: a process id that
// points to ourselves must answer true regardless of platform — the function
// is used to detect leaked runtimes whose PID we still own.
//
// The companion invariant it pins: a freshly pidfile-reaped run whose PID has
// already been recycled by the OS must answer false; Alive() is NEVER allowed
// to treat "no permission to signal" as proof the session is dead — the AGENTS
// rule `probe fail != session dead` reduces to:
//   EPERM/AccessDenied → true (process exists, we just can't signal it)
//   ESRCH/NotFound     → false (process is gone)
// That is the load-bearing contract the lifecycle reaper depends on.
func TestAliveSelfIsAlive(t *testing.T) {
	pid := os.Getpid()
	if !processalive.Alive(pid) {
		t.Fatalf("Alive(self pid=%d) = false; want true", pid)
	}
}

// TestAliveRejectsNonPositivePins the safety property: a zero or negative PID
// is never a real process and must short-circuit to false so callers cannot
// accidentally probe pid 0 or the kernel.
func TestAliveRejectsNonPositive(t *testing.T) {
	tests := []struct {
		name string
		pid  int
	}{
		{"zero", 0},
		{"negative", -1},
		{"large-negative", -9999},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if processalive.Alive(tt.pid) {
				t.Fatalf("Alive(%d) = true; want false", tt.pid)
			}
		})
	}
}

// TestAliveNonexistentPIDReturnsFalse exercises the ESRCH/NotFound branch.
// 0x7FFFFFFE is a high-unix-pid that on this run will never have been issued
// since the kernel typically caps much lower, and using a sentinel value
// instead of a runtime-bounded PID keeps the test deterministic across machines.
func TestAliveNonexistentPIDReturnsFalse(t *testing.T) {
	const mustNotExist = 0x7FFFFFFE
	if processalive.Alive(mustNotExist) {
		t.Fatalf("Alive(%#x) = true; want false (no such process)", mustNotExist)
	}
}
