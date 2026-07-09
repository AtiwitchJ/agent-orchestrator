import { useNavigate, useSearch } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { useShell } from "../lib/shell-context";
import { useUiStore } from "../stores/ui-store";
import { useWorkspaceQuery } from "../hooks/useWorkspaceQuery";
import type { WorkspaceSession } from "../types/workspace";
import { DashboardSubhead } from "./DashboardSubhead";
import { TerminalTile } from "./TerminalTile";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "./ui/select";

const TERMINAL_FONT_SIZE = 12;
// A single tile's height in pixels, and the gap between tiles (matches the
// `gap-4` = 1rem Tailwind class used throughout this layout). Every tile gets
// an explicit height computed from these two constants — never a flex-
// stretched "however tall the sibling is" height — because xterm.js needs a
// concrete, stable pixel height when it measures/fits; an indefinite height
// during an intermediate layout pass can crash its resize handling.
const TILE_HEIGHT = 280;
const TILE_GAP = 16;

function rowsFor(childCount: number): number {
	return childCount === 0 ? 1 : Math.ceil(childCount / 2);
}

// The height a lead tile (CEO or PM) must be to visually span its children:
// however many rows those children occupy in the 2-column grid beside it.
function heightForChildren(childCount: number): number {
	const rows = rowsFor(childCount);
	return rows * TILE_HEIGHT + (rows - 1) * TILE_GAP;
}

type SessionEntry = {
	session: WorkspaceSession;
	projectName: string;
	companyId?: string;
	hqRole?: string;
};

type TileRef = { id: string; entry?: SessionEntry };

type Group = {
	key: string;
	lead?: TileRef;
	children: TileRef[];
};

function parseIds(sessions: string): string[] {
	return sessions ? [...new Set(sessions.split(",").filter(Boolean))] : [];
}

// Splits the selected tiles into an org-hierarchy tree: the CEO tile (if
// selected) on one side, and one row per company/project group on the other —
// each row's "lead" (a company PM, or failing that a plain project's own
// orchestrator) on the left with its children (workers, or any other session
// in that group) stacked beside it. This mirrors the CEO -> PM -> Worker shape
// directly instead of a flat, unordered grid.
function buildTree(ids: string[], findSession: (id: string) => SessionEntry | undefined) {
	const refs: TileRef[] = ids.map((id) => ({ id, entry: findSession(id) }));
	const ceo = refs.find((ref) => ref.entry?.hqRole === "holding");
	const rest = refs.filter((ref) => ref !== ceo);

	const groups = new Map<string, Group>();
	for (const ref of rest) {
		const key = ref.entry?.companyId ?? ref.entry?.session.workspaceId ?? ref.id;
		const group = groups.get(key) ?? { key, children: [] };
		const isLead = ref.entry?.hqRole === "company" || (!group.lead && ref.entry?.session.kind === "orchestrator");
		if (isLead && !group.lead) {
			group.lead = ref;
		} else {
			group.children.push(ref);
		}
		groups.set(key, group);
	}

	return { ceo, groups: [...groups.values()] };
}

