package domain

// SessionStatus is the single-word DISPLAY status the dashboard renders. It is
// derived from persisted session facts plus PR facts and is never stored.
type SessionStatus string

// The display statuses the dashboard renders.
const (
	StatusWorking          SessionStatus = "working"
	StatusPROpen           SessionStatus = "pr_open"
	StatusDraft            SessionStatus = "draft"
	StatusCIFailed         SessionStatus = "ci_failed"
	StatusReviewPending    SessionStatus = "review_pending"
	StatusChangesRequested SessionStatus = "changes_requested"
	StatusApproved         SessionStatus = "approved"
	StatusMergeable        SessionStatus = "mergeable"
	StatusMerged           SessionStatus = "merged"
	StatusNeedsInput       SessionStatus = "needs_input"
	StatusIdle             SessionStatus = "idle"
	StatusTerminated       SessionStatus = "terminated"
	// StatusNoSignal marks a live session whose agent has never delivered a
	// hook callback for the current spawn/restore: AO cannot tell whether the
	// agent is working or stuck (broken hook pipeline, blocked interactive
	// prompt). Rendered instead of a confident idle.
	StatusNoSignal SessionStatus = "no_signal"
	// StatusStalled marks a live WORKER session whose activity_state has
	// claimed "active" far longer than the configured stall threshold: the
	// agent's own hook signal has gone silent while claiming to be busy. It is
	// a derived, read-time-only status (see IsStalled) — never persisted —
	// and it is what the stallmon background monitor watches for before it
	// ever considers auto-terminating a session. An orchestrator, a
	// terminated session, or a session in the sticky ActivityWaitingInput
	// state can never be stalled; see IsStalled for the exact gate.
	StatusStalled SessionStatus = "stalled"
	// StatusReportPending marks a docs-repo session whose agent is still working
	// and has not yet produced its deliverable artifact. It is derived from
	// the project kind and activity state, never persisted.
	StatusReportPending SessionStatus = "report_pending"
	// StatusReportReady marks a docs-repo session where the agent has exited
	// AND the deliverable artifact has been confirmed by the deliverable
	// watcher. It is derived, never persisted.
	StatusReportReady SessionStatus = "report_ready"
)
