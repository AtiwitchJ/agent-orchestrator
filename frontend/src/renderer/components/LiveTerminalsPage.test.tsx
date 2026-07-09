import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { LiveTerminalsPage } from "./LiveTerminalsPage";
import type { WorkspaceSummary } from "../types/workspace";

const { navigateMock, searchMock, getMock } = vi.hoisted(() => ({
	navigateMock: vi.fn(),
	searchMock: vi.fn(),
	getMock: vi.fn(),
}));

vi.mock("@tanstack/react-router", async (importOriginal) => {
	const actual = await importOriginal<typeof import("@tanstack/react-router")>();
	return { ...actual, useNavigate: () => navigateMock, useSearch: () => searchMock() };
});

vi.mock("../lib/api-client", () => ({
	apiClient: { GET: getMock },
	apiErrorMessage: (error: unknown, fallback = "Request failed") => (error instanceof Error ? error.message : fallback),
	hasTrustedApiBaseUrl: () => true,
}));

vi.mock("./TerminalPane", () => ({ TerminalPane: () => <div>terminal body</div> }));

vi.mock("../lib/shell-context", () => ({
	useShell: () => ({ daemonStatus: { state: "ready" } }),
}));

const workspaces: WorkspaceSummary[] = [
	{
		id: "holding-hq",
		name: "holding-hq",
		path: "/hq/holding",
		sessions: [
			{
				id: "holding-hq-1",
				workspaceId: "holding-hq",
				workspaceName: "holding-hq",
				title: "holding-hq-1",
				provider: "claude-code",
				kind: "orchestrator",
				branch: "main",
				status: "working",
				updatedAt: "2026-07-09T00:00:00Z",
				prs: [],
			},
		],
	},
	{
		id: "qb-hq",
		name: "qb-hq",
		path: "/hq/qb",
		sessions: [
			{
				id: "qb-hq-1",
				workspaceId: "qb-hq",
				workspaceName: "qb-hq",
				title: "qb-hq-1",
				provider: "claude-code",
				kind: "orchestrator",
				branch: "main",
				status: "working",
				updatedAt: "2026-07-09T00:00:00Z",
				prs: [],
			},
		],
	},
];

function renderPage() {
	getMock.mockImplementation(async (path: string) => {
		if (path === "/api/v1/projects") return { data: { projects: workspaces.map((w) => ({ id: w.id, name: w.name, path: w.path })) }, error: undefined };
		if (path === "/api/v1/sessions")
			return { data: { sessions: workspaces.flatMap((w) => w.sessions.map((s) => ({ ...s, projectId: w.id, harness: s.provider, isTerminated: false }))) }, error: undefined };
		return { data: {}, error: undefined };
	});
	const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	render(
		<QueryClientProvider client={queryClient}>
			<LiveTerminalsPage />
		</QueryClientProvider>,
	);
}

describe("LiveTerminalsPage", () => {
	it("renders one tile per id in the sessions search param", async () => {
		searchMock.mockReturnValue({ sessions: "holding-hq-1,qb-hq-1" });
		renderPage();

		expect(await screen.findByText("holding-hq-1")).toBeInTheDocument();
		expect(screen.getByText("qb-hq-1")).toBeInTheDocument();
	});

	it("dedupes repeated ids in the sessions search param", async () => {
		searchMock.mockReturnValue({ sessions: "holding-hq-1,holding-hq-1" });
		renderPage();

		await screen.findByText("holding-hq-1");
		expect(screen.getAllByText("holding-hq-1")).toHaveLength(1);
	});

	it("removing a tile updates the sessions search param", async () => {
		searchMock.mockReturnValue({ sessions: "holding-hq-1,qb-hq-1" });
		const user = userEvent.setup();
		renderPage();

		await screen.findByText("holding-hq-1");
		await user.click(screen.getByLabelText("Remove holding-hq-1"));

		expect(navigateMock).toHaveBeenCalledWith({
			to: "/terminals",
			search: { sessions: "qb-hq-1" },
		});
	});

	it("adding a session via the picker updates the sessions search param", async () => {
		searchMock.mockReturnValue({ sessions: "holding-hq-1" });
		const user = userEvent.setup();
		renderPage();

		await screen.findByText("holding-hq-1");
		await user.click(screen.getByRole("combobox", { name: "Add a session" }));
		await user.click(await screen.findByRole("option", { name: /qb-hq-1/ }));

		expect(navigateMock).toHaveBeenCalledWith({
			to: "/terminals",
			search: { sessions: "holding-hq-1,qb-hq-1" },
		});
	});
});

