import { createFileRoute } from "@tanstack/react-router";
import { ProjectBoard } from "../components/ProjectBoard";

export const Route = createFileRoute("/_shell/projects/$projectId")({
	component: ProjectBoardRoute,
});

function ProjectBoardRoute() {
	const { projectId } = Route.useParams();
	return <ProjectBoard projectId={projectId} />;
}
