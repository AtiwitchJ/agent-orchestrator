import { useMutation } from "@tanstack/react-query";
import { X } from "lucide-react";
import { useState } from "react";
import { apiClient, apiErrorMessage } from "../lib/api-client";
import type { Theme } from "../stores/ui-store";
import type { WorkspaceSession } from "../types/workspace";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { TerminalPane } from "./TerminalPane";

export type TerminalTileProps = {
	sessionId: string;
	session?: WorkspaceSession;
	projectName?: string;
	/** Org-hierarchy role badge (e.g. "CEO", "PM") — omitted for plain project/worker tiles. */
	roleLabel?: string;
	theme: Theme;
	daemonReady: boolean;
	fontSize: number;
	onRemove: () => void;
};

// One pane in the Live Terminals grid: a real, interactive TerminalPane (typing
// goes straight into the PTY, same as the single-session view) plus a compose
// bar that queues a message via the daemon's existing send endpoint — the same
// primitive the org heartbeat uses to nudge orchestrators, exposed in the UI
// for the first time.
//
// Height is intentionally flexible (h-full + flex-1 on the terminal area, not a
// fixed px value): the tree layout in LiveTerminalsPage stretches a CEO/PM tile
// across the combined height of the rows nested under it, so this component
// must fill whatever height its container hands it rather than assuming one.
export function TerminalTile({
	sessionId,
	session,
	projectName,
	roleLabel,
	theme,
	daemonReady,
	fontSize,
	onRemove,
}: TerminalTileProps) {
	const [draft, setDraft] = useState("");
	const sendMutation = useMutation({
		mutationFn: async (message: string) => {
			const { data, error } = await apiClient.POST("/api/v1/sessions/{sessionId}/send", {
				params: { path: { sessionId } },
				body: { message },
			});
			if (error) throw new Error(apiErrorMessage(error, "Failed to send message"));
			return data;
		},
		onSuccess: () => setDraft(""),
	});

	return (
		<div className="flex h-full min-h-0 flex-col overflow-hidden rounded-lg border border-border bg-surface">
			<div className="flex shrink-0 items-center gap-2 border-b border-border px-3 py-2">
				{roleLabel && (
					<span className="shrink-0 rounded bg-accent/10 px-1.5 py-0.5 font-mono text-[10px] font-semibold uppercase leading-none tracking-[0.04em] text-accent">
						{roleLabel}
					</span>
				)}
				<span className="min-w-0 truncate text-[12px] font-semibold text-foreground">
					{projectName ?? sessionId}
				</span>
				<span className="min-w-0 truncate font-mono text-[11px] text-passive">{sessionId}</span>
				<button
					aria-label={`Remove ${sessionId}`}
					className="ml-auto grid size-5 shrink-0 place-items-center rounded-md text-passive transition-colors hover:bg-interactive-hover hover:text-foreground"
					onClick={onRemove}
					type="button"
				>
					<X className="size-3.5" aria-hidden="true" />
				</button>
			</div>
			{session ? (
				<>
					<div className="min-h-[220px] flex-1">
						<TerminalPane session={session} theme={theme} daemonReady={daemonReady} fontSize={fontSize} />
					</div>
					<form
						className="flex shrink-0 items-center gap-2 border-t border-border p-2"
						onSubmit={(event) => {
							event.preventDefault();
							const message = draft.trim();
							if (!message || sendMutation.isPending) return;
							sendMutation.mutate(message);
						}}
					>
						<Input
							aria-label={`Message ${sessionId}`}
							className="h-8 flex-1"
							disabled={sendMutation.isPending}
							onChange={(e) => setDraft(e.target.value)}
							placeholder="Send a message..."
							value={draft}
						/>
						<Button disabled={sendMutation.isPending || !draft.trim()} size="sm" type="submit">
							Send
						</Button>
					</form>
					{sendMutation.isError && (
						<p className="border-t border-border px-2 py-1.5 text-[11px] text-destructive">
							{sendMutation.error instanceof Error ? sendMutation.error.message : "Failed to send message"}
						</p>
					)}
				</>
			) : (
				<div className="flex flex-1 items-center justify-center p-6 text-center text-[12px] text-passive">
					Session no longer available
				</div>
			)}
		</div>
	);
}
