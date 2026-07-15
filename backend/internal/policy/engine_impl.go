package policy

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// engineImpl is the production Engine: it drives the Gate implementations
// registered at construction against a durable Store. CDC is emitted for free
// by the DB triggers already wired on policy_runs/gate_results (migration
// 0029) — engineImpl only ever performs plain Store writes, never a
// parallel Go-side event emission (AGENTS.md hard rule).
type engineImpl struct {
	store Store
	gates map[GateID]Gate
	locks keyedMutex
}

// NewEngine returns a production Engine backed by store and driving gates.
// gates must cover every GateID in GateOrder; Run/Decide return an error if a
// gate is dispatched for an id with no registered implementation.
func NewEngine(store Store, gates []Gate) Engine {
	m := make(map[GateID]Gate, len(gates))
	for _, g := range gates {
		m[g.ID()] = g
	}
	return &engineImpl{store: store, gates: m}
}

// Run starts or resumes runID. See the Engine interface doc for the full
// contract; in short: idempotent, ErrRunNotFound for an unknown id,
// ErrAlreadyTerminal for a finished run, and a silent no-op while the run is
// parked waiting on a human Decision.
func (e *engineImpl) Run(ctx context.Context, runID string) error {
	unlock := e.locks.Lock(runID)
	defer unlock()

	run, ok, err := e.store.GetRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("policy: get run %s: %w", runID, err)
	}
	if !ok {
		return ErrRunNotFound
	}
	history, err := e.store.ListGateResults(ctx, runID)
	if err != nil {
		return fmt.Errorf("policy: list gate results for run %s: %w", runID, err)
	}
	current, finalState, priorAttempts := deriveState(history)
	if finalState != "" {
		return ErrAlreadyTerminal
	}
	if isParked(history, current) {
		return nil // waiting for Decide — Run is a no-op until then.
	}
	return e.runLoop(ctx, run, current, priorAttempts)
}

// Decide forwards a human Decision to the run currently parked at a gate. It
// returns ErrRunNotFound / ErrInvalidDecision / ErrAlreadyTerminal per the
// Engine interface doc, plus ErrRunNotParked when the run isn't actually
// waiting on a decision right now.
func (e *engineImpl) Decide(ctx context.Context, runID string, decision Decision) error {
	if err := decision.Validate(); err != nil {
		return ErrInvalidDecision
	}
	unlock := e.locks.Lock(runID)
	defer unlock()

	run, ok, err := e.store.GetRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("policy: get run %s: %w", runID, err)
	}
	if !ok {
		return ErrRunNotFound
	}
	history, err := e.store.ListGateResults(ctx, runID)
	if err != nil {
		return fmt.Errorf("policy: list gate results for run %s: %w", runID, err)
	}
	current, finalState, priorAttempts := deriveState(history)
	if finalState != "" {
		return ErrAlreadyTerminal
	}
	if !isParked(history, current) {
		return ErrRunNotParked
	}

	attempt := priorAttempts + 1
	now := time.Now().UTC()
	switch decision.Action {
	case DecisionApprove:
		if err := e.store.RecordGateResult(ctx, GateResult{RunID: runID, GateID: current, Attempt: attempt, Outcome: OutcomePass}); err != nil {
			return fmt.Errorf("policy: record approve decision for run %s: %w", runID, err)
		}
		return e.advanceOrFinish(ctx, run, current, now)
	case DecisionRequestChanges:
		if err := e.store.RecordGateResult(ctx, GateResult{
			RunID: runID, GateID: current, Attempt: attempt, Outcome: OutcomeFail,
			Reason: "human requested changes: " + decision.Justification,
		}); err != nil {
			return fmt.Errorf("policy: record request_changes decision for run %s: %w", runID, err)
		}
		// Design doc §2 decision 1: restart at Gate 2 (GateReview), not the
		// generic NextGate — CI already passed, no need to re-run it.
		if err := e.store.UpdateCurrentGate(ctx, runID, GateReview, now); err != nil {
			return fmt.Errorf("policy: update current gate for run %s: %w", runID, err)
		}
		return e.runLoop(ctx, run, GateReview, 0)
	case DecisionOverride:
		if err := e.store.RecordGateResult(ctx, GateResult{
			RunID: runID, GateID: current, Attempt: attempt, Outcome: OutcomeOverridden,
			Justification: decision.Justification,
		}); err != nil {
			return fmt.Errorf("policy: record override decision for run %s: %w", runID, err)
		}
		return e.advanceOrFinish(ctx, run, current, now)
	default:
		return ErrInvalidDecision
	}
}

// advanceOrFinish moves the run to NextGate(current), finalizing it as merged
// when current was the last gate, then resumes driving from there.
func (e *engineImpl) advanceOrFinish(ctx context.Context, run Run, current GateID, now time.Time) error {
	next := NextGate(current)
	if next == "" {
		if err := e.store.FinalizeRun(ctx, run.ID, FinalStateMerged, now); err != nil {
			return fmt.Errorf("policy: finalize run %s: %w", run.ID, err)
		}
		return nil
	}
	if err := e.store.UpdateCurrentGate(ctx, run.ID, next, now); err != nil {
		return fmt.Errorf("policy: update current gate for run %s: %w", run.ID, err)
	}
	return e.runLoop(ctx, run, next, 0)
}

