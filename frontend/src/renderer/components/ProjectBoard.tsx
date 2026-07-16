import { useState } from "react";
import { SessionMessagesPanel } from "./SessionMessagesPanel";
import { SessionsBoard } from "./SessionsBoard";
import { Workboard } from "./Workboard";

export function ProjectBoard({ projectId }: { projectId: string }) {
	const [showSessions, setShowSessions] = useState(false);
	return (
		<div className="flex h-full min-h-0">
			<div className="min-w-0 flex-1">
				{showSessions ? <SessionsBoard onShowWorkboard={() => setShowSessions(false)} projectId={projectId} /> : <Workboard projectId={projectId} onShowSessions={() => setShowSessions(true)} />}
			</div>
			<SessionMessagesPanel projectId={projectId} />
		</div>
	);
}
