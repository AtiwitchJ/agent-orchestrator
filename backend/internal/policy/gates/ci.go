package gates

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/modernagent/modern-agent/backend/internal/policy"
)

// PRCIState mirrors domain.CIState without importing internal/domain, keeping
// this package's dependency footprint narrow — the same adapter-boundary
// approach gates/human.go already uses for NotifyIntent.
type PRCIState string

// PRCIState values, matching domain.CIState's vocabulary one-for-one.
const (
	PRCIUnknown PRCIState = "unknown"
	PRCIPending PRCIState = "pending"
	PRCIPassing PRCIState = "passing"
	PRCIFailing PRCIState = "failing"
)

// PRChecker resolves the real CI state of a PR. Production wiring passes a
// closure over the SQLite store's ListPRFactsForSession (matching sessionID +
// prID to a domain.PRFacts.CI, mapped to PRCIState); tests can inject a fake.
// A nil PRChecker keeps CIGate on its Phase 1 stub behavior (see type doc).
type PRChecker func(ctx context.Context, sessionID, prID string) (PRCIState, error)

// CIGate is Gate 1: continuous integration must be green before the engine
// advances to Gate 2 (Review).
//
// Behavior:
//
//   - If rc.Config.AutoFixOnCIFailure is false, the gate passes immediately.
//     Auto-fix being off means the engine will not dispatch fix-up commits, so
//     pretending "green" is the only meaningful outcome without a real
//     observer wired to retry.
//   - When Check is set, it resolves the PR's real CI state: PRCIPassing
//     passes the gate; anything else (pending/failing/unknown) falls through
//     to the same round-counting retry/exhaust logic as the Phase 1 stub.
//   - When Check is nil (Phase 1 stub, e.g. via NewCIGate), the gate cannot
//     observe real CI state at all, so it always falls through to the same
//     retry/exhaust round-counting, returning OutcomeFail until the round
//     limit is hit.
//   - Once rc.Attempt >= rc.Config.MaxAutoFixRounds the gate reports
//     OutcomeExhausted so the engine stops the run and escalates to the human
//     per design doc §3 (Gate 1: "exhausted → notify human, stop run").
//
// The round-limit default lives in policy.DefaultPolicyConfig (3 rounds);
// Validate on Config caps the override at policy.MaxRoundCeiling so
// CIGate can trust the value it reads.
type CIGate struct {
	// Check resolves a PR's real CI state. Nil keeps the Phase 1 stub
	// behavior (see type doc) — set via NewCIGateWithChecker in production.
	Check PRChecker
}

// NewCIGate returns a CIGate with no real CI-state check wired (Phase 1 stub
// behavior — see type doc). The constructor exists so call sites match the
// other gate constructors.
func NewCIGate() *CIGate { return &CIGate{} }

// NewCIGateWithChecker returns a CIGate that consults check for a PR's real
// CI state instead of always falling through to the round-counting stub.
func NewCIGateWithChecker(check PRChecker) *CIGate { return &CIGate{Check: check} }

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

	if g.Check != nil {
		state, err := g.Check(ctx, rc.SessionID, rc.PRID)
		if err != nil {
			// A check failure is transient infrastructure trouble, not a
			// verdict — the Gate contract treats a non-nil error as "could
			// not evaluate", distinct from OutcomeFail.
			return "", fmt.Errorf("ci gate: check PR state: %w", err)
		}
		if state == PRCIPassing {
			log.InfoContext(ctx, "ci gate: checks passing", slog.String("state", string(state)))
			return policy.OutcomePass, nil
		}
		log.InfoContext(ctx, "ci gate: checks not passing, failing for retry",
			slog.Int("max_rounds", maxRounds), slog.String("state", string(state)))
		return policy.OutcomeFail, nil
	}

	// Phase 1 stub (no Check wired): under the round limit, return
	// OutcomeFail so the engine exercises its retry path.
	reason := fmt.Sprintf("ci state unknown in Phase 1 stub (attempt %d/%d)", rc.Attempt, maxRounds)
	log.InfoContext(ctx, "ci gate: failing stub for retry",
		slog.Int("max_rounds", maxRounds),
		slog.String("reason", reason))
	return policy.OutcomeFail, nil
}
