import { Terminal } from "lucide-react";
import type { WorkCard as WorkboardCard } from "../hooks/useWorkboardQuery";
import type { Theme } from "../stores/ui-store";
import type { WorkspaceSession } from "../types/workspace";
import { TerminalPane } from "./TerminalPane";
import { Button } from "./ui/button";

const TERMINAL_FONT_SIZE = 12;

export function WorkCardFocusPanel({
	card,
	session,
	theme,
	daemonReady,
	onClose,
	onShowSessions,
}: {
	card: WorkboardCard;
	session?: WorkspaceSession;
	theme: Theme;
	daemonReady: boolean;
	onClose: () => void;
	onShowSessions?: () => void;
}) {
	const showTerminal = card.status === "running" && Boolean(card.sessionId && session);

	return (
		<aside aria-label={`Focus panel for ${card.title}`} className="flex h-full w-[360px] shrink-0 flex-col border-l border-border bg-surface">
			<div className="flex shrink-0 items-start gap-3 border-b border-border px-4 py-3">
				<div className="min-w-0 flex-1">
					<div className="font-mono text-[10px] font-semibold uppercase tracking-[0.08em] text-accent">Focused card</div>
					<h2 className="mt-1 line-clamp-2 text-[14px] font-medium leading-[1.35] text-foreground">{card.title}</h2>
				</div>
				<Button aria-label="Close card focus panel" onClick={onClose} size="icon-sm" variant="ghost">×</Button>
			</div>
			<div className="shrink-0 space-y-3 border-b border-border px-4 py-3">
				<div className="flex items-center justify-between gap-3 text-[11px]">
					<span className="text-muted-foreground">Status</span>
					<span className="font-mono uppercase tracking-[0.05em] text-foreground">{card.status}</span>
				</div>
				{card.sessionId ? (
					<div className="flex items-center gap-2 text-[11px] text-muted-foreground">
						<Terminal className="size-3.5 text-accent" aria-hidden="true" />
						<span className="truncate">{session ? `Session ${card.sessionId}` : "Linked session unavailable"}</span>
					</div>
				) : <p className="text-[11px] text-passive">No session is linked to this card yet.</p>}
				{card.sessionId && !showTerminal && onShowSessions ? <Button onClick={onShowSessions} size="sm" variant="outline">Open terminal</Button> : null}
			</div>
			{showTerminal ? (
				<div className="min-h-0 flex-1 p-3">
					<div className="h-full min-h-[260px] overflow-hidden rounded-md border border-border">
						<TerminalPane session={session} theme={theme} daemonReady={daemonReady} fontSize={TERMINAL_FONT_SIZE} />
					</div>
				</div>
			) : (
				<div className="flex flex-1 items-center justify-center px-8 text-center text-[12px] leading-[1.5] text-passive">
					{card.sessionId ? "Select a running card to preview its live terminal here." : "Select a card linked to a session to preview its terminal here."}
				</div>
			)}
		</aside>
	);
}
