package gates

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/policy"
)

// NotifyFunc is the HumanGate → notification system boundary. It mirrors the
// narrow surface HumanGate needs from internal/notify.Manager.Notify so the
// gate has no compile-time dependency on internal/notify's full
// Store/Publisher stack and so tests can inject a fake without fakes for the
// whole Manager.
//
// The intent payload mirrors the contract of ports.NotificationIntent that
// internal/notify consumes; callers should pass through the real
// notify.Intent type at wiring time. Keeping it as an untyped struct here
// (rather than importing ports) avoids a circular dependency between
// internal/policy and internal/notify through this stub package.
//
// Field semantics:
//
//   - RunID, ProjectID, SessionID, PRID identify the policy run.
//   - Kind is the existing needs_input kind (Phase 1 only emits this one).
//   - Title is the short user-facing headline for the notification row.
//   - Body is the longer human-readable explanation.
//
// Production wiring (in a later task) is a one-liner: pass a closure that
// adapts these fields to notify.Intent and forwards to notify.Manager.Notify.
type NotifyFunc func(ctx context.Context, intent NotifyIntent) error

// NotifyIntent is the payload NotifyFunc receives. See NotifyFunc doc.
type NotifyIntent struct {
	RunID     string
	ProjectID string
	SessionID string
	PRID      string
	Kind      string
	Title     string
	Body      string
}

// HumanGate is Gate 3: a human maintainer must explicitly approve the PR
// before the engine advances to Gate 4 (Final). Per design doc §3 Gate 3,
// the engine emits a needs_input notification and parks the run until a
// Decision arrives via Engine.Decide.
//
// Behavior:
//
//   - rc.Config.RequireHumanApproval == false → pass immediately, no
//     notification.
//   - rc.Attempt == 0 (first try): emit a needs_input notification through
//     the injected NotifyFunc (no-op when no notifier is configured, so the
//     gate still works in minimal test setups) and return OutcomeParked. The
//     engine records this and stops driving the run until a human calls
//     Engine.Decide (approve / request_changes / override).
//   - rc.Attempt > 0: should not happen in production — the engine never
//     re-invokes Run for a parked gate, only Decide advances it. Fails
//     closed with a reason to surface the bug rather than silently looping,
//     in case some other caller does invoke it this way.
type HumanGate struct {
	// Notify is the notifier HumanGate invokes on the first attempt. nil
	// disables notification (useful for tests that only care about the
	// returned outcome).
	Notify NotifyFunc
	// Clock is an optional wall-clock source, injected for tests that want
	// deterministic time. Defaults to time.Now when nil.
	Clock func() time.Time
}

// NewHumanGate returns a HumanGate. notify may be nil for tests that don't
// care about the notification side-effect.
func NewHumanGate(notify NotifyFunc) *HumanGate {
	return &HumanGate{Notify: notify}
}

// ID satisfies policy.Gate; returns policy.GateHuman.
func (g *HumanGate) ID() policy.GateID { return policy.GateHuman }

// Run evaluates one human gate attempt. See type doc for the stub logic.
// It honors ctx cancellation and logs every decision via slog.Default().
func (g *HumanGate) Run(ctx context.Context, rc policy.RunContext) (policy.GateOutcome, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	log := slog.Default().With(
		slog.String("gate", string(policy.GateHuman)),
		slog.String("run_id", rc.RunID),
		slog.String("pr_id", rc.PRID),
		slog.Int("attempt", rc.Attempt),
	)

	if !rc.Config.RequireHumanApproval {
		log.InfoContext(ctx, "human gate: require_human_approval disabled, passing")
		return policy.OutcomePass, nil
	}

	if rc.Attempt == 0 {
		// First attempt — emit the needs_input notification and park,
		// waiting for a human Decision.
		if err := g.emitNeedsInput(ctx, rc); err != nil {
			// Notification failure is transient infrastructure: report
			// the error so the engine retries (matches Gate contract: a
			// non-nil error means "could not evaluate at all", not
			// "evaluated to fail").
			log.ErrorContext(ctx, "human gate: needs_input notification failed",
				slog.String("err", err.Error()))
			return "", fmt.Errorf("emit needs_input: %w", err)
		}
		log.InfoContext(ctx, "human gate: needs_input emitted, parking for a human Decision")
		return policy.OutcomeParked, nil
	}

	// rc.Attempt > 0 means the engine re-entered the gate without an
	// explicit Decision, which should never happen (the engine never
	// re-invokes Run for a parked gate). Fail closed with a descriptive
	// reason so the bug surfaces immediately.
	reason := fmt.Sprintf("human gate re-entered at attempt %d without a Decision; the engine must park on first try", rc.Attempt)
	log.WarnContext(ctx, "human gate: unexpected retry, failing closed",
		slog.String("reason", reason))
	return policy.OutcomeFail, nil
}

// emitNeedsInput calls the injected notifier when one is configured. A nil
// notifier is treated as a no-op so the gate still works in unit tests that
// only exercise the outcome shape.
//
// Returning an error is preferred over panic-ing so the gate's contract
// (non-nil error = transient infrastructure failure) is honored.
func (g *HumanGate) emitNeedsInput(ctx context.Context, rc policy.RunContext) error {
	if g.Notify == nil {
		return nil
	}
	intent := NotifyIntent{
		RunID:     rc.RunID,
		ProjectID: rc.ProjectID,
		SessionID: rc.SessionID,
		PRID:      rc.PRID,
		Kind:      "needs_input",
		Title:     "PR ready for human review",
		Body: fmt.Sprintf(
			"Run %s on session %s needs human approval before the final-pass agent runs.",
			rc.RunID, rc.SessionID,
		),
	}
	if err := g.Notify(ctx, intent); err != nil {
		return fmt.Errorf("notifier: %w", err)
	}
	return nil
}
