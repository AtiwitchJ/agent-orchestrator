export type SessionStatus =
	| "working"
	| "pr_open"
	| "draft"
	| "ci_failed"
	| "review_pending"
	| "changes_requested"
	| "approved"
	| "mergeable"
	| "merged"
	| "needs_input"
	| "no_signal"
	| "stalled"
	| "idle"
	| "terminated"
	| "unknown";

const sessionStatuses = new Set<SessionStatus>([
	"working",
	"pr_open",
	"draft",
	"ci_failed",
	"review_pending",
	"changes_requested",
	"approved",
	"mergeable",
	"merged",
	"needs_input",
	"no_signal",
	"stalled",
	"idle",
	"terminated",
]);

export function toSessionStatus(status?: string, isTerminated = false): SessionStatus {
	if (status && sessionStatuses.has(status as SessionStatus)) return status as SessionStatus;
	return isTerminated ? "terminated" : "unknown";
}

export type SessionActivityState = "active" | "idle" | "waiting_input" | "exited" | "unknown";

const sessionActivityStates = new Set<SessionActivityState>(["active", "idle", "waiting_input", "exited"]);

export type SessionActivity = {
	state: SessionActivityState;
	lastActivityAt: string;
};

export function toSessionActivity(
	activity?: { state?: string; lastActivityAt?: string } | null,
): SessionActivity | undefined {
	if (!activity) return undefined;
	const state = sessionActivityStates.has(activity.state as SessionActivityState)
		? (activity.state as SessionActivityState)
		: "unknown";
	return {
		state,
		lastActivityAt: activity.lastActivityAt ?? "",
	};
}

export type AgentProvider =
	| "codex"
	| "claude-code"
	| "opencode"
	| "aider"
	| "grok"
	| "droid"
	| "amp"
	| "agy"
	| "crush"
	| "cursor"
	| "qwen"
	| "copilot"
	| "goose"
	| "auggie"
	| "continue"
	| "devin"
	| "cline"
	| "kimi"
	| "kiro"
	| "kilocode"
	| "vibe"
	| "pi"
	| "autohand"
	| "command";

/** A file in a worker's worktree diff (drives the Git review rail). */
export type ChangedFile = {
	path: string;
	additions: number;
	deletions: number;
	staged?: boolean;
};

export type SessionKind = "worker" | "orchestrator";

/** Lifecycle state of a single pull request, mirrors the daemon's enum. */
export type PRState = "open" | "draft" | "merged" | "closed";

/**
 * One attributed pull request, mirroring the daemon's SessionPRFacts wire shape.
 * A session can own many (e.g. a stack), so {@link WorkspaceSession.prs} is a
 * list. The wire carries no source/target branch or parent pointer, so the UI
 * renders a flat list of PRs, not a stack tree.
 */
export type PullRequestFacts = {
	url: string;
	number: number;
	state: PRState;
	ci: string;
	review: string;
	mergeability: string;
	reviewComments: boolean;
	updatedAt: string;
};

export type WorkspaceSession = {
	id: string;
	terminalHandleId?: string;
	workspaceId: string;
	workspaceName: string;
	title: string;
	/** Raw issue/task identifier from the daemon. Intake ids are provider-prefixed. */
	issueId?: string;
	provider: AgentProvider;
	kind?: SessionKind;
	branch: string;
	status: SessionStatus;
	/** ISO timestamp from the daemon — used for relative time in the inspector. */
	createdAt?: string;
	/** ISO timestamp from the daemon. */
	updatedAt: string;
	/** Raw agent lifecycle activity from the daemon. */
	activity?: SessionActivity;
	/**
	 * Live preview target set by the daemon (via `ao preview`) and streamed over
	 * CDC. When non-empty, the browser panel opens and navigates here.
	 */
	previewUrl?: string;
	/**
	 * Monotonic counter the daemon bumps on every `ao preview` call (even when
	 * previewUrl is unchanged), so the browser panel can re-navigate / refresh on
	 * a repeated preview of the same target.
	 */
	previewRevision?: number;
	/** The session's git diff against its base, when known. */
	changedFiles?: ChangedFile[];
	/** Pre-filled commit subject for the Git rail, when known. */
	commitMessage?: string;
	/**
	 * The session's attributed pull requests. One session can own many (a stack
	 * or independent PRs); empty when none are open yet. Status aggregation is
	 * done server-side, so {@link status} already reflects all of these.
	 */
	prs: PullRequestFacts[];
	/**
	 * Display status as derived by the daemon at read time. Optional override; when
	 * absent it is derived from {@link SessionStatus} via {@link workerDisplayStatus}.
	 */
	displayStatus?: WorkerDisplayStatus;
};

