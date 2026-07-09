import { createFileRoute } from "@tanstack/react-router";
import { LiveTerminalsPage } from "../components/LiveTerminalsPage";

export type TerminalsSearch = { sessions: string };

export const Route = createFileRoute("/_shell/terminals")({
	validateSearch: (search: Record<string, unknown>): TerminalsSearch => ({
		sessions: typeof search.sessions === "string" ? search.sessions : "",
	}),
	component: LiveTerminalsPage,
});
