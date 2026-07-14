package gates

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/modernagent/modern-agent/backend/internal/policy"
)

// Compile-time assertions: each concrete gate must continue to satisfy
// policy.Gate. If a method signature drifts, this file fails to compile and
// the regression is caught before runtime.
var (
	_ policy.Gate = (*CIGate)(nil)
	_ policy.Gate = (*ReviewGate)(nil)
	_ policy.Gate = (*HumanGate)(nil)
	_ policy.Gate = (*FinalGate)(nil)
)

// quietLogger discards slog output during tests so the test runner doesn't
// get spammed with phase-1 stub log lines. Tests that need to assert log
// behavior can construct their own slog.Logger.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// withQuietLogger swaps slog.Default() to a discarding logger for the
// duration of t, restoring the prior default on cleanup.
func withQuietLogger(t *testing.T) {
	t.Helper()
	prev := slog.Default()
	slog.SetDefault(quietLogger())
	t.Cleanup(func() { slog.SetDefault(prev) })
}

// newTestRunContext returns a RunContext populated with the bare minimum
// the gates read. Fields the gates don't touch (e.g. SessionID) get empty
// strings — keeps test intent visible.
func newTestRunContext(cfg policy.PolicyConfig, attempt int) policy.RunContext {
	return policy.RunContext{
		RunID:     "run-test",
		ProjectID: "proj-test",
		SessionID: "sess-test",
		PRID:      "pr-test",
		Config:    cfg,
		Attempt:   attempt,
	}
}