// Tracker providers whose ids the intake daemon stamps sessions with, in
// "<provider>:<native>" form. Adding a provider (Linear, Jira, ...) later is
// just another prefix in this list — no caller of canonicalTrackerIssueId
// needs to change.
const TRACKER_PROVIDER_PREFIXES = ["github:"] as const;

/**
 * The provider-prefixed issue id if `issueId` came from tracker intake, or
 * undefined for manually created sessions (whose issueId, if any, is a plain
 * task title with no provider prefix).
 */
export function canonicalTrackerIssueId(issueId?: string): string | undefined {
	if (!issueId) return undefined;
	return TRACKER_PROVIDER_PREFIXES.some((prefix) => issueId.startsWith(prefix)) ? issueId : undefined;
}

/** Glanceable worker status. Maps 1:1 to the accent colors in DESIGN.md. */
export type WorkerDisplayStatus =
	"working" | "needs_you" | "mergeable" | "ci_failed" | "no_signal" | "done" | "unknown";

export function workerDisplayStatus(session: WorkspaceSession): WorkerDisplayStatus {
	if (session.displayStatus) return session.displayStatus;
	switch (session.status) {
		case "needs_input":
		case "changes_requested":
		case "review_pending":
		// A stalled worker also needs a human — the daemon already tried to
		// auto-kill it, so this is either a fresh stall or a harness the
		// killer can't safely act on.
		case "stalled":
			return "needs_you";
		case "ci_failed":
			return "ci_failed";
		case "no_signal":
			return "no_signal";
		case "approved":
		case "mergeable":
			return "mergeable";
		case "merged":
		case "terminated":
			return "done";
		case "unknown":
			return "unknown";
		default:
			return "working";
	}
}

// Open PRs (actionable) sort above merged/closed; ties break by number.
const prStateRank: Record<PRState, number> = { open: 0, draft: 1, merged: 2, closed: 3 };

/** A session's PRs ordered actionable-first (open, draft, merged, closed). */
export function sortedPRs(session: WorkspaceSession): PullRequestFacts[] {
	return [...session.prs].sort((a, b) => prStateRank[a.state] - prStateRank[b.state] || a.number - b.number);
}

/** PRs still in flight (open or draft). */
export function openPRs(session: WorkspaceSession): PullRequestFacts[] {
	return session.prs.filter((pr) => pr.state === "open" || pr.state === "draft");
}

export function mergedPRCount(session: WorkspaceSession): number {
	return session.prs.filter((pr) => pr.state === "merged").length;
}

/** The highest-priority PR for compact one-line surfaces (board card, sidebar). */
export function primaryPR(session: WorkspaceSession): PullRequestFacts | undefined {
	return sortedPRs(session)[0];
}

export function isOrchestratorSession(session: WorkspaceSession): boolean {
	return session.kind === "orchestrator" || session.id.endsWith("-orchestrator");
}

/**
 * The project's LIVE orchestrator, if any. Terminated orchestrator rows stay in
 * the session list (the daemon returns all sessions, ordered by spawn number),
 * so an earlier dead orchestrator must not shadow a live one — its zellij
 * session is deleted and attaching to it dead-ends in an instant
 * "[process exited]". No live orchestrator → undefined, so the topbar offers
 * Spawn instead of navigating to a dead session.
 */
export function findProjectOrchestrator(
	workspaces: WorkspaceSummary[],
	projectId: string,
): WorkspaceSession | undefined {
	const workspace = workspaces.find((w) => w.id === projectId);
	return newestActiveOrchestrator(workspace?.sessions ?? []);
}

