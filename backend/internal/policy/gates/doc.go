// Package gates holds the four concrete Gate implementations the policy
// engine dispatches per run: CI (Gate 1), Review (Gate 2), Human (Gate 3),
// and Final (Gate 4). Each implementation satisfies the policy.Gate
// interface (ID + Run) and is responsible only for evaluating its own gate
// against a RunContext; the engine owns sequencing, retry, and terminal
// state transitions.
//
// Phase 1 scope:
//
//   - All four gates return pass/fail/exhausted based on rc.Attempt and the
//     matching round limit in rc.Config. No external integrations are made.
//   - HumanGate emits a notification through an injectable Notifier interface
//     (see NotifyFunc below) so the package has no hard dependency on
//     internal/notify's full Store/Publisher stack. Tests inject a fake
//     notifier; production wires the real notify.Manager.
//
// Phase 2 (future tasks) will replace the attempt-based stubs with real
// integrations:
//
//   - CIGate: subscribe to SCM observer check events from internal/observe/scm.
//   - ReviewGate: spawn a review agent through internal/adapters/agent/.
//   - HumanGate: actually wait on the human_override_decided event.
//   - FinalGate: run rebase + lint + secret scan via existing adapters and
//     trigger the hybrid-veto path on failure.
//
// References:
//   - docs/superpowers/specs/2026-07-14-hybrid-approval-gates-design.md §3
//   - docs/superpowers/plans/2026-07-14-hybrid-approval-gates.md Tasks 5–8
package gates
