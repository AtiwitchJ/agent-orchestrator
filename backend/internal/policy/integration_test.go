//go:build integration

// Package policy_test exercises the production Engine (internal/policy's
// engineImpl, via NewEngine) end to end against a real Store implementation
// and the gate implementations in internal/policy/gates — CIGate graduated to
// a fake SCM checker, HumanGate graduated to a fake notifier/park, ReviewGate
// and FinalGate still Phase 1 stubs (see docs/superpowers/plans/
// 2026-07-14-hybrid-approval-gates.md's gate graduation order: CI → Human →
// Review → Final, of which this session completed CI and Human).
package policy_test

import (
	"context"
	"testing"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/policy"
	"github.com/modernagent/modern-agent/backend/internal/policy/gates"
)

// memStore is a minimal in-memory policy.Store fake, standing in for the real
// SQLite-backed store (internal/storage/sqlite/store/policy_store.go) so this
// test can exercise the Engine's full public contract without a database.
type memStore struct {
	runs    map[string]policy.Run
	results map[string][]policy.GateResult
}

func newMemStore() *memStore {
	return &memStore{runs: map[string]policy.Run{}, results: map[string][]policy.GateResult{}}
}

func (m *memStore) CreateRun(_ context.Context, run policy.Run) error {
	m.runs[run.ID] = run
	return nil
}

func (m *memStore) GetRun(_ context.Context, runID string) (policy.Run, bool, error) {
	r, ok := m.runs[runID]
	return r, ok, nil
}

func (m *memStore) ListActiveRuns(_ context.Context) ([]policy.Run, error) {
	var out []policy.Run
	for _, r := range m.runs {
		if r.FinalState == "" {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *memStore) UpdateCurrentGate(_ context.Context, runID string, gate policy.GateID, updatedAt time.Time) error {
	r := m.runs[runID]
	r.CurrentGate = gate
	r.UpdatedAt = updatedAt
	m.runs[runID] = r
	return nil
}

func (m *memStore) FinalizeRun(_ context.Context, runID, finalState string, updatedAt time.Time) error {
	r := m.runs[runID]
	r.FinalState = finalState
	r.UpdatedAt = updatedAt
	m.runs[runID] = r
	return nil
}

func (m *memStore) RecordGateResult(_ context.Context, result policy.GateResult) error {
	m.results[result.RunID] = append(m.results[result.RunID], result)
	return nil
}

func (m *memStore) ListGateResults(_ context.Context, runID string) ([]policy.GateResult, error) {
	return append([]policy.GateResult(nil), m.results[runID]...), nil
}

// fakeSCM stands in for the real SCM observer CIGate consults through its
// PRChecker seam (gates.NewCIGateWithChecker) — ChecksGreen models "the fake
// SCM already reports this PR's checks passing."
type fakeSCM struct {
	ChecksGreen bool
}

func (s *fakeSCM) Check(_ context.Context, _, _ string) (gates.PRCIState, error) {
	if s.ChecksGreen {
		return gates.PRCIPassing, nil
	}
	return gates.PRCIFailing, nil
}

// fakeAgent stands in for the real notification/reviewer infrastructure:
// it backs HumanGate's NotifyFunc so the test can assert the needs_input
// notification actually fired when the run parked.
type fakeAgent struct {
	notified []gates.NotifyIntent
}

func (a *fakeAgent) Notify(_ context.Context, intent gates.NotifyIntent) error {
	a.notified = append(a.notified, intent)
	return nil
}

// TestIntegration_HappyPathPassesAllFourGates drives a real Engine (backed by
// an in-memory Store) through CI (real checker, green) → Review (stub,
// disabled) → Human (real park + notify, then human approves) → Final (stub,
// passes on first attempt) and asserts the run reaches FinalStateMerged with
// one GateResult per gate in order, plus the human notification firing
// exactly once.
func TestIntegration_HappyPathPassesAllFourGates(t *testing.T) {
	ctx := context.Background()
	scm := &fakeSCM{ChecksGreen: true}
	agent := &fakeAgent{}

	cfg := policy.DefaultPolicyConfig()
	cfg.AutoFixOnCIFailure = true // CIGate's real checker path only engages when this is true.
	// ReviewGate stays a Phase 1 stub in this session (see gate graduation
	// order); disable it so the happy path doesn't depend on unimplemented
	// agent-review behavior.
	cfg.RequireAgentReview = false
	cfg.RequireHumanApproval = true
	cfg.AgentFinalPass = true

	store := newMemStore()
	run := policy.Run{
		ID: "run-1", ProjectID: "proj-1", SessionID: "sess-1", PRID: "pr-1",
		Config: cfg, CurrentGate: policy.GateCI, StartedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := store.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	engine := policy.NewEngine(store, []policy.Gate{
		gates.NewCIGateWithChecker(scm.Check),
		gates.NewReviewGate(cfg.ReviewStrategy),
		gates.NewHumanGate(agent.Notify),
		gates.NewFinalGate(),
	})

	if err := engine.Run(ctx, "run-1"); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	got, err := engine.GetRun(ctx, "run-1")
	if err != nil {
		t.Fatalf("GetRun() = %v, want nil", err)
	}
	if got.CurrentGate != policy.GateHuman || got.FinalState != "" {
		t.Fatalf("after first Run(): got = %+v, want parked at human, not terminal", got)
	}
	if len(agent.notified) != 1 {
		t.Fatalf("agent.notified = %d entries, want 1 (from HumanGate parking)", len(agent.notified))
	}
	if agent.notified[0].Kind != "needs_input" || agent.notified[0].RunID != "run-1" {
		t.Errorf("notified = %+v, want kind=needs_input runId=run-1", agent.notified[0])
	}

	if err := engine.Decide(ctx, "run-1", policy.Decision{Action: policy.DecisionApprove}); err != nil {
		t.Fatalf("Decide(approve) = %v, want nil", err)
	}

	got, err = engine.GetRun(ctx, "run-1")
	if err != nil {
		t.Fatalf("GetRun() = %v, want nil", err)
	}
	if got.FinalState != policy.FinalStateMerged {
		t.Fatalf("FinalState = %q, want %q", got.FinalState, policy.FinalStateMerged)
	}
	if got.CurrentGate != "" {
		t.Errorf("CurrentGate = %q, want empty (terminal)", got.CurrentGate)
	}

	wantOrder := []policy.GateID{policy.GateCI, policy.GateReview, policy.GateHuman, policy.GateHuman, policy.GateFinal}
	if len(got.GateHistory) != len(wantOrder) {
		t.Fatalf("GateHistory = %d entries, want %d: %+v", len(got.GateHistory), len(wantOrder), got.GateHistory)
	}
	for i, want := range wantOrder {
		if got.GateHistory[i].GateID != want {
			t.Errorf("GateHistory[%d].GateID = %s, want %s", i, got.GateHistory[i].GateID, want)
		}
	}
	if got.GateHistory[2].Outcome != policy.OutcomeParked {
		t.Errorf("GateHistory[2] (human park) outcome = %s, want parked", got.GateHistory[2].Outcome)
	}
	if got.GateHistory[3].Outcome != policy.OutcomePass {
		t.Errorf("GateHistory[3] (human approve) outcome = %s, want pass", got.GateHistory[3].Outcome)
	}
	for i, r := range got.GateHistory {
		if r.Outcome != policy.OutcomePass && r.Outcome != policy.OutcomeParked {
			t.Errorf("GateHistory[%d] = %+v, want pass or parked (happy path)", i, r)
		}
	}
}