export function newestActiveOrchestrator(sessions: WorkspaceSession[]): WorkspaceSession | undefined {
	const active = sessions.filter((session) => isOrchestratorSession(session) && sessionIsActive(session));
	return active.reduce<WorkspaceSession | undefined>(
		(newest, session) => (!newest || sessionNewer(session, newest) ? session : newest),
		undefined,
	);
}

function sessionNewer(a: WorkspaceSession, b: WorkspaceSession): boolean {
	const aCreated = timestamp(a.createdAt);
	const bCreated = timestamp(b.createdAt);
	if (aCreated !== bCreated) return aCreated > bCreated;
	const aUpdated = timestamp(a.updatedAt);
	const bUpdated = timestamp(b.updatedAt);
	if (aUpdated !== bUpdated) return aUpdated > bUpdated;
	return a.id > b.id;
}

function timestamp(value?: string): number {
	if (!value) return 0;
	const parsed = Date.parse(value);
	return Number.isNaN(parsed) ? 0 : parsed;
}

export function workerSessions(sessions: WorkspaceSession[]): WorkspaceSession[] {
	return sessions.filter((s) => !isOrchestratorSession(s));
}

export function sessionIsActive(session: WorkspaceSession): boolean {
	return session.status !== "merged" && session.status !== "terminated";
}

export function sessionNeedsAttention(session: WorkspaceSession): boolean {
	return (
		session.status === "needs_input" ||
		session.status === "no_signal" ||
		session.status === "changes_requested" ||
		session.status === "review_pending" ||
		session.status === "ci_failed" ||
		session.status === "stalled"
	);
}

export const workerStatusLabel: Record<WorkerDisplayStatus, string> = {
	working: "working",
	needs_you: "needs you",
	mergeable: "mergeable",
	ci_failed: "ci failed",
	no_signal: "no signal",
	done: "done",
	unknown: "unknown",
};

/** Whether a status should breathe (alive/working). */
export function workerStatusPulses(status: WorkerDisplayStatus): boolean {
	return status === "working" || status === "needs_you";
}

/**
 * Kanban attention zone, ordered by human-action urgency — ported from
 * agent-orchestrator's getAttentionLevel (packages/web/src/lib/types.ts),
 * collapsed to its default "simple" set and rebound to reverbcode's
 * {@link SessionStatus}. The board groups sessions into these columns so the
 * highest-ROrI work (a one-click merge) sits leftmost.
 */
export type AttentionZone = "merge" | "action" | "pending" | "working" | "done";

/** Columns left→right, most-urgent first. "done" is the archive column. */
export const attentionZoneOrder: AttentionZone[] = ["merge", "action", "pending", "working", "done"];

export const attentionZoneLabel: Record<AttentionZone, string> = {
	merge: "Ready to merge",
	action: "Needs you",
	pending: "Pending",
	working: "Working",
	done: "Done",
};

export function attentionZone(session: WorkspaceSession): AttentionZone {
	switch (session.status) {
		// Terminal — archive.
		case "merged":
		case "terminated":
			return "done";
		// One click to clear — highest ROI, checked first.
		case "approved":
		case "mergeable":
			return "merge";
		// Agent waiting on a human (respond) or a problem to investigate (review);
		// agent-orchestrator collapses these into one "action" zone by default.
		case "needs_input":
		case "no_signal":
		case "ci_failed":
		case "changes_requested":
		// Stalled wins over whatever PR status was last recorded — approved by
		// design (see task-5-brief risk notes): a session that stopped
		// producing signal still needs a human even if its last-known PR state
		// looked fine. Do not "fix" this back to a PR-status branch.
		case "stalled":
			return "action";
		// Waiting on an external reviewer / CI — nothing to do right now.
		case "review_pending":
		case "pr_open":
		case "draft":
		case "unknown":
			return "pending";
		// Agents doing their thing — don't interrupt.
		case "working":
		case "idle":
		default:
			return "working";
	}
}

