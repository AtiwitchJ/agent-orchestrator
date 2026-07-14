package gates

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/modernagent/modern-agent/backend/internal/policy"
)

// ReviewGate is Gate 2: an agent (the primary, a designated reviewer, or the
// Gate 4 second-opinion in second_only mode) must approve the changes before
// the engine advances to Gate 3 (Human).
//
// Phase 1 behavior (stub):
//
//   - When rc.Config.RequireAgentReview is false the gate passes immediately.
//     The whole gate can be turned off per-project, matching design doc §4
//     (RequireAgentReview) and §2 decision 5 (opt-in per project).
//   - rc.Config.ReviewStrategy chooses the dispatch shape, but in Phase 1 all
//     three strategies return the same attempt-based outcome. The Phase 2
//     adapter spawns land in a follow-up task; this stub exercises the engine
//     state machine with realistic inputs.
//   - rc.Attempt within rc.Config.MaxReviseRounds → OutcomeFail (engine will
//     re-enter the gate).
//   - rc.Attempt >= rc.Config.MaxReviseRounds → OutcomeExhausted (engine stops
//     the run and escalates to the human per design doc §3 Gate 2).
type ReviewGate struct {
	// Strategy is the resolved review strategy for this run. The engine
	// populates it from rc.Config.ReviewStrategy before invoking Run. We
	// store it on the struct (instead of reading rc.Config again) so tests
	// and Phase 2 implementations have a single source of truth and so the
	// field is plumbed today even though Phase 1 treats all three
	// strategies identically.
	Strategy string
}

// NewReviewGate returns a ReviewGate bound to the given strategy. The
// strategy must be one of policy.ReviewStrategy*; pass "" to inherit from
// rc.Config at Run time (useful for tests).
func NewReviewGate(strategy string) *ReviewGate {
	return &ReviewGate{Strategy: strategy}
}

// ID satisfies policy.Gate; returns policy.GateReview.
func (g *ReviewGate) ID() policy.GateID { return policy.GateReview }

// Run evaluates one review gate attempt. See type doc for the stub logic.
// It honors ctx cancellation and logs every decision via slog.Default().
func (g *ReviewGate) Run(ctx context.Context, rc policy.RunContext) (policy.GateOutcome, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	log := slog.Default().With(
		slog.String("gate", string(policy.GateReview)),
		slog.String("run_id", rc.RunID),
		slog.String("pr_id", rc.PRID),
		slog.Int("attempt", rc.Attempt),
	)

	strategy := g.Strategy
	if strategy == "" {
		strategy = rc.Config.ReviewStrategy
	}
	log = log.With(slog.String("strategy", strategy))

	// Disabled gate → pass through. This matches design doc §6 "No auto-skip
	// of human approval" — Gate 3 still records every transition, but Gate 2
	// can be turned off project-wide without breaking the run.
	if !rc.Config.RequireAgentReview {
		log.InfoContext(ctx, "review gate: require_agent_review disabled, passing")
		return policy.OutcomePass, nil
	}

	// second_only: skip the Gate 2 review entirely; Gate 4 will spawn the
	// second-opinion agent if it fires (design doc §2 decision 3). Treat as
	// pass for the engine state machine.
	if strategy == policy.ReviewStrategySecondOnly {
		log.InfoContext(ctx, "review gate: strategy=second_only, skipping (Gate 4 will provide second opinion)")
		return policy.OutcomePass, nil
	}

	// Unknown strategy: refuse to silently pass. Failing closed is safer than
	// failing open for a gate whose job is to catch regressions before they
	// reach the human.
	if strategy != "" &&
		strategy != policy.ReviewStrategySameAgent &&
		strategy != policy.ReviewStrategyCrossAgent {
		reason := fmt.Sprintf("unknown review strategy %q", strategy)
		log.WarnContext(ctx, "review gate: unknown strategy, failing",
			slog.String("reason", reason))
		return policy.OutcomeFail, nil
	}

	maxRounds := rc.Config.MaxReviseRounds
	if maxRounds <= 0 {
		log.InfoContext(ctx, "review gate: no revise rounds configured, reporting exhausted",
			slog.Int("max_rounds", maxRounds))
		return policy.OutcomeExhausted, nil
	}

	if rc.Attempt > maxRounds {
		log.WarnContext(ctx, "review gate: attempt exceeded max rounds",
			slog.Int("max_rounds", maxRounds))
		return policy.OutcomeExhausted, nil
	}

	if rc.Attempt >= maxRounds {
		log.InfoContext(ctx, "review gate: round limit reached, reporting exhausted",
			slog.Int("max_rounds", maxRounds))
		return policy.OutcomeExhausted, nil
	}

	// Phase 1 stub: under the round limit, return OutcomeFail so the engine
	// exercises its retry path. Real agent spawn lands in Phase 2; until then
	// the gate logic mirrors CIGate so the engine's state machine is
	// exercised identically.
	reason := fmt.Sprintf("reviewer agent not spawned in Phase 1 stub (attempt %d/%d, strategy=%s)",
		rc.Attempt, maxRounds, strategy)
	log.InfoContext(ctx, "review gate: failing stub for retry",
		slog.Int("max_rounds", maxRounds),
		slog.String("reason", reason))
	return policy.OutcomeFail, nil
}
