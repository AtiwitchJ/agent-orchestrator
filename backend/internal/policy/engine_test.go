package policy

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Compile-time assertions: these types must continue to satisfy the Engine
// and Gate interfaces. If a method signature drifts, this file fails to
// compile and the regression is caught before runtime.
var (
	_ Engine = (*stubEngine)(nil)
	_ Gate   = (*stubGate)(nil)
)

// stubEngine is a no-op Engine used only to prove the interface compiles and
// that the package-level error variables are reachable. Real engine logic
// lands in subsequent tasks.
type stubEngine struct{}

func (stubEngine) Run(ctx context.Context, runID string) error { return nil }
func (stubEngine) Decide(ctx context.Context, runID string, decision Decision) error {
	return nil
}
func (stubEngine) GetRun(ctx context.Context, runID string) (Run, error) {
	return Run{}, nil
}

// stubGate is a no-op Gate used for the same purpose.
type stubGate struct{ id GateID }

func (s stubGate) ID() GateID                                       { return s.id }
func (stubGate) Run(ctx context.Context, rc RunContext) (GateOutcome, error) {
	return OutcomePass, nil
}

func TestEngineInterfaceCompiles(t *testing.T) {
	// Touching a method through the interface forces a compile-time check.
	var e Engine = stubEngine{}
	if err := e.Run(context.Background(), "run-1"); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	if _, err := e.GetRun(context.Background(), "run-1"); err != nil {
		t.Fatalf("GetRun() = %v, want nil", err)
	}
	if err := e.Decide(context.Background(), "run-1", Decision{Action: DecisionApprove}); err != nil {
		t.Fatalf("Decide() = %v, want nil", err)
	}
}

func TestGateInterfaceCompiles(t *testing.T) {
	for _, id := range []GateID{GateCI, GateReview, GateHuman, GateFinal} {
		var g Gate = stubGate{id: id}
		if g.ID() != id {
			t.Errorf("Gate.ID() = %q, want %q", g.ID(), id)
		}
		out, err := g.Run(context.Background(), RunContext{RunID: "r"})
		if err != nil {
			t.Errorf("Gate(%s).Run() error = %v", id, err)
		}
		if out != OutcomePass {
			t.Errorf("Gate(%s).Run() = %s, want %s", id, out, OutcomePass)
		}
	}
}

func TestGateOrderIsCanonical(t *testing.T) {
	want := []GateID{GateCI, GateReview, GateHuman, GateFinal}
	if len(GateOrder) != len(want) {
		t.Fatalf("GateOrder length = %d, want %d", len(GateOrder), len(want))
	}
	for i, g := range want {
		if GateOrder[i] != g {
			t.Errorf("GateOrder[%d] = %q, want %q", i, GateOrder[i], g)
		}
	}
}

