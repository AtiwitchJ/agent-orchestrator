package policy

import (
	"context"
	"errors"
	"time"
)

// Decision is the human's response at Gate 3 (approve) or to a hybrid-veto
// prompt at Gate 4. Action drives the engine; Justification is required when
// Action == "override" per design doc §3.1.
type Decision struct {
	// Action is one of:
	//   - "approve": continue the run (advance to next gate, or proceed to
	//     merge after the final gate).
	//   - "request_changes": send the work back; restart at Gate 2 (design
	//     doc §2 decision 1 — CI already passed, no need to re-run).
	//   - "override": at Gate 4 hybrid-veto path, override the second-opinion
	//     agent's vote; Justification is required and will be logged.
	Action string
	// Justification is the human's free-text rationale. Required when
	// Action == "override"; optional otherwise. Persisted verbatim on the
	// resulting GateResult so Phase 2 telemetry can audit override quality.
	Justification string
}

// Decision action constants.
const (
	DecisionApprove        = "approve"
	DecisionRequestChanges = "request_changes"
	DecisionOverride       = "override"
)

// Validate checks that a Decision is well-formed for persistence. It enforces
// the override-justification invariant from design doc §3.1. The transport
// layer (CLI / HTTP) should call this before forwarding to Engine.Decide so
// the engine can assume inputs are already validated.
func (d Decision) Validate() error {
	switch d.Action {
	case DecisionApprove, DecisionRequestChanges:
		return nil
	case DecisionOverride:
		if d.Justification == "" {
			return errors.New("policy: override decision requires justification")
		}
		return nil
	case "":
		return errors.New("policy: decision action is required")
	default:
		return errors.New("policy: decision action must be approve|request_changes|override")
	}
}

// Run is the durable record of a single PR's lifecycle through the gate
// sequence. It mirrors the policy_runs SQLite table that lands in Task 5 of
// the plan; the engine reads/writes through the Store interface.
//
// FinalState is derived from GateHistory: the engine never stores a synthetic
// "current state" column (status-is-derived rule from AGENTS.md).
type Run struct {
	// ID is the stable policy run id (uuid). Used as the correlation key in
	// all CDC events for this PR's journey.
	ID string
	// ProjectID is the project whose Config governs this run.
	ProjectID string
	// SessionID is the worker session that opened the PR.
	SessionID string
	// PRID is the pull request under review.
	PRID string
	// Config is the frozen Config snapshot at run start.
	Config Config
	// CurrentGate is the GateID the engine is about to enter next, or empty
	// when the run has reached a terminal state.
	CurrentGate GateID
	// FinalState is the terminal state once the run completes; empty while
	// the run is still in flight. Values: "merged", "superseded",
	// "exhausted", "vetoed", "errored". Status is derived from GateHistory so
	// this field is purely a convenience for the read API.
	FinalState string
	// StartedAt is when the engine accepted this run (UTC).
	StartedAt time.Time
	// UpdatedAt is the last successful state transition (UTC).
	UpdatedAt time.Time
	// GateHistory is the append-only list of GateResult rows for this run,
	// newest last. The engine walks this to derive status at read time.
	GateHistory []GateResult
}

// Engine is the contract the daemon, CLI, and HTTP controllers drive. It is
// the only public entry point for the policy package — gates, persistence,
// and CDC emission are all internal collaborators.
//
// Implementations:
//   - production: backed by SQLite (Store) + the SCM observer (Task 4).
//   - tests: a fakeEngine that records calls; see engine_test.go.
//
// Lifecycle:
//  1. Caller calls Run(runID) when a PR is opened on a tracker-spawned session.
//     The engine hydrates RunContext from the Store and walks GateOrder.
//  2. When Gate 3 (Human) or the Gate 4 hybrid-veto path waits for input, the
//     engine parks the run and emits needs_input / human_override_requested.
//  3. The human acts via CLI or HTTP; the caller forwards the Decision to
//     Decide. The engine resumes the run.
//  4. GetRun is the read surface for the desktop and `ao policy get`.
type Engine interface {
	// Run starts (or resumes) the policy run identified by runID. Idempotent:
	// if a run is already in flight for that id, Run returns nil and continues
	// from CurrentGate. If runID is unknown, Run returns ErrRunNotFound.
	//
	// The first call after PR-open hydrates RunContext and records the
	// policy_run_started CDC event. Subsequent calls (e.g. after a retry)
	// advance or retry gates according to GateHistory.
	Run(ctx context.Context, runID string) error

	// Decide forwards a human Decision to the engine. The engine records the
	// decision as a GateResult on the current gate and advances the run:
	//   - approve  → advance to next gate (or proceed to merge after Gate 4)
	//   - request_changes → re-enter at Gate 2 (design doc §2 decision 1)
	//   - override → record Override outcome on the current gate and advance
	//
	// Decide returns ErrRunNotFound when runID is unknown and
	// ErrInvalidDecision when the Decision fails Decision.Validate.
	Decide(ctx context.Context, runID string, decision Decision) error

	// GetRun returns the current durable record for runID. The returned Run
	// is a snapshot; mutating it has no effect on engine state. Returns
	// (Run{}, ErrRunNotFound) when runID is unknown.
	GetRun(ctx context.Context, runID string) (Run, error)
}

// Engine-level errors. Transport layers map these to the same envelopes the
// rest of the daemon uses: ErrRunNotFound → 404, ErrInvalidDecision → 422.
var (
	// ErrRunNotFound is returned by Run / Decide / GetRun when runID does not
	// correspond to any persisted run.
	ErrRunNotFound = errors.New("policy: run not found")
	// ErrInvalidDecision is returned by Decide when the supplied Decision
	// fails Decision.Validate (e.g. override without justification).
	ErrInvalidDecision = errors.New("policy: invalid decision")
	// ErrAlreadyTerminal is returned by Run / Decide when the run has already
	// reached a final state and cannot accept further state transitions.
	ErrAlreadyTerminal = errors.New("policy: run is already terminal")
	// ErrRunNotParked is returned by Decide when the run is not currently
	// waiting on a human decision (i.e. the last recorded GateResult for the
	// current gate is not OutcomeParked).
	ErrRunNotParked = errors.New("policy: run is not waiting for a decision")
)
