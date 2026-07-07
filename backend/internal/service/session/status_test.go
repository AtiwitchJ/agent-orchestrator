package session

import (
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

var statusNow = time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

// statusStallThreshold is a stand-in stall threshold for tests that don't
// care about stalled-status derivation. None of the pre-existing records
// below set Kind to domain.KindWorker (the zero SessionKind is ""), so
// domain.IsStalled structurally can never fire for them regardless of this
// value — see the dedicated stalled-status test cases further down for the
// cases that do care.
const statusStallThreshold = 4 * time.Minute

// statusRec builds a session whose agent HAS delivered a hook signal; the
// no-signal cases below zero FirstSignalAt explicitly.
func statusRec(activity domain.ActivityState, terminated bool) domain.SessionRecord {
	return domain.SessionRecord{
		Activity:      domain.Activity{State: activity, LastActivityAt: statusNow},
		FirstSignalAt: statusNow,
		IsTerminated:  terminated,
	}
}

// silentRec builds a live session that has never delivered a hook signal,
// seeded (spawned/restored) `age` before the derivation time.
func silentRec(age time.Duration) domain.SessionRecord {
	return domain.SessionRecord{
		Activity: domain.Activity{State: domain.ActivityIdle, LastActivityAt: statusNow.Add(-age)},
	}
}

func statusPR(facts domain.PRFacts) []domain.PRFacts { return []domain.PRFacts{facts} }

func TestServiceDerivesStatusFromSessionFactsAndPR(t *testing.T) {
	tests := []struct {
		name string
		rec  domain.SessionRecord
		pr   []domain.PRFacts
		// hookless marks a harness with no activity pipeline (signalCapable
		// false): silence is its permanent normal state, never no_signal.
		hookless bool
		want     domain.SessionStatus
	}{
		{"terminated", statusRec(domain.ActivityExited, true), nil, false, domain.StatusTerminated},
		{"merged-pr", statusRec(domain.ActivityIdle, true), statusPR(domain.PRFacts{Merged: true}), false, domain.StatusMerged},
		{"needs-input", statusRec(domain.ActivityWaitingInput, false), statusPR(domain.PRFacts{CI: domain.CIFailing}), false, domain.StatusNeedsInput},
		{"ci-failed", statusRec(domain.ActivityIdle, false), statusPR(domain.PRFacts{CI: domain.CIFailing}), false, domain.StatusCIFailed},
		{"draft", statusRec(domain.ActivityIdle, false), statusPR(domain.PRFacts{Draft: true}), false, domain.StatusDraft},
		{"changes-requested", statusRec(domain.ActivityIdle, false), statusPR(domain.PRFacts{Review: domain.ReviewChangesRequest}), false, domain.StatusChangesRequested},
		{"mergeable", statusRec(domain.ActivityIdle, false), statusPR(domain.PRFacts{Mergeability: domain.MergeMergeable}), false, domain.StatusMergeable},
		{"approved", statusRec(domain.ActivityIdle, false), statusPR(domain.PRFacts{Review: domain.ReviewApproved}), false, domain.StatusApproved},
		{"review-pending", statusRec(domain.ActivityIdle, false), statusPR(domain.PRFacts{Review: domain.ReviewRequired}), false, domain.StatusReviewPending},
		{"pr-open", statusRec(domain.ActivityIdle, false), statusPR(domain.PRFacts{}), false, domain.StatusPROpen},
		{"working", statusRec(domain.ActivityActive, false), nil, false, domain.StatusWorking},
		{"idle", statusRec(domain.ActivityIdle, false), nil, false, domain.StatusIdle},

		// A live session whose hook-capable agent never signaled is no_signal
		// once the grace passes — never a confident idle.
		{"no-signal-after-grace", silentRec(2 * noSignalGrace), nil, false, domain.StatusNoSignal},
		// A hook-less harness can never signal: its silence stays idle forever
		// instead of degrading into a false "needs you".
		{"hookless-silent-stays-idle", silentRec(2 * noSignalGrace), nil, true, domain.StatusIdle},
		// Right after spawn the agent legitimately hasn't called back yet.
		{"silent-within-grace-is-idle", silentRec(10 * time.Second), nil, false, domain.StatusIdle},
		// Termination and PR facts outrank the missing-signal downgrade.
		{
			"no-signal-terminated-wins",
			domain.SessionRecord{Activity: domain.Activity{State: domain.ActivityExited, LastActivityAt: statusNow.Add(-2 * noSignalGrace)}, IsTerminated: true},
			nil,
			false,
			domain.StatusTerminated,
		},
		{"no-signal-pr-wins", silentRec(2 * noSignalGrace), statusPR(domain.PRFacts{}), false, domain.StatusPROpen},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deriveStatus(tt.rec, tt.pr, domain.ProjectKindSingleRepo, statusNow, statusStallThreshold, !tt.hookless); got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

// A blocked stacked child cannot merge until its parent does, so its readiness
// signals are suppressed, but its problem signals (failing CI, draft,
// requested-changes/unresolved-comments) must still surface for the session.
func TestAggregateStackedChildSignals(t *testing.T) {
	parent := domain.PRFacts{URL: "parent", SourceBranch: "feat", Mergeability: domain.MergeMergeable}
	child := func(f domain.PRFacts) domain.PRFacts {
		f.URL = "child"
		f.SourceBranch = "feat/child"
		f.TargetBranch = "feat"
		return f
	}
	tests := []struct {
		name string
		prs  []domain.PRFacts
		want domain.SessionStatus
	}{
		{"blocked-child-ci-failing-surfaces", []domain.PRFacts{parent, child(domain.PRFacts{CI: domain.CIFailing})}, domain.StatusCIFailed},
		{"blocked-child-draft-surfaces", []domain.PRFacts{parent, child(domain.PRFacts{Draft: true})}, domain.StatusDraft},
		{"blocked-child-changes-requested-surfaces", []domain.PRFacts{parent, child(domain.PRFacts{Review: domain.ReviewChangesRequest})}, domain.StatusChangesRequested},
		{"blocked-child-unresolved-comments-surfaces", []domain.PRFacts{parent, child(domain.PRFacts{ReviewComments: true})}, domain.StatusChangesRequested},
		// A blocked child's readiness signals stay hidden: only the parent's
		// mergeable state drives the session.
		{"blocked-child-mergeable-suppressed", []domain.PRFacts{parent, child(domain.PRFacts{Mergeability: domain.MergeMergeable})}, domain.StatusMergeable},
		{"blocked-child-approved-suppressed", []domain.PRFacts{parent, child(domain.PRFacts{Review: domain.ReviewApproved})}, domain.StatusMergeable},
		// Degenerate set where every open PR is blocked and none is actionable:
		// fall back to the raw aggregate so the session never goes dark.
		{
			"all-blocked-no-actionable-falls-back",
			[]domain.PRFacts{
				{URL: "a", SourceBranch: "feat/a", TargetBranch: "feat/b", Mergeability: domain.MergeMergeable},
				{URL: "b", SourceBranch: "feat/b", TargetBranch: "feat/a", Mergeability: domain.MergeMergeable},
			},
			domain.StatusMergeable,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deriveStatus(statusRec(domain.ActivityIdle, false), tt.prs, domain.ProjectKindSingleRepo, statusNow, statusStallThreshold, true); got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

// Without an injected capability predicate the service must never claim
// no_signal; with one, capability follows the predicate per harness.
func TestHarnessSignalsCapabilityGate(t *testing.T) {
	if (&Service{}).harnessSignals(domain.HarnessCodex) {
		t.Fatal("zero-value Service reports signal-capable; want incapable (never no_signal)")
	}
	s := NewWithDeps(Deps{SignalCapable: func(h domain.AgentHarness) bool { return h == domain.HarnessCodex }})
	if !s.harnessSignals(domain.HarnessCodex) {
		t.Fatal("harnessSignals(codex) = false with codex-capable predicate")
	}
	if s.harnessSignals(domain.HarnessAmp) {
		t.Fatal("harnessSignals(amp) = true with codex-only predicate")
	}
}

// stalledWorker builds a worker session whose activity_state has claimed
// "active" for twice the test threshold without a fresh signal — the
// canonical case IsStalled is built to catch.
func stalledWorker() domain.SessionRecord {
	return domain.SessionRecord{
		Kind:          domain.KindWorker,
		Activity:      domain.Activity{State: domain.ActivityActive, LastActivityAt: statusNow.Add(-2 * statusStallThreshold)},
		FirstSignalAt: statusNow.Add(-2 * statusStallThreshold),
	}
}

// TestDeriveStatusStalled covers the 7-safety-property surface at the
// deriveStatus level: only a stale-active WORKER surfaces as stalled; an
// orchestrator in the identical stale-active shape never does (structural
// immunity), and the sticky waiting_input state never does either. It also
// pins that a stalled worker wins over an otherwise-mergeable PR, since
// stallmon's kill decision and the dashboard badge must agree that a
// silently-stuck agent needs attention regardless of how its PR looks.
func TestDeriveStatusStalled(t *testing.T) {
	mergeablePR := statusPR(domain.PRFacts{Mergeability: domain.MergeMergeable})

	tests := []struct {
		name string
		rec  domain.SessionRecord
		prs  []domain.PRFacts
		want domain.SessionStatus
	}{
		{"stale-active-worker-is-stalled", stalledWorker(), nil, domain.StatusStalled},
		{
			"orchestrator-immune-even-when-stale-active",
			func() domain.SessionRecord { r := stalledWorker(); r.Kind = domain.KindOrchestrator; return r }(),
			nil,
			domain.StatusWorking,
		},
		{
			"waiting-input-sticky-immune-even-when-stale",
			func() domain.SessionRecord {
				r := stalledWorker()
				r.Activity.State = domain.ActivityWaitingInput
				return r
			}(),
			nil,
			domain.StatusNeedsInput,
		},
		{"stalled-beats-mergeable-pr", stalledWorker(), mergeablePR, domain.StatusStalled},
		{
			"fresh-active-worker-not-stalled",
			domain.SessionRecord{Kind: domain.KindWorker, Activity: domain.Activity{State: domain.ActivityActive, LastActivityAt: statusNow}, FirstSignalAt: statusNow},
			nil,
			domain.StatusWorking,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deriveStatus(tt.rec, tt.prs, domain.ProjectKindSingleRepo, statusNow, statusStallThreshold, true); got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestDeriveStatusDocsRepo(t *testing.T) {
	docsRepo := domain.ProjectKindDocsRepo
	tests := []struct {
		name    string
		rec     domain.SessionRecord
		prs     []domain.PRFacts
		want    domain.SessionStatus
	}{
		{
			"terminated-no-deliverable",
			func() domain.SessionRecord {
				r := statusRec(domain.ActivityExited, true)
				return r
			}(),
			nil,
			domain.StatusTerminated,
		},
		{
			"terminated-with-deliverable",
			func() domain.SessionRecord {
				r := statusRec(domain.ActivityExited, true)
				r.DeliverableConfirmedAt = statusNow
				return r
			}(),
			nil,
			domain.StatusReportReady,
		},
		{
			"active-no-deliverable",
			statusRec(domain.ActivityActive, false),
			nil,
			domain.StatusReportPending,
		},
		{
			"idle-no-deliverable",
			statusRec(domain.ActivityIdle, false),
			nil,
			domain.StatusIdle,
		},
		{
			"waiting-input",
			statusRec(domain.ActivityWaitingInput, false),
			nil,
			domain.StatusNeedsInput,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deriveStatus(tt.rec, tt.prs, docsRepo, statusNow, statusStallThreshold, true); got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}
