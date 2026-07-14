package gates

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/modernagent/modern-agent/backend/internal/policy"
)

// CIGate is Gate 1: continuous integration must be green before the engine
// advances to Gate 2 (Review).
//
// Phase 1 behavior (stub):
//
//   - If rc.Config.AutoFixOnCIFailure is false, the gate passes immediately.
//     Auto-fix being off means the engine will not dispatch fix-up commits, so
//     pretending "green" is the only meaningful stub outcome; the real signal
//     comes from the SCM observer in Phase 2.
//   - If rc.Attempt is within rc.Config.MaxAutoFixRounds, the gate reports
//     OutcomeFail — the engine will re-enter the gate (and, in Phase 2, dispatch
//     an auto-fix commit).
//   - Once rc.Attempt >= rc.Config.MaxAutoFixRounds the gate reports
//     OutcomeExhausted so the engine stops the run and escalates to the human
//     per design doc §3 (Gate 1: "exhausted → notify human, stop run").
//
// The round-limit default lives in policy.DefaultPolicyConfig (3 rounds);
// Validate on Config caps the override at policy.MaxRoundCeiling so
// CIGate can trust the value it reads.
//
// Phase 2 will replace the stub with a real SCM observer subscription: fetch
// the PR's check status for rc.PRID, return OutcomePass on success, and only
// fall back to the round counter when checks are still pending or failing.
type CIGate struct{}

// NewCIGate returns a ready-to-use CIGate. The struct carries no state in
// Phase 1; the constructor exists so call sites match the other gate
// constructors and so Phase 2 can plumb dependencies without changing them.
func NewCIGate() *CIGate { return &CIGate{} }

// ID satisfies policy.Gate; returns policy.GateCI.
func (g *CIGate) ID() policy.GateID { return policy.GateCI }

// Run evaluates one CI gate attempt. See type doc for the stub logic.
// It honors ctx cancellation and logs every decision via slog.Default() so
// the Phase 1 run trace matches the CDC events Phase 2 will emit.
func (g *CIGate) Run(ctx context.Context, rc policy.RunContext) (policy.GateOutcome, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	log := slog.Default().With(
		slog.String("gate", string(policy.GateCI)),
		slog.String("run_id", rc.RunID),
		slog.String("pr_id", rc.PRID),
		slog.Int("attempt", rc.Attempt),
	)

	// Auto-fix disabled → pretend green. The real SCM observer hookup lands in
	// Phase 2; without it, "fail because checks are unknown" would block every
	// run forever, so honor the explicit opt-out.
	if !rc.Config.AutoFixOnCIFailure {
		log.InfoContext(ctx, "ci gate: auto-fix disabled, passing stub")
		return policy.OutcomePass, nil
	}

	maxRounds := rc.Config.MaxAutoFixRounds
	if maxRounds <= 0 {
		// Defensive: Validate already rejects < 0, but a project could set 0
		// to mean "no auto-fix attempts at all". Treat as exhausted immediately.
		log.InfoContext(ctx, "ci gate: no auto-fix rounds configured, reporting exhausted",
			slog.Int("max_rounds", maxRounds))
		return policy.OutcomeExhausted, nil
	}

	if rc.Attempt > maxRounds {
		// Past the limit — should not normally happen because the engine stops
		// calling Run once a gate reports OutcomeExhausted, but guard anyway.
		log.WarnContext(ctx, "ci gate: attempt exceeded max rounds",
			slog.Int("max_rounds", maxRounds))
		return policy.OutcomeExhausted, nil
	}

	if rc.Attempt >= maxRounds {
		log.InfoContext(ctx, "ci gate: round limit reached, reporting exhausted",
			slog.Int("max_rounds", maxRounds))
		return policy.OutcomeExhausted, nil
	}

	// Phase 1 stub: under the round limit, return OutcomeFail so the engine
	// exercises its retry path. Real CI state lands with the SCM observer.
	reason := fmt.Sprintf("ci state unknown in Phase 1 stub (attempt %d/%d)", rc.Attempt, maxRounds)
	log.InfoContext(ctx, "ci gate: failing stub for retry",
		slog.Int("max_rounds", maxRounds),
		slog.String("reason", reason))
	return policy.OutcomeFail, nil
}
