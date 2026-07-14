package gates

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/modernagent/modern-agent/backend/internal/policy"
)

// FinalGate is Gate 4: an agent final-pass runs the four checks from design
// doc §3 (rebase against main → lint/policy scan → secret/leftover-debug
// scan → commit-message convention check). A failure here triggers the
// hybrid-veto path described in §3.1.
//
// Phase 1 behavior (stub):
//
//   - rc.Attempt == 0 → OutcomePass. The first attempt is the "fresh PR"
//     state; the engine has just received an approve decision from Gate 3
//     and the run is otherwise in good shape. Returning pass lets the
//     engine proceed to the merge policy.
//   - rc.Attempt > 0 → OutcomeFail with a descriptive reason. Phase 2 will
//     replace this stub with the real rebase + lint + secret scan; the
//     hybrid-veto flow lives in a separate hybrid_veto.go file (Plan
//     Task 8 Step 4) which FinalGate will hand off to on failure.
//
// Why pass on first attempt and fail on retry: in Phase 2 the gate runs the
// four checks once and only re-runs them after a human requests changes
// (design doc §2 decision 1 → restart at Gate 2, which eventually reaches
// Gate 4 again with Attempt incremented). A re-entered Gate 4 in Phase 1
// is therefore the right place to surface the "real checks would run here"
// TODO without breaking the engine's happy-path test in Phase 1.
type FinalGate struct{}

// NewFinalGate returns a ready-to-use FinalGate. The struct carries no state
// in Phase 1; the constructor exists so call sites match the other gate
// constructors and so Phase 2 can plumb dependencies without changing them.
func NewFinalGate() *FinalGate { return &FinalGate{} }

// ID satisfies policy.Gate; returns policy.GateFinal.
func (g *FinalGate) ID() policy.GateID { return policy.GateFinal }

// Run evaluates one final-pass attempt. See type doc for the stub logic.
// It honors ctx cancellation and logs every decision via slog.Default().
func (g *FinalGate) Run(ctx context.Context, rc policy.RunContext) (policy.GateOutcome, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	log := slog.Default().With(
		slog.String("gate", string(policy.GateFinal)),
		slog.String("run_id", rc.RunID),
		slog.String("pr_id", rc.PRID),
		slog.Int("attempt", rc.Attempt),
	)

	// rc.Config.AgentFinalPass=false is the documented opt-out from §4. Pass
	// through to the merge policy without running checks. The merge policy
	// is a separate concern and lives outside this package.
	if !rc.Config.AgentFinalPass {
		log.InfoContext(ctx, "final gate: agent_final_pass disabled, passing")
		return policy.OutcomePass, nil
	}

	if rc.Attempt == 0 {
		log.InfoContext(ctx, "final gate: first attempt, passing stub (Phase 2: rebase+lint+secret+commit-msg)")
		return policy.OutcomePass, nil
	}

	// Phase 2: actual rebase + lint + secret scan + commit-message check.
	// On any failure, hand off to hybrid_veto.go (Plan Task 8 Step 4).
	reason := fmt.Sprintf("final-pass re-entered at attempt %d; Phase 2 will run rebase+lint+secret+commit-msg checks and trigger hybrid veto on failure", rc.Attempt)
	log.InfoContext(ctx, "final gate: re-entry, failing stub",
		slog.String("reason", reason))
	return policy.OutcomeFail, nil
}