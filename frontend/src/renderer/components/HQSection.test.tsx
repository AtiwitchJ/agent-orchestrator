import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { getMock, putMock, postMock, navigateMock } = vi.hoisted(() => ({
	getMock: vi.fn(),
	putMock: vi.fn(),
	postMock: vi.fn(),
	navigateMock: vi.fn(),
}));

vi.mock("@tanstack/react-router", async (importOriginal) => {
	const actual = await importOriginal<typeof import("@tanstack/react-router")>();
	return { ...actual, useNavigate: () => navigateMock };
});

vi.mock("../lib/api-client", () => ({
	apiClient: { GET: getMock, PUT: putMock, POST: postMock },
	apiErrorMessage: (error: unknown) => {
		if (error instanceof Error) return error.message;
		if (typeof error === "object" && error !== null && "message" in error) {
			return String((error as { message: unknown }).message);
		}
		return "Request failed";
	},
}));

import { HQSection } from "./HQSection";
import { workspaceQueryKey } from "../hooks/useWorkspaceQuery";
import type { WorkspaceSummary } from "../types/workspace";

function renderHQ(scope: { kind: "holding" } | { kind: "company"; companyId: string }, workspaces: WorkspaceSummary[]) {
	const queryClient = new QueryClient({
		defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
	});
	queryClient.setQueryData(workspaceQueryKey, workspaces);
	render(
		<QueryClientProvider client={queryClient}>
			<HQSection scope={scope} />
		</QueryClientProvider>,
	);
	return queryClient;
}

beforeEach(() => {
	getMock.mockReset();
	putMock.mockReset();
	postMock.mockReset();
	navigateMock.mockReset();
	// HQSection mounts useWorkspaceQuery, an active observer — any
	// queryClient.invalidateQueries() call in the component triggers a real
	// background refetch through this same mocked GET. Give it a safe default
	// so that refetch doesn't crash (destructuring `undefined`) and retry.
	getMock.mockResolvedValue({ data: { projects: [], sessions: [] }, error: undefined });
});

describe("HQSection — no HQ project yet", () => {
	it("shows a Create CEO HQ button for the holding scope, with no folder picker involved", () => {
		renderHQ({ kind: "holding" }, []);
		expect(screen.getByRole("button", { name: "Create CEO HQ" })).toBeInTheDocument();
	});

	it("shows a Create PM HQ button for a company scope", () => {
		renderHQ({ kind: "company", companyId: "acme" }, []);
		expect(screen.getByRole("button", { name: "Create PM HQ" })).toBeInTheDocument();
	});

	it("ignores an HQ project belonging to a different company", () => {
		const otherCompanyHQ: WorkspaceSummary = {
			id: "bravo-hq",
			name: "Bravo HQ",
			path: "/repo/bravo-hq",
			companyId: "bravo",
			hqRole: "company",
			sessions: [],
		};
		renderHQ({ kind: "company", companyId: "acme" }, [otherCompanyHQ]);
		expect(screen.getByRole("button", { name: "Create PM HQ" })).toBeInTheDocument();
	});

	it("auto-provisions the holding HQ (no path in the request) and starts the CEO orchestrator", async () => {
		postMock.mockImplementation(async (path: string) => {
			if (path === "/api/v1/org/holding-hq") {
				return { data: { projectId: "holding-hq" }, error: undefined };
			}
			if (path === "/api/v1/orchestrators") {
				return { data: { orchestrator: { id: "holding-hq-1" } }, error: undefined, response: { status: 201 } };
			}
			return { data: undefined, error: undefined };
		});
		renderHQ({ kind: "holding" }, []);

		await userEvent.click(screen.getByRole("button", { name: "Create CEO HQ" }));

		await waitFor(() => expect(postMock).toHaveBeenCalledWith("/api/v1/org/holding-hq"));
		await waitFor(() => expect(postMock).toHaveBeenCalledWith("/api/v1/orchestrators", { body: { projectId: "holding-hq", clean: false } }));
		await waitFor(
			() =>
				expect(navigateMock).toHaveBeenCalledWith({
					to: "/projects/$projectId/sessions/$sessionId",
					params: { projectId: "holding-hq", sessionId: "holding-hq-1" },
				}),
			{ timeout: 10000 },
		);
	});

	it("auto-provisions a company's PM HQ scoped to that company id", async () => {
		postMock.mockImplementation(async (path: string) => {
			if (path === "/api/v1/org/companies/{companyId}/hq") {
				return { data: { projectId: "acme-hq" }, error: undefined };
			}
			if (path === "/api/v1/orchestrators") {
				return { data: { orchestrator: { id: "acme-hq-1" } }, error: undefined, response: { status: 201 } };
			}
			return { data: undefined, error: undefined };
		});
		renderHQ({ kind: "company", companyId: "acme" }, []);

		await userEvent.click(screen.getByRole("button", { name: "Create PM HQ" }));

		await waitFor(
			() =>
				expect(navigateMock).toHaveBeenCalledWith({
					to: "/projects/$projectId/sessions/$sessionId",
					params: { projectId: "acme-hq", sessionId: "acme-hq-1" },
				}),
			{ timeout: 10000 },
		);
		expect(postMock).toHaveBeenCalledWith("/api/v1/org/companies/{companyId}/hq", {
			params: { path: { companyId: "acme" } },
		});
	});

	it("shows an error when auto-provisioning fails", async () => {
		postMock.mockImplementation(async (path: string) => {
			if (path === "/api/v1/org/holding-hq") {
				return { data: undefined, error: { message: "disk full" } };
			}
			return { data: undefined, error: undefined };
		});
		renderHQ({ kind: "holding" }, []);

		await userEvent.click(screen.getByRole("button", { name: "Create CEO HQ" }));

		expect(await screen.findByText("disk full")).toBeInTheDocument();
		expect(navigateMock).not.toHaveBeenCalled();
	});
});

