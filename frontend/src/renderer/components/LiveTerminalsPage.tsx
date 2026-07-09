import { useNavigate, useSearch } from "@tanstack/react-router";
import { useShell } from "../lib/shell-context";
import { useUiStore } from "../stores/ui-store";
import { useWorkspaceQuery } from "../hooks/useWorkspaceQuery";
import { DashboardSubhead } from "./DashboardSubhead";
import { TerminalTile } from "./TerminalTile";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "./ui/select";

const TERMINAL_FONT_SIZE = 12;

function parseIds(sessions: string): string[] {
	return sessions ? sessions.split(",").filter(Boolean) : [];
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

	const allSessions = workspaces.flatMap((workspace) =>
		workspace.sessions.map((session) => ({ session, projectName: workspace.name })),
	);
	const findSession = (id: string) => allSessions.find((entry) => entry.session.id === id);
	const availableToAdd = allSessions.filter((entry) => !selectedIds.includes(entry.session.id));

	return (
		<div className="flex h-full min-h-0 flex-col bg-background text-foreground">
			<DashboardSubhead
				title="Live Terminals"
				subtitle="Watch and message multiple sessions at once"
				actions={
					availableToAdd.length > 0 ? (
						<Select
							value=""
							onValueChange={(id) => setIds([...selectedIds, id])}
						>
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
					<div className="grid grid-cols-1 gap-4 lg:grid-cols-2 xl:grid-cols-3">
						{selectedIds.map((id) => {
							const entry = findSession(id);
							return (
								<TerminalTile
									key={id}
									sessionId={id}
									session={entry?.session}
									projectName={entry?.projectName}
									theme={theme}
									daemonReady={daemonStatus.state === "ready"}
									fontSize={TERMINAL_FONT_SIZE}
									onRemove={() => setIds(selectedIds.filter((existing) => existing !== id))}
								/>
							);
						})}
					</div>
				)}
			</div>
		</div>
	);
}
