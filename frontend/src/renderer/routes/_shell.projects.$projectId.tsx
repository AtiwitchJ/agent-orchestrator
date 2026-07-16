import { createFileRoute } from "@tanstack/react-router";
import { SessionsBoard } from "../components/SessionsBoard";
import { Workboard } from "../components/Workboard";
import { useState } from "react";

export const Route = createFileRoute("/_shell/projects/$projectId")({
	component: ProjectBoardRoute,
});

function ProjectBoardRoute() {
	const { projectId } = Route.useParams();
	const [showSessions, setShowSessions] = useState(false);
	return (
		<div className="h-full min-h-0">
			{showSessions ? <SessionsBoard projectId={projectId} /> : <Workboard projectId={projectId} onShowSessions={() => setShowSessions(true)} />}
		</div>
	);
}