describe("LiveTerminalsPage org-hierarchy tree", () => {
	const hierarchyWorkspaces: WorkspaceSummary[] = [
		{
			id: "holding-hq",
			name: "holding-hq",
			path: "/hq/holding",
			hqRole: "holding",
			sessions: [
				{
					id: "holding-hq-1",
					workspaceId: "holding-hq",
					workspaceName: "holding-hq",
					title: "holding-hq-1",
					provider: "claude-code",
					kind: "orchestrator",
					branch: "main",
					status: "working",
					updatedAt: "2026-07-09T00:00:00Z",
					prs: [],
				},
			],
		},
		{
			id: "qb-hq",
			name: "qb-hq",
			path: "/hq/qb",
			hqRole: "company",
			companyId: "qb",
			sessions: [
				{
					id: "qb-hq-1",
					workspaceId: "qb-hq",
					workspaceName: "qb-hq",
					title: "qb-hq-1",
					provider: "claude-code",
					kind: "orchestrator",
					branch: "main",
					status: "working",
					updatedAt: "2026-07-09T00:00:00Z",
					prs: [],
				},
			],
		},
		{
			id: "limbic-agentstation",
			name: "limbic-agentstation",
			path: "/repo/limbic",
			companyId: "qb",
			sessions: [
				{
					id: "limbic-agentstation-1",
					workspaceId: "limbic-agentstation",
					workspaceName: "limbic-agentstation",
					title: "limbic-agentstation-1",
					provider: "claude-code",
					kind: "worker",
					branch: "main",
					status: "working",
					updatedAt: "2026-07-09T00:00:00Z",
					prs: [],
				},
			],
		},
	];

	function renderHierarchyPage() {
		getMock.mockImplementation(async (path: string) => {
			if (path === "/api/v1/projects")
				return {
					data: {
						projects: hierarchyWorkspaces.map((w) => ({
							id: w.id,
							name: w.name,
							path: w.path,
							hqRole: w.hqRole,
							companyId: w.companyId,
						})),
					},
					error: undefined,
				};
			if (path === "/api/v1/sessions")
				return {
					data: {
						sessions: hierarchyWorkspaces.flatMap((w) =>
							w.sessions.map((s) => ({ ...s, projectId: w.id, harness: s.provider, isTerminated: false })),
						),
					},
					error: undefined,
				};
			return { data: {}, error: undefined };
		});
		const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
		render(
			<QueryClientProvider client={queryClient}>
				<LiveTerminalsPage />
			</QueryClientProvider>,
		);
	}

	it("labels the holding HQ tile CEO and the company HQ tile PM", async () => {
		searchMock.mockReturnValue({ sessions: "holding-hq-1,qb-hq-1,limbic-agentstation-1" });
		renderHierarchyPage();

		await screen.findByText("holding-hq-1");
		expect(screen.getByText("CEO")).toBeInTheDocument();
		expect(screen.getByText("PM")).toBeInTheDocument();
		expect(screen.getByText("limbic-agentstation-1")).toBeInTheDocument();
	});

	it("still renders every selected tile when the CEO is excluded (PM + worker only)", async () => {
		searchMock.mockReturnValue({ sessions: "qb-hq-1,limbic-agentstation-1" });
		renderHierarchyPage();

		await screen.findByText("qb-hq-1");
		expect(screen.getByText("PM")).toBeInTheDocument();
		expect(screen.getByText("limbic-agentstation-1")).toBeInTheDocument();
		expect(screen.queryByText("CEO")).not.toBeInTheDocument();
	});

	it("labels a worker-kind session as Worker", async () => {
		searchMock.mockReturnValue({ sessions: "qb-hq-1,limbic-agentstation-1" });
		renderHierarchyPage();

		await screen.findByText("limbic-agentstation-1");
		expect(screen.getByText("Worker")).toBeInTheDocument();
	});
});
