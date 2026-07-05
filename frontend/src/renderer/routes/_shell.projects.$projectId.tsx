import { createFileRoute } from "@tanstack/react-router";
import { SessionMessagesPanel } from "../components/SessionMessagesPanel";
import { SessionsBoard } from "../components/SessionsBoard";

export const Route = createFileRoute("/_shell/projects/$projectId")({
	component: ProjectBoardRoute,
});

function ProjectBoardRoute() {
	const { projectId } = Route.useParams();
	return (
		<div className="flex h-full min-h-0">
			<div className="min-w-0 flex-1">
				<SessionsBoard projectId={projectId} />
			</div>
			<SessionMessagesPanel projectId={projectId} />
		</div>
	);
}