// GetRun returns the durable record for runID with CurrentGate/FinalState
// always derived fresh from GateHistory (never trusted from the persisted
// cache columns) per AGENTS.md's "status is derived, never stored" rule.
func (e *engineImpl) GetRun(ctx context.Context, runID string) (Run, error) {
	run, ok, err := e.store.GetRun(ctx, runID)
	if err != nil {
		return Run{}, fmt.Errorf("policy: get run %s: %w", runID, err)
	}
	if !ok {
		return Run{}, ErrRunNotFound
	}
	history, err := e.store.ListGateResults(ctx, runID)
	if err != nil {
		return Run{}, fmt.Errorf("policy: list gate results for run %s: %w", runID, err)
	}
	current, finalState, _ := deriveState(history)
	run.CurrentGate = current
	run.FinalState = finalState
	run.GateHistory = history
	return run, nil
}

// runLoop dispatches gates starting at (current, priorAttempts) until the run
// advances past GateFinal, reaches a terminal outcome, or parks waiting for a
// Decision. Each iteration is exactly one Gate.Run call plus the persistence
// implied by its outcome; OutcomeOverridden is never produced here (only
// Decide records it) since no gate returns it directly from Run.
func (e *engineImpl) runLoop(ctx context.Context, run Run, current GateID, priorAttempts int) error {
	for {
		gate, ok := e.gates[current]
		if !ok {
			return fmt.Errorf("policy: no gate implementation registered for %s", current)
		}
		rc := RunContext{
			RunID: run.ID, ProjectID: run.ProjectID, SessionID: run.SessionID,
			PRID: run.PRID, Config: run.Config, Attempt: priorAttempts,
		}
		start := time.Now()
		outcome, err := gate.Run(ctx, rc)
		if err != nil {
			return fmt.Errorf("policy: gate %s run: %w", current, err)
		}
		elapsed := time.Since(start)
		now := time.Now().UTC()
		attempt := priorAttempts + 1

		if outcome == OutcomeParked {
			if err := e.store.RecordGateResult(ctx, GateResult{RunID: run.ID, GateID: current, Attempt: attempt, Outcome: OutcomeParked, Duration: elapsed}); err != nil {
				return fmt.Errorf("policy: record parked result for run %s gate %s: %w", run.ID, current, err)
			}
			return nil
		}
		if err := e.store.RecordGateResult(ctx, GateResult{RunID: run.ID, GateID: current, Attempt: attempt, Outcome: outcome, Duration: elapsed}); err != nil {
			return fmt.Errorf("policy: record gate result for run %s gate %s: %w", run.ID, current, err)
		}

		switch {
		case outcome == OutcomePass:
			next := NextGate(current)
			if next == "" {
				if err := e.store.FinalizeRun(ctx, run.ID, FinalStateMerged, now); err != nil {
					return fmt.Errorf("policy: finalize run %s: %w", run.ID, err)
				}
				return nil
			}
			if err := e.store.UpdateCurrentGate(ctx, run.ID, next, now); err != nil {
				return fmt.Errorf("policy: update current gate for run %s: %w", run.ID, err)
			}
			current, priorAttempts = next, 0
		case outcome == OutcomeExhausted:
			if err := e.store.FinalizeRun(ctx, run.ID, FinalStateExhausted, now); err != nil {
				return fmt.Errorf("policy: finalize run %s: %w", run.ID, err)
			}
			return nil
		case IsRetryable(outcome):
			priorAttempts = attempt
		default:
			return fmt.Errorf("policy: gate %s returned unexpected outcome %q", current, outcome)
		}
	}
}

// isParked reports whether the last recorded attempt for the current gate is
// OutcomeParked — i.e. the run is waiting for a Decide call, not for Run to
// drive it further.
func isParked(history []GateResult, current GateID) bool {
	if len(history) == 0 {
		return false
	}
	last := history[len(history)-1]
	return last.GateID == current && last.Outcome == OutcomeParked
}

// deriveState replays gateHistory (oldest first) and returns the gate the
// engine should dispatch next (empty once terminal), the FinalState (empty
// while in flight), and the number of prior attempts already recorded at the
// returned gate — the value the next RunContext.Attempt should carry.
//
// This is the single source of truth for "where is this run" — both Run and
// GetRun call it rather than trusting the persisted current_gate/final_state
// columns, which exist only as a query-ability cache (idx_policy_runs_state)
// per AGENTS.md's "status is derived, never stored" rule.
func deriveState(history []GateResult) (current GateID, finalState string, priorAttempts int) {
	current = GateOrder[0]
	for _, r := range history {
		switch r.Outcome {
		case OutcomePass:
			current = NextGate(current)
			priorAttempts = 0
			if current == "" {
				finalState = FinalStateMerged
			}
		case OutcomeFail:
			if r.GateID == GateHuman {
				// Design doc §2 decision 1: human "request_changes" restarts
				// at GateReview, not a retry of GateHuman itself.
				current = GateReview
				priorAttempts = 0
			} else {
				priorAttempts = r.Attempt
			}
		case OutcomeExhausted:
			current = ""
			finalState = FinalStateExhausted
		case OutcomeOverridden:
			current = NextGate(current)
			priorAttempts = 0
			if current == "" {
				finalState = FinalStateMerged
			}
		case OutcomeParked:
			current = r.GateID
		}
	}
	return current, finalState, priorAttempts
}

// keyedMutex serializes Run/Decide calls per runID so two concurrent calls
// against the same run can't interleave their read-modify-write sequence.
// Locks are created lazily and never removed — policy runs are few enough
// (one per tracker-spawned PR) that this doesn't leak meaningfully within a
// daemon process lifetime.
type keyedMutex struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// Lock blocks until key's mutex is held and returns the unlock function.
func (k *keyedMutex) Lock(key string) func() {
	k.mu.Lock()
	if k.locks == nil {
		k.locks = make(map[string]*sync.Mutex)
	}
	l, ok := k.locks[key]
	if !ok {
		l = &sync.Mutex{}
		k.locks[key] = l
	}
	k.mu.Unlock()
	l.Lock()
	return l.Unlock
}
