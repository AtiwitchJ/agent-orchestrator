package gates

import (
	"context"
	"strings"
	"testing"

	"github.com/modernagent/modern-agent/backend/internal/policy"
)

// stubConfig returns a deterministic Config suitable for gate tests.
// Defaults match policy.DefaultPolicyConfig so tests reflect what callers
// would see in production unless they explicitly override a field.
func stubConfig() policy.Config {
	c := policy.DefaultPolicyConfig()
	c.Enabled = true
	return c
}

// TestCIGate_ID verifies the gate reports the right GateID constant.
func TestCIGate_ID(t *testing.T) {
	if got := NewCIGate().ID(); got != policy.GateCI {
		t.Fatalf("CIGate.ID() = %q, want %q", got, policy.GateCI)
	}
}

// TestCIGate_AutoFixDisabled_PassImmediately covers the explicit opt-out path:
// when the project sets AutoFixOnCIFailure=false the gate pretends green
// regardless of attempt count, because Phase 1 has no real SCM signal and
// blocking on "unknown" would deadlock every run.
func TestCIGate_AutoFixDisabled_PassImmediately(t *testing.T) {
	g := NewCIGate()
	c := stubConfig()
	c.AutoFixOnCIFailure = false

	for attempt := 0; attempt < 5; attempt++ {
		out, err := g.Run(context.Background(), policy.RunContext{
			RunID:   "r1",
			PRID:    "pr1",
			Config:  c,
			Attempt: attempt,
		})
		if err != nil {
			t.Fatalf("attempt %d: unexpected error: %v", attempt, err)
		}
		if out != policy.OutcomePass {
			t.Fatalf("attempt %d: got %q, want %q", attempt, out, policy.OutcomePass)
		}
	}
}

// TestCIGate_UnderRoundLimit_Fail exercises the stub retry path: while the
// engine is still under the round limit the gate reports OutcomeFail so the
// engine re-enters. The reason text should mention the attempt so a
// human reviewing the run trace understands this is the stub, not a real
// CI failure.
func TestCIGate_UnderRoundLimit_Fail(t *testing.T) {
	g := NewCIGate()
	c := stubConfig() // MaxAutoFixRounds = 3
	if c.MaxAutoFixRounds <= 0 {
		t.Fatalf("test precondition: MaxAutoFixRounds must be > 0, got %d", c.MaxAutoFixRounds)
	}

	for attempt := 0; attempt < c.MaxAutoFixRounds; attempt++ {
		out, err := g.Run(context.Background(), policy.RunContext{
			RunID: "r1", PRID: "pr1", Config: c, Attempt: attempt,
		})
		if err != nil {
			t.Fatalf("attempt %d: unexpected error: %v", attempt, err)
		}
		if out != policy.OutcomeFail {
			t.Fatalf("attempt %d: got %q, want %q", attempt, out, policy.OutcomeFail)
		}
	}
}

// TestCIGate_ExhaustedAtRoundLimit covers the boundary: once rc.Attempt
// reaches MaxAutoFixRounds the gate reports OutcomeExhausted so the engine
// stops the run and escalates to the human (design doc §3).
func TestCIGate_ExhaustedAtRoundLimit(t *testing.T) {
	g := NewCIGate()
	c := stubConfig() // MaxAutoFixRounds = 3

	out, err := g.Run(context.Background(), policy.RunContext{
		RunID: "r1", PRID: "pr1", Config: c, Attempt: c.MaxAutoFixRounds,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != policy.OutcomeExhausted {
		t.Fatalf("got %q, want %q", out, policy.OutcomeExhausted)
	}
}

// TestCIGate_ExhaustedWhenMaxRoundsZero covers the defensive branch: a
// project could (legitimately) configure MaxAutoFixRounds=0 to mean
// "never auto-fix", and that should be honored as immediate exhaustion
// rather than a divide-by-zero-style surprise.
func TestCIGate_ExhaustedWhenMaxRoundsZero(t *testing.T) {
	g := NewCIGate()
	c := stubConfig()
	c.MaxAutoFixRounds = 0

	out, err := g.Run(context.Background(), policy.RunContext{
		RunID: "r1", PRID: "pr1", Config: c, Attempt: 0,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != policy.OutcomeExhausted {
		t.Fatalf("got %q, want %q", out, policy.OutcomeExhausted)
	}
}

// TestCIGate_RespectsContextCancellation ensures a cancelled context short-
// circuits the gate rather than running through its decision tree and
// returning a stale outcome.
func TestCIGate_RespectsContextCancellation(t *testing.T) {
	g := NewCIGate()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := g.Run(ctx, policy.RunContext{
		RunID: "r1", PRID: "pr1", Config: stubConfig(),
	})
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("error %q does not mention context cancellation", err)
	}
}