export type WorkspaceSummary = {
	id: string;
	name: string;
	path: string;
	type?: "main" | "worktree";
	orchestratorAgent?: AgentProvider;
	accentColor?: string;
	diff?: {
		additions: number;
		deletions: number;
	};
	/** Company this project is grouped under in the sidebar, if any. */
	companyId?: string;
	sessions: WorkspaceSession[];
};

/** A minimal company shape for grouping — decoupled from the api schema type. */
export type CompanyLike = { id: string; name: string };

/** Sidebar id prefix for a company's disclosure state, avoiding collisions
 * with project ids in the shared collapsedIds set. */
export const UNASSIGNED_COMPANY_ID = "unassigned";

export type WorkspaceCompanyGroup = {
	id: string;
	name: string;
	workspaces: WorkspaceSummary[];
};

/**
 * Buckets workspaces by their companyId, in company list order, with any
 * workspace whose companyId is unset or points at a company that no longer
 * exists falling into a trailing "Unassigned" group (only rendered when
 * non-empty). Callers should skip grouping entirely when `companies` is
 * empty — see Sidebar's no-companies fallback.
 */
export function groupWorkspacesByCompany(
	workspaces: WorkspaceSummary[],
	companies: CompanyLike[],
): WorkspaceCompanyGroup[] {
	const groups: WorkspaceCompanyGroup[] = companies.map((company) => ({
		id: company.id,
		name: company.name,
		workspaces: [],
	}));
	const groupById = new Map(groups.map((group) => [group.id, group]));
	const unassigned: WorkspaceSummary[] = [];
	for (const workspace of workspaces) {
		const group = workspace.companyId ? groupById.get(workspace.companyId) : undefined;
		if (group) {
			group.workspaces.push(workspace);
		} else {
			unassigned.push(workspace);
		}
	}
	if (unassigned.length > 0) {
		groups.push({ id: UNASSIGNED_COMPANY_ID, name: "Unassigned", workspaces: unassigned });
	}
	return groups;
}

export function orchestratorNeedsRestart(workspace: WorkspaceSummary, orchestrator?: WorkspaceSession): boolean {
	if (!orchestrator || !workspace.orchestratorAgent) return false;
	return orchestrator.provider !== workspace.orchestratorAgent;
}

export type OrchestratorHealth =
	| { state: "ok" }
	| { state: "restarting"; message: string }
	| { state: "restart_needed"; message: string }
	| { state: "missing"; message: string }
	| { state: "duplicates"; message: string };

export function orchestratorHealth(workspace: WorkspaceSummary, restarting = false): OrchestratorHealth {
	if (restarting) {
		return { state: "restarting", message: "Restarting orchestrator. New tasks wait until the replacement is ready." };
	}
	const active = workspace.sessions.filter((session) => isOrchestratorSession(session) && sessionIsActive(session));
	if (active.length > 1) {
		return {
			state: "duplicates",
			message: "Multiple orchestrators are active. The newest one is used; stale ones will be cleaned up on daemon reconcile.",
		};
	}
	const orchestrator = newestActiveOrchestrator(workspace.sessions);
	if (!orchestrator) {
		return { state: "missing", message: "No orchestrator is running for this project." };
	}
	if (orchestratorNeedsRestart(workspace, orchestrator)) {
		return {
			state: "restart_needed",
			message: `Configured orchestrator agent is ${workspace.orchestratorAgent}; running agent is ${orchestrator.provider}.`,
		};
	}
	return { state: "ok" };
}

export function toAgentProvider(provider?: string): AgentProvider {
	switch (provider) {
		case "claude-code":
		case "opencode":
		case "aider":
		case "grok":
		case "droid":
		case "amp":
		case "agy":
		case "crush":
		case "cursor":
		case "qwen":
		case "copilot":
		case "goose":
		case "auggie":
		case "continue":
		case "devin":
		case "cline":
		case "kimi":
		case "kiro":
		case "kilocode":
		case "vibe":
		case "pi":
		case "autohand":
		case "command":
			return provider;
		default:
			return "codex";
	}
}
