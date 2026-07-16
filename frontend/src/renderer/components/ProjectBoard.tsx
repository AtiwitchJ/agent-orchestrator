import { useState } from "react";
import { SessionsBoard } from "./SessionsBoard";
import { Workboard } from "./Workboard";

export function ProjectBoard({ projectId }: { projectId: string }) {
	const [showSessions, setShowSessions] = useState(false);
	return (
		<div className="h-full min-h-0">
			{showSessions ? <SessionsBoard onShowWorkboard={() => setShowSessions(false)} projectId={projectId} /> : <Workboard projectId={projectId} onShowSessions={() => setShowSessions(true)} />}
		</div>
	);
}
