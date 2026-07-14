package policy

import (
	"context"
	"time"
)

// GateID identifies one of the four sequential approval gates the engine runs
// every tracker-spawned PR through. Order matters: the engine moves forward
// only when the previous gate returned OutcomePass or OutcomeOverridden.
//
// Reference: docs/superpowers/specs/2026-07-14-hybrid-approval-gates-design.md §2 (decision 1) and §3 (state machine).
type GateID string

const (
	// GateCI is Gate 1: continuous integration must be green.
	GateCI GateID = "ci"
	// GateReview is Gate 2: an agent self-review (or cross-agent second opinion)
	// must approve the changes.
	GateReview GateID = "review"
	// GateHuman is Gate 3: a human maintainer must explicitly approve before
	// the final-pass agent runs.
	GateHuman GateID = "human"
	// GateFinal is Gate 4: an agent final-pass scans for rebase conflicts,
	// lint failures, secret hits, and commit-message convention. Failures here
	// enter the hybrid-veto path (design doc §3.1).
	GateFinal GateID = "final"
)

// GateOrder is the canonical gate sequence. The engine walks this slice in
// order; human "request_changes" restarts at index 1 (GateReview) per design
// doc §2 decision 1, not at index 0.
var GateOrder = []GateID{GateCI, GateReview, GateHuman, GateFinal}

// GateOutcome is the terminal status of a single gate attempt. Outcomes are
// stored verbatim in the gate_results table and surfaced through CDC events
// (gate_passed, gate_failed, gate_exhausted, human_override_decided).
type GateOutcome string

const (
	// OutcomePass: gate satisfied; engine may advance to the next gate or to
	// the merge policy.
	OutcomePass GateOutcome = "pass"
	// OutcomeFail: gate did not satisfy but the run has remaining rounds; the
	// engine will retry automatically.
	OutcomeFail GateOutcome = "fail"
	// OutcomeExhausted: round limit hit; engine stops the run and escalates to
	// the human (design doc §3 — exhausted branches emit agent_exhausted_ci /
	// agent_exhausted_review notifications).
	OutcomeExhausted GateOutcome = "exhausted"
	// OutcomeOverridden: gate failed but the human overrode the result
	// (design doc §3.1 — human_override_after_veto path).
	OutcomeOverridden GateOutcome = "overridden"
)

// GateResult is the durable record of a single gate attempt. One row per
// attempt — retries create new rows, they do not mutate the previous one.
// Status is derived: the engine never stores a synthetic "current" column.
type GateResult struct {
	// RunID is the policy run this attempt belongs to. Stable across all
	// retries of a single PR's lifecycle.
	RunID string
	// GateID is which of the four gates produced this result.
	GateID GateID
	// Attempt is the 1-indexed round number within the gate. Attempt=1 is the
	// first try; n=MaxAutoFixRounds is the last allowed retry.
	Attempt int
	// Outcome is the terminal status (see GateOutcome constants).
	Outcome GateOutcome
	// Reason is a short machine-readable description of why the outcome
	// occurred (e.g. "checks failed: foo", "agent rejected: bar"). Surfaced in
	// CDC gate_failed payload.
	Reason string
	// SecondVote captures the hybrid-veto second-opinion agent's vote when
	// GateID == GateFinal and the run entered the veto path. Empty otherwise.
	// Values are "approve" or "reject" with Rationale in Justification.
	SecondVote string
	// Justification is the human's free-text rationale when Outcome ==
	// OutcomeOverridden. Required for human_override_after_veto events;
	// persisted verbatim so Phase 2 telemetry can audit override quality.
	Justification string
	// Duration is wall-clock time the gate spent in Run. Useful for
	// gate_passed CDC events and Phase 2 cost telemetry.
	Duration time.Duration
}

// RunContext is the read-only view a gate receives when invoked. It carries
// everything the gate needs to evaluate the current attempt without reaching
// back into the engine. Mutating RunContext from a Gate has no effect.
type RunContext struct {
	// RunID is the policy run id (stable per PR lifecycle).
	RunID string
	// ProjectID identifies the project whose PolicyConfig governs this run.
	ProjectID string
	// SessionID is the worker session that opened the PR.
	SessionID string
	// PRID is the pull request under review.
	PRID string
	// Config is the snapshot of PolicyConfig active for this run. Frozen at
	// run start so per-project config changes mid-run do not desync gates.
	Config PolicyConfig
	// Attempt is the 1-indexed round within the current gate. Gates use this
	// to decide whether they are on their last allowed retry.
	Attempt int
}

// Gate is the contract every concrete gate implementation must satisfy. The
// engine dispatches a gate by calling Run once per attempt; the gate returns
// its terminal outcome and an optional error.
//
// Contract:
//   - Run MUST honor ctx cancellation and return ctx.Err() promptly.
//   - Run MUST be idempotent: calling it twice with the same RunContext MUST
//     produce the same outcome (no internal state mutation that depends on
//     call order).
//   - A non-nil error means the gate could not evaluate at all (transient
//     infrastructure failure). The engine treats this as a fail-with-retry
//     rather than a terminal failure, distinct from OutcomeFail.
//   - The returned outcome is final for this attempt; the engine records it
//     and decides whether to advance, retry, or escalate.
type Gate interface {
	// ID returns which GateID this implementation handles. The engine uses
	// this to dispatch gates from GateOrder.
	ID() GateID
	// Run executes one attempt of this gate against the given context and
	// returns its terminal outcome plus any infrastructure-level error.
	Run(ctx context.Context, rc RunContext) (GateOutcome, error)
}