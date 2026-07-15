package policy

import (
	"context"
	"time"
)

// Store is the durable persistence a production Engine drives. Every method
// is a plain read/write against policy_runs / gate_results (migration 0029);
// CDC comes for free from the DB triggers already wired on those tables, so
// Store implementations must not add a parallel Go-side emission path (see
// AGENTS.md's "SQLite change events come from DB triggers" hard rule).
//
// Store never derives CurrentGate/FinalState from GateHistory itself — the
// policy_runs columns it reads/writes are a query-ability cache (see
// idx_policy_runs_state), not the source of truth. The Engine is responsible
// for deriving the authoritative CurrentGate/FinalState from GateHistory at
// read time (AGENTS.md: "do not store derived/display status").
type Store interface {
	// CreateRun persists a new run row at its initial gate. Callers must set
	// run.CurrentGate to GateOrder[0] and leave run.FinalState empty.
	CreateRun(ctx context.Context, run Run) error
	// GetRun returns the run row (GateHistory left nil — see ListGateResults),
	// ok=false when runID is unknown.
	GetRun(ctx context.Context, runID string) (Run, bool, error)
	// ListActiveRuns returns every run with an empty FinalState, oldest
	// updated first. Used for boot-time recovery of in-flight runs.
	ListActiveRuns(ctx context.Context) ([]Run, error)
	// UpdateCurrentGate advances the persisted current-gate cache column.
	UpdateCurrentGate(ctx context.Context, runID string, gate GateID, updatedAt time.Time) error
	// FinalizeRun sets the terminal state cache column. finalState must be
	// one of the FinalState* constants.
	FinalizeRun(ctx context.Context, runID, finalState string, updatedAt time.Time) error
	// RecordGateResult appends one gate-attempt row. Attempt must be
	// 1-indexed (the first attempt at a gate is Attempt=1).
	RecordGateResult(ctx context.Context, result GateResult) error
	// ListGateResults returns every attempt for a run, oldest first
	// (matches insertion order, which is what the Engine replays to derive
	// CurrentGate/FinalState).
	ListGateResults(ctx context.Context, runID string) ([]GateResult, error)
}

// FinalState values a run's FinalState field can hold once terminal. Mirrors
// the vocabulary documented on the Run.FinalState field (engine.go).
const (
	FinalStateMerged     = "merged"
	FinalStateSuperseded = "superseded"
	FinalStateExhausted  = "exhausted"
	FinalStateVetoed     = "vetoed"
	FinalStateErrored    = "errored"
)