export function LiveTerminalsPage() {
	const navigate = useNavigate();
	const { sessions } = useSearch({ from: "/_shell/terminals" });
	const { daemonStatus } = useShell();
	const theme = useUiStore((s) => s.theme);
	const workspaceQuery = useWorkspaceQuery();
	const workspaces = workspaceQuery.data ?? [];

	const selectedIds = parseIds(sessions);
	const setIds = (ids: string[]) => void navigate({ to: "/terminals", search: { sessions: ids.join(",") } });
	const removeId = (id: string) => setIds(selectedIds.filter((existing) => existing !== id));

	const allSessions: SessionEntry[] = workspaces.flatMap((workspace) =>
		workspace.sessions.map((session) => ({
			session,
			projectName: workspace.name,
			companyId: workspace.companyId,
			hqRole: workspace.hqRole,
		})),
	);
	const findSession = (id: string) => allSessions.find((entry) => entry.session.id === id);
	const availableToAdd = allSessions.filter((entry) => !selectedIds.includes(entry.session.id));

	const { ceo, groups } = buildTree(selectedIds, findSession);

	const roleLabelFor = (ref: TileRef, isLead: boolean): string | undefined => {
		if (ref.entry?.hqRole === "holding") return "CEO";
		if (ref.entry?.hqRole === "company") return "PM";
		if (isLead) return undefined;
		if (ref.entry?.session.kind === "worker") return "Worker";
		return undefined;
	};

	const renderTile = (ref: TileRef, height: number, roleLabel?: string) => {
		// The CEO tile is a singleton with no ambiguity to disambiguate — show
		// "CEO" as its name directly instead of the raw "holding-hq" project
		// slug, and skip the badge so the name isn't shown twice. PM/Worker tiles
		// keep their real project name (there can be several) plus the badge.
		const isCeo = roleLabel === "CEO";
		return (
			<TerminalTile
				key={ref.id}
				sessionId={ref.id}
				session={ref.entry?.session}
				projectName={isCeo ? "CEO" : ref.entry?.projectName}
				roleLabel={isCeo ? undefined : roleLabel}
				theme={theme}
				daemonReady={daemonStatus.state === "ready"}
				fontSize={TERMINAL_FONT_SIZE}
				height={height}
				onRemove={() => removeId(ref.id)}
			/>
		);
	};

	// The one shape used at every level of the tree: a "lead" tile on the left
	// at a fixed width, a 2-column grid of its children to the right. Applied
	// twice — CEO leading a grid of company cells, and (inside each cell) a
	// PM/project-lead leading a grid of its own workers. The lead's explicit
	// height (computed by the caller via heightForChildren) is what makes it
	// visually span its children — not flex stretch — so no special-casing is
	// needed for "how many levels are actually selected".
	const renderLeadRow = (lead: ReactNode | undefined, items: ReactNode[]): ReactNode => {
		if (!lead && items.length === 0) return null;
		return (
			<div className="flex gap-4">
				{lead && <div className="w-[300px] shrink-0">{lead}</div>}
				{items.length > 0 && <div className="grid flex-1 grid-cols-2 content-start gap-4">{items}</div>}
			</div>
		);
	};

	const renderGroupCell = (group: Group) => (
		<div key={group.key}>
			{renderLeadRow(
				group.lead ? renderTile(group.lead, heightForChildren(group.children.length), roleLabelFor(group.lead, true)) : undefined,
				group.children.map((child) => renderTile(child, TILE_HEIGHT, roleLabelFor(child, false))),
			)}
		</div>
	);

	const treeHeight =
		groups.length === 0 ? TILE_HEIGHT : groups.reduce((sum, g) => sum + heightForChildren(g.children.length), 0) + (groups.length - 1) * TILE_GAP;

	return (
		<div className="flex h-full min-h-0 flex-col bg-background text-foreground">
			<DashboardSubhead
				title="Live Terminals"
				subtitle="Watch and message multiple sessions at once"
				actions={
					availableToAdd.length > 0 ? (
						<Select value="" onValueChange={(id) => setIds([...selectedIds, id])}>
							<SelectTrigger aria-label="Add a session" className="h-8 w-56 text-[13px]">
								<SelectValue placeholder="Add a session..." />
							</SelectTrigger>
							<SelectContent position="popper" align="end">
								{availableToAdd.map(({ session, projectName }) => (
									<SelectItem key={session.id} value={session.id}>
										{projectName} — {session.title}
									</SelectItem>
								))}
							</SelectContent>
						</Select>
					) : undefined
				}
			/>
			<div className="min-h-0 flex-1 overflow-y-auto p-[18px]">
				{selectedIds.length === 0 ? (
					<div className="flex h-full items-center justify-center text-passive">
						No sessions selected — use "Add a session" above.
					</div>
				) : (
					renderLeadRow(
						ceo ? renderTile(ceo, treeHeight, "CEO") : undefined,
						groups.map((group) => renderGroupCell(group)),
					)
				)}
			</div>
		</div>
	);
}
