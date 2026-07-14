//go:build integration

// Package policy_test exercises the Phase 1 gate stubs (internal/policy/gates)
// end to end through the sequencing helpers in internal/policy/state.go.
//
// There is no concrete, SQLite-backed policy.Engine implementation yet — only
// the Engine interface (engine.go) and a fakeEngine used by engine_test.go.
// The production Engine (design doc §3 state machine, driven by the Store and
// the SCM observer) is a separate, not-yet-landed unit of work. This test
// therefore drives the four Gate implementations directly with a hand-rolled
// loop that mirrors what that Engine will do (walk policy.GateOrder, record a
// policy.GateResult per attempt, advance via policy.NextGate), rather than
// exercising a real Engine.
package policy_test

import (
	"context"
	"testing"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/policy"
	"github.com/modernagent/modern-agent/backend/internal/policy/gates"
)

// fakeSCM stands in for the Phase 2 SCM observer CIGate will consult once the
// "TODO: implement — auto-fix triggers `ao hooks` activity dispatch" work
// lands (see gates/ci.go). Phase 1's CIGate stub never reads it — the field
// exists so this fixture already has the shape Phase 2 will need.
type fakeSCM struct {
	ChecksGreen bool
}

// fakeAgent stands in for the Phase 2 reviewer adapter spawn ReviewGate will
// make (see gates/review.go) and backs the one real injectable dependency
// among the four Phase 1 gates: HumanGate's NotifyFunc.
type fakeAgent struct {
	Approves bool
	notified []gates.NotifyIntent
}

func (a *fakeAgent) Notify(_ context.Context, intent gates.NotifyIntent) error {
	a.notified = append(a.notified, intent)
	return nil
}

// TestIntegration_HappyPathPassesAllFourGates drives CIGate, ReviewGate,
// HumanGate, and FinalGate in policy.GateOrder against a fake SCM/agent pair
// and asserts every gate is visited exactly once, in order, with
// policy.OutcomePass.
//
// Attempt indexing note: RunContext.Attempt is documented as 1-indexed
// ("Attempt=1 is the first try" — gates.go), but HumanGate and FinalGate's
// Phase 1 stubs branch on `rc.Attempt == 0` for "first attempt" (see
// gates/human.go, gates/final.go). This test passes Attempt=0 on the first
// call to match the gates' actual behavior; a spec-compliant Attempt=1 first
// call would make HumanGate and FinalGate fail closed immediately. That
// mismatch is a pre-existing inconsistency in the Phase 1 stubs, not
// something this test fixes — flagged here for whoever lands the Phase 2
// Engine and has to reconcile the two.
func TestIntegration_HappyPathPassesAllFourGates(t *testing.T) {
	// Not yet consulted by the Phase 1 CIGate stub; see doc comment on fakeSCM.
	scm := &fakeSCM{}
	agent := &fakeAgent{Approves: true}
	_ = scm

	cfg := policy.DefaultPolicyConfig()
	// CIGate and ReviewGate have no "checks are green" / "agent approved"
	// signal to consult in Phase 1 — they can only ever return
	// OutcomePass by way of the opt-out flags below, which is the closest
	// current stand-in for "the fake SCM/agent already said yes".
	cfg.AutoFixOnCIFailure = false
	cfg.RequireAgentReview = false
	cfg.RequireHumanApproval = true
	cfg.AgentFinalPass = true

	gateImpls := map[policy.GateID]policy.Gate{
		policy.GateCI:     gates.NewCIGate(),
		policy.GateReview: gates.NewReviewGate(cfg.ReviewStrategy),
		policy.GateHuman:  gates.NewHumanGate(agent.Notify),
		policy.GateFinal:  gates.NewFinalGate(),
	}

	rc := policy.RunContext{
		RunID:     "run-1",
		ProjectID: "proj-1",
		SessionID: "sess-1",
		PRID:      "pr-1",
		Config:    cfg,
	}

	var order []policy.GateID
	var results []policy.GateResult
	current := policy.GateOrder[0]
	for current != "" {
		gate, ok := gateImpls[current]
		if !ok {
			t.Fatalf("no gate implementation registered for %s", current)
		}
		rc.Attempt = 0
		start := time.Now()
		outcome, err := gate.Run(context.Background(), rc)
		if err != nil {
			t.Fatalf("gate %s: unexpected error: %v", current, err)
		}
		order = append(order, current)
		results = append(results, policy.GateResult{
			RunID:    rc.RunID,
			GateID:   current,
			Attempt:  1,
			Outcome:  outcome,
			Duration: time.Since(start),
		})
		if outcome != policy.OutcomePass {
			t.Fatalf("gate %s: outcome = %s, want pass (happy path)", current, outcome)
		}
		current = policy.NextGate(current)
	}

	wantOrder := policy.GateOrder
	if len(order) != len(wantOrder) {
		t.Fatalf("visited %d gates, want %d: %v", len(order), len(wantOrder), order)
	}
	for i, g := range wantOrder {
		if order[i] != g {
			t.Errorf("order[%d] = %s, want %s (full order: %v)", i, order[i], g, order)
		}
	}

	if len(results) != 4 {
		t.Fatalf("recorded %d gate results, want 4", len(results))
	}
	for _, r := range results {
		if r.Outcome != policy.OutcomePass {
			t.Errorf("gate %s: recorded outcome %s, want pass", r.GateID, r.Outcome)
		}
	}

	// HumanGate is the one gate that exercises the fake agent: it should have
	// emitted exactly one needs_input notification on its (only) attempt.
	if len(agent.notified) != 1 {
		t.Fatalf("agent.notified = %d entries, want 1 (from HumanGate)", len(agent.notified))
	}
	if agent.notified[0].Kind != "needs_input" || agent.notified[0].RunID != "run-1" {
		t.Errorf("notified = %+v, want kind=needs_input runId=run-1", agent.notified[0])
	}
}