describe("HQSection — HQ project exists", () => {
	const hqNoOrchestrator: WorkspaceSummary = {
		id: "acme-hq",
		name: "Acme HQ",
		path: "/repo/acme-hq",
		companyId: "acme",
		hqRole: "company",
		sessions: [],
	};

	const hqWithOrchestrator: WorkspaceSummary = {
		...hqNoOrchestrator,
		sessions: [
			{
				id: "acme-hq-1",
				workspaceId: "acme-hq",
				workspaceName: "Acme HQ",
				title: "orchestrator",
				provider: "claude-code",
				kind: "orchestrator",
				branch: "ao/acme-hq-orchestrator",
				status: "idle",
				createdAt: "2026-01-01T00:00:00Z",
				updatedAt: "2026-01-01T00:00:00Z",
				activity: { state: "idle", lastActivityAt: "2026-01-01T00:00:00Z" },
				prs: [],
			},
		],
	};

	function mockProjectConfig(heartbeat: { enabled?: boolean; interval?: string } = {}) {
		getMock.mockImplementation(async (path: string) => {
			if (path === "/api/v1/projects/{id}") {
				return { data: { status: "ok", project: { id: "acme-hq", config: { heartbeat } } }, error: undefined };
			}
			return { data: undefined, error: undefined };
		});
	}

	it("shows a Start PM button when no orchestrator is running", () => {
		mockProjectConfig();
		renderHQ({ kind: "company", companyId: "acme" }, [hqNoOrchestrator]);
		expect(screen.getByRole("button", { name: /Start PM/ })).toBeInTheDocument();
	});

	it("spawns the orchestrator and navigates on Start PM", async () => {
		mockProjectConfig();
		postMock.mockResolvedValue({
			data: { orchestrator: { id: "acme-hq-1" } },
			error: undefined,
			response: { status: 201 },
		});
		renderHQ({ kind: "company", companyId: "acme" }, [hqNoOrchestrator]);

		await userEvent.click(screen.getByRole("button", { name: /Start PM/ }));

		await waitFor(() => expect(postMock).toHaveBeenCalledWith("/api/v1/orchestrators", { body: { projectId: "acme-hq", clean: false } }));
		await waitFor(() =>
			expect(navigateMock).toHaveBeenCalledWith({
				to: "/projects/$projectId/sessions/$sessionId",
				params: { projectId: "acme-hq", sessionId: "acme-hq-1" },
			}),
		);
	});

	it("shows orchestrator status, Open terminal, and Replace when running", () => {
		mockProjectConfig();
		renderHQ({ kind: "company", companyId: "acme" }, [hqWithOrchestrator]);
		expect(screen.getByText(/acme-hq-1/)).toBeInTheDocument();
		expect(screen.getByText(/idle/)).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Open terminal" })).toBeInTheDocument();
		expect(screen.getByRole("button", { name: /Replace/ })).toBeInTheDocument();
	});

	it("reflects the loaded heartbeat config and toggles it", async () => {
		mockProjectConfig({ enabled: false, interval: "30m" });
		putMock.mockResolvedValue({ error: undefined });
		renderHQ({ kind: "company", companyId: "acme" }, [hqWithOrchestrator]);

		const checkbox = await screen.findByRole("checkbox");
		expect(checkbox).not.toBeChecked();

		await userEvent.click(checkbox);

		await waitFor(() =>
			expect(putMock).toHaveBeenCalledWith("/api/v1/projects/{id}/config", {
				params: { path: { id: "acme-hq" } },
				body: { config: { heartbeat: { enabled: true, interval: "30m" } } },
			}),
		);
	});
});