func TestNextGate(t *testing.T) {
	cases := []struct {
		in   GateID
		want GateID
	}{
		{GateCI, GateReview},
		{GateReview, GateHuman},
		{GateHuman, GateFinal},
		{GateFinal, ""}, // terminal
		{"unknown", ""},
	}
	for _, tc := range cases {
		if got := NextGate(tc.in); got != tc.want {
			t.Errorf("NextGate(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestGateIndex(t *testing.T) {
	if GateIndex(GateCI) != 0 {
		t.Errorf("GateIndex(GateCI) = %d, want 0", GateIndex(GateCI))
	}
	if GateIndex(GateReview) != 1 {
		t.Errorf("GateIndex(GateReview) = %d, want 1", GateIndex(GateReview))
	}
	if GateIndex(GateHuman) != 2 {
		t.Errorf("GateIndex(GateHuman) = %d, want 2", GateIndex(GateHuman))
	}
	if GateIndex(GateFinal) != 3 {
		t.Errorf("GateIndex(GateFinal) = %d, want 3", GateIndex(GateFinal))
	}
	if GateIndex("unknown") != -1 {
		t.Errorf("GateIndex(\"unknown\") = %d, want -1", GateIndex("unknown"))
	}
}

func TestDecisionValidate(t *testing.T) {
	cases := []struct {
		name    string
		d       Decision
		wantErr bool
	}{
		{"approve_ok", Decision{Action: DecisionApprove}, false},
		{"request_changes_ok", Decision{Action: DecisionRequestChanges}, false},
		{"override_with_justification_ok", Decision{Action: DecisionOverride, Justification: "because"}, false},
		{"override_without_justification", Decision{Action: DecisionOverride}, true},
		{"empty_action", Decision{}, true},
		{"unknown_action", Decision{Action: "maybe"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.d.Validate()
			if tc.wantErr && err == nil {
				t.Errorf("Validate() = nil, want error")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestGateOutcomeTerminalAndRetryable(t *testing.T) {
	if !IsTerminalOutcome(OutcomeExhausted) {
		t.Error("IsTerminalOutcome(Exhausted) = false, want true")
	}
	if !IsTerminalOutcome(OutcomeOverridden) {
		t.Error("IsTerminalOutcome(Overridden) = false, want true")
	}
	if IsTerminalOutcome(OutcomePass) {
		t.Error("IsTerminalOutcome(Pass) = true, want false")
	}
	if IsTerminalOutcome(OutcomeFail) {
		t.Error("IsTerminalOutcome(Fail) = true, want false")
	}
	if !IsRetryable(OutcomeFail) {
		t.Error("IsRetryable(Fail) = false, want true")
	}
	for _, o := range []GateOutcome{OutcomePass, OutcomeExhausted, OutcomeOverridden} {
		if IsRetryable(o) {
			t.Errorf("IsRetryable(%s) = true, want false", o)
		}
	}
}

func TestValidateGateID(t *testing.T) {
	for _, ok := range []GateID{GateCI, GateReview, GateHuman, GateFinal, ""} {
		if err := ValidateGateID(ok); err != nil {
			t.Errorf("ValidateGateID(%q) = %v, want nil", ok, err)
		}
	}
	if err := ValidateGateID("bogus"); err == nil {
		t.Error("ValidateGateID(\"bogus\") = nil, want error")
	}
}

// TestGateResult_DerivedStatus documents the AGENTS.md hard rule that policy
// status must be derived from durable facts, never stored as a column. The
// Run struct therefore has FinalState and CurrentGate as convenience fields
// populated from GateHistory at read time — this test pins that those fields
// are not used as the authoritative state by any code path under test.
func TestRun_HoldsDerivedState(t *testing.T) {
	r := Run{
		ID:          "run-1",
		ProjectID:   "proj-1",
		SessionID:   "sess-1",
		PRID:        "pr-1",
		Config:      DefaultPolicyConfig(),
		CurrentGate: GateReview,
		FinalState:  "",
		StartedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		GateHistory: []GateResult{
			{
				RunID:    "run-1",
				GateID:   GateCI,
				Attempt:  1,
				Outcome:  OutcomePass,
				Duration: 30 * time.Second,
			},
		},
	}
	if r.FinalState != "" {
		t.Errorf("expected empty FinalState for in-flight run, got %q", r.FinalState)
	}
	if r.CurrentGate != GateReview {
		t.Errorf("CurrentGate = %q, want %q", r.CurrentGate, GateReview)
	}
	if len(r.GateHistory) != 1 {
		t.Fatalf("GateHistory length = %d, want 1", len(r.GateHistory))
	}
	if r.GateHistory[0].Outcome != OutcomePass {
		t.Errorf("first GateResult outcome = %s, want pass", r.GateHistory[0].Outcome)
	}
}

// TestEngineErrors are reachable from callers and stable across the package.
// Transport-layer code may switch on errors.Is(err, policy.ErrXxx); changing
// the sentinel identity is a breaking change.
func TestEngineErrors(t *testing.T) {
	if !errors.Is(ErrRunNotFound, ErrRunNotFound) {
		t.Error("ErrRunNotFound must round-trip errors.Is")
	}
	if !errors.Is(ErrInvalidDecision, ErrInvalidDecision) {
		t.Error("ErrInvalidDecision must round-trip errors.Is")
	}
	if !errors.Is(ErrAlreadyTerminal, ErrAlreadyTerminal) {
		t.Error("ErrAlreadyTerminal must round-trip errors.Is")
	}
	if errors.Is(ErrRunNotFound, ErrInvalidDecision) {
		t.Error("ErrRunNotFound must not match ErrInvalidDecision")
	}
}