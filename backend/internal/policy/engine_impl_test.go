package policy

import (
	"context"
	"errors"
	"testing"
	"time"
)

// memStore is a minimal in-memory Store fake for engineImpl unit tests.
type memStore struct {
	runs    map[string]Run
	results map[string][]GateResult
}

func newMemStore() *memStore {
	return &memStore{runs: map[string]Run{}, results: map[string][]GateResult{}}
}

func (m *memStore) CreateRun(_ context.Context, run Run) error {
	m.runs[run.ID] = run
	return nil
}

func (m *memStore) GetRun(_ context.Context, runID string) (Run, bool, error) {
	r, ok := m.runs[runID]
	return r, ok, nil
}

func (m *memStore) ListActiveRuns(_ context.Context) ([]Run, error) {
	var out []Run
	for _, r := range m.runs {
		if r.FinalState == "" {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *memStore) UpdateCurrentGate(_ context.Context, runID string, gate GateID, updatedAt time.Time) error {
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

func (m *memStore) RecordGateResult(_ context.Context, result GateResult) error {
	m.results[result.RunID] = append(m.results[result.RunID], result)
	return nil
}

func (m *memStore) ListGateResults(_ context.Context, runID string) ([]GateResult, error) {
	return append([]GateResult(nil), m.results[runID]...), nil
}

// scriptedGate returns outcomes from a fixed slice, indexed by call count
// (clamped to the last entry once exhausted), so a test can script a
// fail/fail/pass sequence for one gate. Also records every RunContext it saw.
type scriptedGate struct {
	id       GateID
	outcomes []GateOutcome
	calls    int
	seen     []RunContext
}

func (g *scriptedGate) ID() GateID { return g.id }

func (g *scriptedGate) Run(_ context.Context, rc RunContext) (GateOutcome, error) {
	g.seen = append(g.seen, rc)
	idx := g.calls
	if idx >= len(g.outcomes) {
		idx = len(g.outcomes) - 1
	}
	g.calls++
	return g.outcomes[idx], nil
}

func passGate(id GateID) *scriptedGate {
	return &scriptedGate{id: id, outcomes: []GateOutcome{OutcomePass}}
}

func newRun(id string) Run {
	now := time.Now().UTC()
	return Run{
		ID: id, ProjectID: "proj-1", SessionID: "sess-1", PRID: "pr-1",
		Config: DefaultPolicyConfig(), CurrentGate: GateCI, StartedAt: now, UpdatedAt: now,
	}
}

func newTestEngine(store Store, gates ...*scriptedGate) *engineImpl {
	impl := make([]Gate, len(gates))
	for i, g := range gates {
		impl[i] = g
	}
	return NewEngine(store, impl).(*engineImpl)
}

func TestEngineImpl_HappyPathPassesAllFourGates(t *testing.T) {
	store := newMemStore()
	run := newRun("run-1")
	if err := store.CreateRun(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	e := newTestEngine(store, passGate(GateCI), passGate(GateReview), passGate(GateHuman), passGate(GateFinal))

	if err := e.Run(context.Background(), "run-1"); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	got, err := e.GetRun(context.Background(), "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.FinalState != FinalStateMerged {
		t.Errorf("FinalState = %q, want %q", got.FinalState, FinalStateMerged)
	}
	if got.CurrentGate != "" {
		t.Errorf("CurrentGate = %q, want empty (terminal)", got.CurrentGate)
	}
	if len(got.GateHistory) != 4 {
		t.Fatalf("GateHistory = %d entries, want 4: %+v", len(got.GateHistory), got.GateHistory)
	}
	wantOrder := []GateID{GateCI, GateReview, GateHuman, GateFinal}
	for i, want := range wantOrder {
		if got.GateHistory[i].GateID != want || got.GateHistory[i].Outcome != OutcomePass {
			t.Errorf("GateHistory[%d] = %+v, want gate=%s outcome=pass", i, got.GateHistory[i], want)
		}
		if got.GateHistory[i].Attempt != 1 {
			t.Errorf("GateHistory[%d].Attempt = %d, want 1", i, got.GateHistory[i].Attempt)
		}
	}
}

func TestEngineImpl_CIRetriesThenPasses(t *testing.T) {
	store := newMemStore()
	if err := store.CreateRun(context.Background(), newRun("run-1")); err != nil {
		t.Fatal(err)
	}
	ci := &scriptedGate{id: GateCI, outcomes: []GateOutcome{OutcomeFail, OutcomeFail, OutcomePass}}
	e := newTestEngine(store, ci, passGate(GateReview), passGate(GateHuman), passGate(GateFinal))

	if err := e.Run(context.Background(), "run-1"); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	if ci.calls != 3 {
		t.Fatalf("CIGate called %d times, want 3", ci.calls)
	}
	// 0-indexed Attempt fed to the gate: 0, 1, 2.
	for i, rc := range ci.seen {
		if rc.Attempt != i {
			t.Errorf("ci.seen[%d].Attempt = %d, want %d", i, rc.Attempt, i)
		}
	}
	got, err := e.GetRun(context.Background(), "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.FinalState != FinalStateMerged {
		t.Errorf("FinalState = %q, want merged", got.FinalState)
	}
	// Persisted Attempt is 1-indexed: 1, 2, 3.
	ciResults := got.GateHistory[:3]
	for i, r := range ciResults {
		if r.GateID != GateCI || r.Attempt != i+1 {
			t.Errorf("GateHistory[%d] = %+v, want gate=ci attempt=%d", i, r, i+1)
		}
	}
}

func TestEngineImpl_CIExhaustedStopsRun(t *testing.T) {
	store := newMemStore()
	run := newRun("run-1")
	run.Config.MaxAutoFixRounds = 2
	if err := store.CreateRun(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	ci := &scriptedGate{id: GateCI, outcomes: []GateOutcome{OutcomeFail, OutcomeExhausted}}
	e := newTestEngine(store, ci, passGate(GateReview), passGate(GateHuman), passGate(GateFinal))

	if err := e.Run(context.Background(), "run-1"); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	got, err := e.GetRun(context.Background(), "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.FinalState != FinalStateExhausted {
		t.Errorf("FinalState = %q, want exhausted", got.FinalState)
	}
	if got.CurrentGate != "" {
		t.Errorf("CurrentGate = %q, want empty", got.CurrentGate)
	}
}

func TestEngineImpl_HumanGateParksThenApprove(t *testing.T) {
	store := newMemStore()
	if err := store.CreateRun(context.Background(), newRun("run-1")); err != nil {
		t.Fatal(err)
	}
	human := &scriptedGate{id: GateHuman, outcomes: []GateOutcome{OutcomeParked}}
	e := newTestEngine(store, passGate(GateCI), passGate(GateReview), human, passGate(GateFinal))

	if err := e.Run(context.Background(), "run-1"); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	got, err := e.GetRun(context.Background(), "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.CurrentGate != GateHuman || got.FinalState != "" {
		t.Fatalf("got = %+v, want parked at human, not terminal", got)
	}

	// A second Run() call while parked must be a no-op (idempotent) and must
	// not re-invoke the gate (no duplicate notification).
	if err := e.Run(context.Background(), "run-1"); err != nil {
		t.Fatalf("Run() while parked = %v, want nil", err)
	}
	if human.calls != 1 {
		t.Fatalf("HumanGate called %d times across two Run() calls, want 1", human.calls)
	}

	if err := e.Decide(context.Background(), "run-1", Decision{Action: DecisionApprove}); err != nil {
		t.Fatalf("Decide(approve) = %v, want nil", err)
	}
	got, err = e.GetRun(context.Background(), "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.FinalState != FinalStateMerged {
		t.Errorf("FinalState = %q, want merged", got.FinalState)
	}
}

func TestEngineImpl_HumanRequestChangesRestartsAtReview(t *testing.T) {
	store := newMemStore()
	if err := store.CreateRun(context.Background(), newRun("run-1")); err != nil {
		t.Fatal(err)
	}
	human := &scriptedGate{id: GateHuman, outcomes: []GateOutcome{OutcomeParked}}
	review := passGate(GateReview)
	e := newTestEngine(store, passGate(GateCI), review, human, passGate(GateFinal))

	if err := e.Run(context.Background(), "run-1"); err != nil {
		t.Fatal(err)
	}
	if err := e.Decide(context.Background(), "run-1", Decision{Action: DecisionRequestChanges, Justification: "needs tests"}); err != nil {
		t.Fatalf("Decide(request_changes) = %v, want nil", err)
	}

	got, err := e.GetRun(context.Background(), "run-1")
	if err != nil {
		t.Fatal(err)
	}
	// request_changes restarts at GateReview, then ReviewGate passes and the
	// run parks at GateHuman again (second call).
	if got.CurrentGate != GateHuman {
		t.Fatalf("CurrentGate = %q, want human (parked again after review re-pass)", got.CurrentGate)
	}
	if review.calls != 2 {
		t.Errorf("ReviewGate called %d times, want 2 (initial pass + after request_changes)", review.calls)
	}
	if human.calls != 2 {
		t.Errorf("HumanGate called %d times, want 2 (parked, then parked again)", human.calls)
	}

	// Find the request_changes GateResult and check it landed on GateHuman
	// with OutcomeFail and the justification in Reason.
	var found bool
	for _, r := range got.GateHistory {
		if r.GateID == GateHuman && r.Outcome == OutcomeFail {
			found = true
			if r.Reason != "human requested changes: needs tests" {
				t.Errorf("Reason = %q, want to include justification", r.Reason)
			}
		}
	}
	if !found {
		t.Errorf("no GateHuman/Fail entry in history: %+v", got.GateHistory)
	}
}

func TestEngineImpl_DecideOverrideAdvances(t *testing.T) {
	store := newMemStore()
	if err := store.CreateRun(context.Background(), newRun("run-1")); err != nil {
		t.Fatal(err)
	}
	final := &scriptedGate{id: GateFinal, outcomes: []GateOutcome{OutcomeParked}}
	e := newTestEngine(store, passGate(GateCI), passGate(GateReview), passGate(GateHuman), final)

	if err := e.Run(context.Background(), "run-1"); err != nil {
		t.Fatal(err)
	}
	got, _ := e.GetRun(context.Background(), "run-1")
	if got.CurrentGate != GateFinal {
		t.Fatalf("CurrentGate = %q, want final (parked)", got.CurrentGate)
	}

	if err := e.Decide(context.Background(), "run-1", Decision{Action: DecisionOverride, Justification: "acceptable risk"}); err != nil {
		t.Fatalf("Decide(override) = %v, want nil", err)
	}
	got, err := e.GetRun(context.Background(), "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.FinalState != FinalStateMerged {
		t.Errorf("FinalState = %q, want merged", got.FinalState)
	}
	last := got.GateHistory[len(got.GateHistory)-1]
	if last.Outcome != OutcomeOverridden || last.Justification != "acceptable risk" {
		t.Errorf("last result = %+v, want overridden with justification", last)
	}
}

func TestEngineImpl_DecideOverrideWithoutJustificationIsInvalid(t *testing.T) {
	store := newMemStore()
	if err := store.CreateRun(context.Background(), newRun("run-1")); err != nil {
		t.Fatal(err)
	}
	final := &scriptedGate{id: GateFinal, outcomes: []GateOutcome{OutcomeParked}}
	e := newTestEngine(store, passGate(GateCI), passGate(GateReview), passGate(GateHuman), final)
	if err := e.Run(context.Background(), "run-1"); err != nil {
		t.Fatal(err)
	}
	err := e.Decide(context.Background(), "run-1", Decision{Action: DecisionOverride})
	if !errors.Is(err, ErrInvalidDecision) {
		t.Fatalf("Decide(override, no justification) = %v, want ErrInvalidDecision", err)
	}
}

func TestEngineImpl_RunUnknownID(t *testing.T) {
	e := newTestEngine(newMemStore(), passGate(GateCI), passGate(GateReview), passGate(GateHuman), passGate(GateFinal))
	if err := e.Run(context.Background(), "missing"); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("Run(missing) = %v, want ErrRunNotFound", err)
	}
	if _, err := e.GetRun(context.Background(), "missing"); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("GetRun(missing) = %v, want ErrRunNotFound", err)
	}
}

func TestEngineImpl_RunAlreadyTerminal(t *testing.T) {
	store := newMemStore()
	if err := store.CreateRun(context.Background(), newRun("run-1")); err != nil {
		t.Fatal(err)
	}
	e := newTestEngine(store, passGate(GateCI), passGate(GateReview), passGate(GateHuman), passGate(GateFinal))
	if err := e.Run(context.Background(), "run-1"); err != nil {
		t.Fatal(err)
	}
	if err := e.Run(context.Background(), "run-1"); !errors.Is(err, ErrAlreadyTerminal) {
		t.Fatalf("Run() on merged run = %v, want ErrAlreadyTerminal", err)
	}
}

func TestEngineImpl_DecideOnTerminalRunIsAlreadyTerminal(t *testing.T) {
	store := newMemStore()
	if err := store.CreateRun(context.Background(), newRun("run-1")); err != nil {
		t.Fatal(err)
	}
	e := newTestEngine(store, passGate(GateCI), passGate(GateReview), passGate(GateHuman), passGate(GateFinal))
	if err := e.Run(context.Background(), "run-1"); err != nil {
		t.Fatal(err)
	}
	err := e.Decide(context.Background(), "run-1", Decision{Action: DecisionApprove})
	if !errors.Is(err, ErrAlreadyTerminal) {
		t.Fatalf("Decide() on a merged (terminal) run = %v, want ErrAlreadyTerminal", err)
	}
}

// TestEngineImpl_DecideWhenNotParked simulates a run that is in flight but
// not currently parked (e.g. a crash between recording a Pass and the engine
// continuing to the next gate — boot-time recovery of such runs is out of
// scope for this increment) by writing history directly against the store,
// bypassing Run/Decide.
func TestEngineImpl_DecideWhenNotParked(t *testing.T) {
	store := newMemStore()
	if err := store.CreateRun(context.Background(), newRun("run-1")); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordGateResult(context.Background(), GateResult{RunID: "run-1", GateID: GateCI, Attempt: 1, Outcome: OutcomePass}); err != nil {
		t.Fatal(err)
	}
	e := newTestEngine(store, passGate(GateCI), passGate(GateReview), passGate(GateHuman), passGate(GateFinal))
	err := e.Decide(context.Background(), "run-1", Decision{Action: DecisionApprove})
	if !errors.Is(err, ErrRunNotParked) {
		t.Fatalf("Decide() on a non-parked in-flight run = %v, want ErrRunNotParked", err)
	}
}
