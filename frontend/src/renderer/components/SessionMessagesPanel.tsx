import { MessageSquare } from "lucide-react";
import { useProjectMessagesQuery, type SessionMessage } from "../hooks/useProjectMessagesQuery";
import { useWorkspaceQuery } from "../hooks/useWorkspaceQuery";
import { formatTimeCompact } from "../lib/format-time";

// Read-only timeline of durable agent-to-agent send facts (Task 3's message
// store) for a project. Lives beside the board rather than inside it — it's
// project-scoped, not session-scoped, so it doesn't fit any single kanban
// column. SSE (`session_message_created`, wired in lib/event-transport.ts)
// keeps it live; refetchInterval on the query is just the fallback.
export function SessionMessagesPanel({ projectId }: { projectId?: string }) {
	const messagesQuery = useProjectMessagesQuery(projectId);
	const workspaceQuery = useWorkspaceQuery();
	const workspace = workspaceQuery.data?.find((w) => w.id === projectId);

	const sessionLabel = (sessionId: string): string =>
		workspace?.sessions.find((session) => session.id === sessionId)?.title ?? sessionId;

	// Newest first: a live timeline reads top-down like a chat/log, not a
	// document — the most recent send is the one you care about right now.
	const messages = [...(messagesQuery.data ?? [])].sort(
		(a, b) => Date.parse(b.createdAt) - Date.parse(a.createdAt),
	);

	return (
		<aside className="flex w-[320px] shrink-0 flex-col border-l border-border bg-background">
			<div className="flex shrink-0 items-center gap-2 border-b border-border px-[15px] py-3">
				<MessageSquare aria-hidden="true" className="h-3.5 w-3.5 text-passive" />
				<h2 className="text-[12px] font-semibold uppercase tracking-[0.06em] text-passive">Messages</h2>
			</div>
			<div className="min-h-0 flex-1 overflow-y-auto p-2">
				{messagesQuery.isError ? (
					<p className="px-2 py-6 text-center text-[12px] text-passive">Could not load messages.</p>
				) : messages.length === 0 ? (
					<p className="px-2 py-6 text-center text-[12px] text-passive">No agent messages yet.</p>
				) : (
					<div className="flex flex-col gap-1.5">
						{messages.map((message) => (
							<MessageRow key={message.id} message={message} sessionLabel={sessionLabel} />
						))}
					</div>
				)}
			</div>
		</aside>
	);
}

function MessageRow({
	message,
	sessionLabel,
}: {
	message: SessionMessage;
	sessionLabel: (sessionId: string) => string;
}) {
	// A missing senderSessionId means the send came from outside any tracked
	// session — a human running `ao send` from the CLI, not another agent.
	const sender = message.senderSessionId ? sessionLabel(message.senderSessionId) : "you";
	const target = sessionLabel(message.targetSessionId);
	return (
		<div className="rounded-md border border-border bg-surface px-2.5 py-2">
			<div className="flex items-center gap-1.5 font-mono text-[10.5px] text-passive">
				<span className="min-w-0 truncate text-foreground">{sender}</span>
				<span aria-hidden="true">→</span>
				<span className="min-w-0 truncate text-foreground">{target}</span>
				<span className="ml-auto shrink-0">{formatTimeCompact(message.createdAt)}</span>
			</div>
			<p className="mt-1 whitespace-pre-wrap break-words text-[12px] leading-5 text-muted-foreground">
				{message.content}
			</p>
		</div>
	);
}