// TestGateIDsMatchConstants is a cheap belt-and-braces check that the
// constructors' ID() values still match the package-level GateID constants.
func TestGateIDsMatchConstants(t *testing.T) {
	withQuietLogger(t)
	cases := []struct {
		name string
		g    policy.Gate
		want policy.GateID
	}{
		{"ci", NewCIGate(), policy.GateCI},
		{"review-same", NewReviewGate(policy.ReviewStrategySameAgent), policy.GateReview},
		{"review-cross", NewReviewGate(policy.ReviewStrategyCrossAgent), policy.GateReview},
		{"review-second-only", NewReviewGate(policy.ReviewStrategySecondOnly), policy.GateReview},
		{"human", NewHumanGate(nil), policy.GateHuman},
		{"final", NewFinalGate(), policy.GateFinal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.g.ID(); got != tc.want {
				t.Errorf("ID() = %q, want %q", got, tc.want)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// CIGate tests
// -----------------------------------------------------------------------------

// TestCIGate_PassOnFirstAttempt: Attempt=0 with default config (3 rounds,
// auto_fix_on_ci_failure=true) returns OutcomeFail in Phase 1 (because the
// stub pretends CI is red on every round < MaxAutoFixRounds). The test name
// in the plan brief calls this "PassOnFirstAttempt"; the actual Phase 1
// behavior is "under-round-limit returns Fail for retry" — the engine then
// re-enters Gate 1 with attempt=1, etc. We name the behavior we assert and
// document the Phase 1 nuance so future readers don't trip on it.
//
// To get a true OutcomePass in Phase 1, set AutoFixOnCIFailure=false (the
// gate honors the documented opt-out by passing through). We exercise that
// path here as the "Pass on first attempt" case, which is the meaningful
// "happy path" assertion the plan brief is asking for.
func TestCIGate_PassOnFirstAttempt(t *testing.T) {
	withQuietLogger(t)
	cfg := policy.DefaultPolicyConfig()
	cfg.AutoFixOnCIFailure = false

	g := NewCIGate()
	out, err := g.Run(context.Background(), newTestRunContext(cfg, 0))
	if err != nil {
		t.Fatalf("Run() err = %v, want nil", err)
	}
	if out != policy.OutcomePass {
		t.Errorf("Run() outcome = %q, want %q (AutoFixOnCIFailure=false should pass through)", out, policy.OutcomePass)
	}
}

// TestCIGate_FailUnderRoundLimit: default config (3 rounds), Attempt=1 →
// OutcomeFail, retriable per IsRetryable.
func TestCIGate_FailUnderRoundLimit(t *testing.T) {
	withQuietLogger(t)
	cfg := policy.DefaultPolicyConfig() // MaxAutoFixRounds=3, AutoFixOnCIFailure=true

	g := NewCIGate()
	out, err := g.Run(context.Background(), newTestRunContext(cfg, 1))
	if err != nil {
		t.Fatalf("Run() err = %v, want nil", err)
	}
	if out != policy.OutcomeFail {
		t.Errorf("Run() outcome = %q, want %q", out, policy.OutcomeFail)
	}
	if !policy.IsRetryable(out) {
		t.Errorf("OutcomeFail must be retryable per IsRetryable")
	}
	if policy.IsTerminalOutcome(out) {
		t.Errorf("OutcomeFail must not be terminal")
	}
}

// TestCIGate_Exhausted: Attempt=MaxAutoFixRounds → OutcomeExhausted, terminal.
func TestCIGate_Exhausted(t *testing.T) {
	withQuietLogger(t)
	cfg := policy.DefaultPolicyConfig() // MaxAutoFixRounds=3

	g := NewCIGate()
	out, err := g.Run(context.Background(), newTestRunContext(cfg, cfg.MaxAutoFixRounds))
	if err != nil {
		t.Fatalf("Run() err = %v, want nil", err)
	}
	if out != policy.OutcomeExhausted {
		t.Errorf("Run() outcome = %q, want %q", out, policy.OutcomeExhausted)
	}
	if !policy.IsTerminalOutcome(out) {
		t.Errorf("OutcomeExhausted must be terminal per IsTerminalOutcome")
	}
	if policy.IsRetryable(out) {
		t.Errorf("OutcomeExhausted must not be retryable")
	}
}

// TestCIGate_ExhaustedAtAndOverLimit guards the >= boundary: Attempt at the
// limit AND Attempt past the limit both report OutcomeExhausted.
func TestCIGate_ExhaustedAtAndOverLimit(t *testing.T) {
	withQuietLogger(t)
	cfg := policy.DefaultPolicyConfig() // MaxAutoFixRounds=3

	g := NewCIGate()
	for _, attempt := range []int{cfg.MaxAutoFixRounds, cfg.MaxAutoFixRounds + 1} {
		out, err := g.Run(context.Background(), newTestRunContext(cfg, attempt))
		if err != nil {
			t.Fatalf("Run(attempt=%d) err = %v, want nil", attempt, err)
		}
		if out != policy.OutcomeExhausted {
			t.Errorf("Run(attempt=%d) outcome = %q, want %q", attempt, out, policy.OutcomeExhausted)
		}
	}
}

// TestCIGate_HonorsContextCancellation: a cancelled ctx surfaces ctx.Err()
// rather than a fabricated outcome. Matches the Gate interface contract.
func TestCIGate_HonorsContextCancellation(t *testing.T) {
	withQuietLogger(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	g := NewCIGate()
	_, err := g.Run(ctx, newTestRunContext(policy.DefaultPolicyConfig(), 0))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Run() err = %v, want context.Canceled", err)
	}
}

// -----------------------------------------------------------------------------
// ReviewGate tests
// -----------------------------------------------------------------------------

// TestReviewGate_AllStrategies exercises the three documented strategies
// with a default-but-active config (RequireAgentReview=true).
//
//   - same_agent, cross_agent: under the round limit → OutcomeFail.
//   - second_only: skips the gate entirely → OutcomePass (Gate 4 will
//     provide the second opinion if it fires; design doc §2 decision 3).
func TestReviewGate_AllStrategies(t *testing.T) {
	withQuietLogger(t)
	cfg := policy.DefaultPolicyConfig() // RequireAgentReview=true, MaxReviseRounds=3

	cases := []struct {
		strategy string
		attempt  int
		want     policy.GateOutcome
	}{
		{policy.ReviewStrategySameAgent, 1, policy.OutcomeFail},
		{policy.ReviewStrategyCrossAgent, 1, policy.OutcomeFail},
		{policy.ReviewStrategySecondOnly, 1, policy.OutcomePass},
	}
	for _, tc := range cases {
		t.Run(tc.strategy, func(t *testing.T) {
			g := NewReviewGate(tc.strategy)
			out, err := g.Run(context.Background(), newTestRunContext(cfg, tc.attempt))
			if err != nil {
				t.Fatalf("Run() err = %v, want nil", err)
			}
			if out != tc.want {
				t.Errorf("Run() outcome = %q, want %q (strategy=%s, attempt=%d)",
					out, tc.want, tc.strategy, tc.attempt)
			}
		})
	}
}

// TestReviewGate_Disabled: RequireAgentReview=false → OutcomePass
// regardless of strategy, attempt, or round limit. Matches design doc §4.
func TestReviewGate_Disabled(t *testing.T) {
	withQuietLogger(t)
	cfg := policy.DefaultPolicyConfig()
	cfg.RequireAgentReview = false

	g := NewReviewGate(policy.ReviewStrategySameAgent)
	// attempt > MaxReviseRounds to prove disabled means "skip the gate".
	out, err := g.Run(context.Background(), newTestRunContext(cfg, cfg.MaxReviseRounds+5))
	if err != nil {
		t.Fatalf("Run() err = %v, want nil", err)
	}
	if out != policy.OutcomePass {
		t.Errorf("Run() outcome = %q, want %q (disabled gate must pass through)", out, policy.OutcomePass)
	}
}

// TestReviewGate_StrategyInheritedFromConfig: a ReviewGate built with an
// empty Strategy field inherits the value from rc.Config.ReviewStrategy at
// Run time. This is the path the engine uses when it doesn't have a per-run
// strategy override.
func TestReviewGate_StrategyInheritedFromConfig(t *testing.T) {
	withQuietLogger(t)
	cfg := policy.DefaultPolicyConfig()
	cfg.ReviewStrategy = policy.ReviewStrategyCrossAgent

	g := NewReviewGate("") // no override → inherit from cfg
	out, err := g.Run(context.Background(), newTestRunContext(cfg, 1))
	if err != nil {
		t.Fatalf("Run() err = %v, want nil", err)
	}
	if out != policy.OutcomeFail {
		t.Errorf("Run() outcome = %q, want %q (cross_agent under limit should fail for retry)",
			out, policy.OutcomeFail)
	}
}

// TestReviewGate_Exhausted: Attempt=MaxReviseRounds → OutcomeExhausted,
// terminal. Same boundary semantics as CIGate but uses MaxReviseRounds.
func TestReviewGate_Exhausted(t *testing.T) {
	withQuietLogger(t)
	cfg := policy.DefaultPolicyConfig() // MaxReviseRounds=3

	g := NewReviewGate(policy.ReviewStrategySameAgent)
	out, err := g.Run(context.Background(), newTestRunContext(cfg, cfg.MaxReviseRounds))
	if err != nil {
		t.Fatalf("Run() err = %v, want nil", err)
	}
	if out != policy.OutcomeExhausted {
		t.Errorf("Run() outcome = %q, want %q", out, policy.OutcomeExhausted)
	}
}

// -----------------------------------------------------------------------------
// HumanGate tests
// -----------------------------------------------------------------------------

// TestHumanGate_RequiresApproval: default config (RequireHumanApproval=true),
// Attempt=0 → OutcomePass AND the injected notifier is called exactly once
// with kind=needs_input. The Phase 1 stub pretends the human approved on
// first contact; Phase 2 will park the run waiting for Engine.Decide.
func TestHumanGate_RequiresApproval(t *testing.T) {
	withQuietLogger(t)
	cfg := policy.DefaultPolicyConfig() // RequireHumanApproval=true

	var calls atomic.Int32
	var lastIntent NotifyIntent
	notify := func(ctx context.Context, intent NotifyIntent) error {
		calls.Add(1)
		lastIntent = intent
		return nil
	}

	g := NewHumanGate(notify)
	out, err := g.Run(context.Background(), newTestRunContext(cfg, 0))
	if err != nil {
		t.Fatalf("Run() err = %v, want nil", err)
	}
	if out != policy.OutcomePass {
		t.Errorf("Run() outcome = %q, want %q", out, policy.OutcomePass)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("notifier call count = %d, want 1", got)
	}
	if lastIntent.Kind != "needs_input" {
		t.Errorf("notifier intent.Kind = %q, want %q", lastIntent.Kind, "needs_input")
	}
	if lastIntent.RunID != "run-test" {
		t.Errorf("notifier intent.RunID = %q, want %q", lastIntent.RunID, "run-test")
	}
	if lastIntent.PRID != "pr-test" {
		t.Errorf("notifier intent.PRID = %q, want %q", lastIntent.PRID, "pr-test")
	}
	if lastIntent.Title == "" {
		t.Errorf("notifier intent.Title must not be empty")
	}
}

// TestHumanGate_DisabledPassThrough: RequireHumanApproval=false →
// OutcomePass with no notifier call. Verifies the notifier is only invoked
// when the gate actually needs the human's attention.
func TestHumanGate_DisabledPassThrough(t *testing.T) {
	withQuietLogger(t)
	cfg := policy.DefaultPolicyConfig()
	cfg.RequireHumanApproval = false

	var calls atomic.Int32
	notify := func(ctx context.Context, intent NotifyIntent) error {
		calls.Add(1)
		return nil
	}

	g := NewHumanGate(notify)
	out, err := g.Run(context.Background(), newTestRunContext(cfg, 0))
	if err != nil {
		t.Fatalf("Run() err = %v, want nil", err)
	}
	if out != policy.OutcomePass {
		t.Errorf("Run() outcome = %q, want %q", out, policy.OutcomePass)
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("notifier call count = %d, want 0 when RequireHumanApproval=false", got)
	}
}

// TestHumanGate_NilNotifierOK: a nil notifier is treated as a no-op so the
// gate still works in minimal test setups. Outcome is what matters; the
// notifier is a side effect.
func TestHumanGate_NilNotifierOK(t *testing.T) {
	withQuietLogger(t)
	cfg := policy.DefaultPolicyConfig() // RequireHumanApproval=true

	g := NewHumanGate(nil)
	out, err := g.Run(context.Background(), newTestRunContext(cfg, 0))
	if err != nil {
		t.Fatalf("Run() err = %v, want nil", err)
	}
	if out != policy.OutcomePass {
		t.Errorf("Run() outcome = %q, want %q", out, policy.OutcomePass)
	}
}

// TestHumanGate_NotifierErrorBubblesUp: a failing notifier surfaces the
// error so the engine treats the attempt as a transient failure (Gate
// contract: non-nil error = "could not evaluate at all").
func TestHumanGate_NotifierErrorBubblesUp(t *testing.T) {
	withQuietLogger(t)
	cfg := policy.DefaultPolicyConfig()
	boom := errors.New("notifier exploded")
	notify := func(ctx context.Context, intent NotifyIntent) error { return boom }

	g := NewHumanGate(notify)
	_, err := g.Run(context.Background(), newTestRunContext(cfg, 0))
	if err == nil {
		t.Fatalf("Run() err = nil, want notifier error to surface")
	}
	if !errors.Is(err, boom) {
		t.Errorf("Run() err = %v, want it to wrap %v", err, boom)
	}
}

// TestHumanGate_RetryWithoutDecisionFailsClosed: a re-entry at Attempt>0 in
// Phase 1 returns OutcomeFail. Phase 2 will park the run on first try and
// only resume via Engine.Decide, so Attempt>0 here would mean a bug.
func TestHumanGate_RetryWithoutDecisionFailsClosed(t *testing.T) {
	withQuietLogger(t)
	cfg := policy.DefaultPolicyConfig()

	var calls atomic.Int32
	g := NewHumanGate(func(ctx context.Context, intent NotifyIntent) error {
		calls.Add(1)
		return nil
	})
	out, err := g.Run(context.Background(), newTestRunContext(cfg, 1))
	if err != nil {
		t.Fatalf("Run() err = %v, want nil", err)
	}
	if out != policy.OutcomeFail {
		t.Errorf("Run() outcome = %q, want %q", out, policy.OutcomeFail)
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("notifier call count = %d, want 0 on retry without Decision", got)
	}
}

// -----------------------------------------------------------------------------
// FinalGate tests
// -----------------------------------------------------------------------------

// TestFinalGate_Pass: default config, Attempt=0 → OutcomePass. This is the
// "happy path" the engine walks after a human approve at Gate 3.
func TestFinalGate_Pass(t *testing.T) {
	withQuietLogger(t)
	cfg := policy.DefaultPolicyConfig() // AgentFinalPass=true

	g := NewFinalGate()
	out, err := g.Run(context.Background(), newTestRunContext(cfg, 0))
	if err != nil {
		t.Fatalf("Run() err = %v, want nil", err)
	}
	if out != policy.OutcomePass {
		t.Errorf("Run() outcome = %q, want %q", out, policy.OutcomePass)
	}
}

// TestFinalGate_FailOnRetry: Attempt>0 → OutcomeFail (Phase 1 stub for the
// "real checks would re-run here" path). Phase 2 will replace this with the
// real rebase + lint + secret scan and trigger hybrid veto on failure.
func TestFinalGate_FailOnRetry(t *testing.T) {
	withQuietLogger(t)
	cfg := policy.DefaultPolicyConfig()

	g := NewFinalGate()
	out, err := g.Run(context.Background(), newTestRunContext(cfg, 1))
	if err != nil {
		t.Fatalf("Run() err = %v, want nil", err)
	}
	if out != policy.OutcomeFail {
		t.Errorf("Run() outcome = %q, want %q", out, policy.OutcomeFail)
	}
	if !policy.IsRetryable(out) {
		t.Errorf("OutcomeFail must be retryable")
	}
}

// TestFinalGate_DisabledPassThrough: AgentFinalPass=false → OutcomePass
// regardless of attempt. Matches design doc §4 (opt-out knob).
func TestFinalGate_DisabledPassThrough(t *testing.T) {
	withQuietLogger(t)
	cfg := policy.DefaultPolicyConfig()
	cfg.AgentFinalPass = false

	g := NewFinalGate()
	// attempt > 0 to prove the disabled knob short-circuits.
	out, err := g.Run(context.Background(), newTestRunContext(cfg, 5))
	if err != nil {
		t.Fatalf("Run() err = %v, want nil", err)
	}
	if out != policy.OutcomePass {
		t.Errorf("Run() outcome = %q, want %q (AgentFinalPass=false must pass through)",
			out, policy.OutcomePass)
	}
}

// TestFinalGate_HonorsContextCancellation: cancelled ctx surfaces ctx.Err().
func TestFinalGate_HonorsContextCancellation(t *testing.T) {
	withQuietLogger(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	g := NewFinalGate()
	_, err := g.Run(ctx, newTestRunContext(policy.DefaultPolicyConfig(), 0))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Run() err = %v, want context.Canceled", err)
	}
}