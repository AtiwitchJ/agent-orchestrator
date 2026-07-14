import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

const { navigateMock, getMock } = vi.hoisted(() => ({ navigateMock: vi.fn(), getMock: vi.fn() }));

vi.mock("@tanstack/react-router", async (importOriginal) => {
	const actual = await importOriginal<typeof import("@tanstack/react-router")>();
	return { ...actual, useNavigate: () => navigateMock, createFileRoute: () => (opts: unknown) => opts };
});

vi.mock("../lib/api-client", () => ({
	apiClient: { GET: getMock, POST: vi.fn() },
	apiErrorMessage: (error: unknown, fallback = "Request failed") => (error instanceof Error ? error.message : fallback),
	hasTrustedApiBaseUrl: () => true,
}));

vi.mock("../components/MigrationPopup", () => ({ MigrationPopup: () => null }));
vi.mock("../components/HQSection", () => ({ HQSection: () => null }));
vi.mock("../components/HeartbeatPauseSwitch", () => ({ HeartbeatPauseSwitch: () => null }));

import { CEODashboard } from "./_shell.index";

function renderDashboard() {
	getMock.mockImplementation(async (path: string) => {
		if (path === "/api/v1/companies")
			return { data: { companies: [{ id: "qb", name: "qb", createdAt: "2026-07-09T00:00:00Z" }] }, error: undefined };
		if (path === "/api/v1/projects")
			return { data: { projects: [{ id: "limbic-agentstation", name: "limbic-agentstation", path: "/x", companyId: "qb" }] }, error: undefined };
		if (path === "/api/v1/sessions") return { data: { sessions: [] }, error: undefined };
		if (path === "/api/v1/org/overview")
			return {
				data: {
					overview: {
						holdingHq: { projectId: "holding-hq", orchestratorSessionId: "holding-hq-1" },
						companies: [
							{
								id: "qb",
								name: "qb",
								hq: { projectId: "qb-hq", orchestratorSessionId: "qb-hq-1" },
								projects: [{ id: "limbic-agentstation", name: "limbic-agentstation", kind: "workspace", orchestratorSessionId: "limbic-agentstation-1", activeSessions: 1, totalSessions: 1 }],
							},
						],
						paused: false,
					},
				},
				error: undefined,
			};
		return { data: {}, error: undefined };
	});
	const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	render(
		<QueryClientProvider client={queryClient}>
			<CEODashboard />
		</QueryClientProvider>,
	);
}

describe("CEODashboard Watch Live", () => {
	it("navigates to /terminals pre-filled with CEO HQ, that company's PM HQ, and its worker orchestrator", async () => {
		const user = userEvent.setup();
		renderDashboard();

		await user.click(await screen.findByLabelText("Watch qb live"));

		await waitFor(() =>
			expect(navigateMock).toHaveBeenCalledWith({
				to: "/terminals",
				search: { sessions: "holding-hq-1,qb-hq-1,limbic-agentstation-1" },
			}),
		);
	});
});
