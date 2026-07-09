import { SidebarProvider } from "@/components/ui/sidebar";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { CreateProjectFlow, Sidebar } from "./Sidebar";
import type { WorkspaceSession, WorkspaceSummary } from "../types/workspace";
import { agentsQueryKey } from "../hooks/useAgentsQuery";
import { companiesQueryKey, type Company } from "../hooks/useCompaniesQuery";

const { getMock, navigateMock, mockParams, renameSessionMock } = vi.hoisted(() => ({
	getMock: vi.fn(),
	navigateMock: vi.fn(),
	mockParams: { projectId: undefined as string | undefined },
	renameSessionMock: vi.fn().mockResolvedValue(undefined),
}));

vi.mock("../lib/rename-session", () => ({ renameSession: renameSessionMock }));

vi.mock("@tanstack/react-router", async (importOriginal) => {
	const actual = await importOriginal<typeof import("@tanstack/react-router")>();
	return {
		...actual,
		useNavigate: () => navigateMock,
		useParams: () => ({}),
		useRouterState: ({ select }: { select: (state: { location: { pathname: string } }) => unknown }) =>
			select({ location: { pathname: "/" } }),
	};
});

vi.mock("../lib/api-client", () => ({
	apiClient: { GET: getMock },
	apiErrorMessage: (error: unknown) => {
		if (error instanceof Error) return error.message;
		if (typeof error === "object" && error !== null && "message" in error && typeof error.message === "string") {
			return error.message;
		}
		return "Request failed";
	},
}));

const workspace: WorkspaceSummary = {
	id: "proj-1",
	name: "Project One",
	path: "/repo/project-one",
	sessions: [],
};

const session: WorkspaceSession = {
	id: "proj-1-1",
	workspaceId: "proj-1",
	workspaceName: "Project One",
	title: "fix login",
	provider: "claude-code",
	kind: "worker",
	branch: "session/proj-1-1",
	status: "working",
	updatedAt: "2026-06-30T00:00:00Z",
	prs: [],
};

type CreateProjectHandler = (input: { path: string; workerAgent: string; orchestratorAgent: string }) => Promise<void>;
type RemoveProjectHandler = (projectId: string) => Promise<void>;

function renderSidebar({
	companies,
	defaultOpen = true,
	onCreateProject = vi.fn().mockResolvedValue(undefined) as CreateProjectHandler,
	onRemoveProject = vi.fn().mockResolvedValue(undefined) as RemoveProjectHandler,
	seedAgents = true,
	workspaces = [workspace],
}: {
	companies?: Company[];
	defaultOpen?: boolean;
	onCreateProject?: CreateProjectHandler;
	onRemoveProject?: RemoveProjectHandler;
	seedAgents?: boolean;
	workspaces?: WorkspaceSummary[];
} = {}) {
	const queryClient = new QueryClient({
		defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
	});
	if (seedAgents) {
		queryClient.setQueryData(agentsQueryKey, {
			supported: [
				{ id: "claude-code", label: "Claude Code" },
				{ id: "codex", label: "Codex" },
			],
			installed: [
				{ id: "claude-code", label: "Claude Code" },
				{ id: "codex", label: "Codex" },
			],
			authorized: [
				{ id: "claude-code", label: "Claude Code", authStatus: "authorized" },
				{ id: "codex", label: "Codex", authStatus: "authorized" },
			],
		});
	}
	if (companies) {
		queryClient.setQueryData(companiesQueryKey, companies);
	}
	render(
		<QueryClientProvider client={queryClient}>
			<SidebarProvider defaultOpen={defaultOpen}>
				<Sidebar
					daemonStatus={{ state: "running" }}
					onCreateProject={onCreateProject}
					onRemoveProject={onRemoveProject}
					workspaces={workspaces}
				/>
			</SidebarProvider>
		</QueryClientProvider>,
	);
	return onRemoveProject;
}

async function chooseOption(trigger: HTMLElement, optionName: string) {
	await userEvent.click(trigger);
	await userEvent.click(await screen.findByRole("option", { name: optionName }));
}

// Sidebar's useCompaniesQuery fires apiClient.GET("/api/v1/companies") on
// every mount, alongside whatever endpoint a given test is exercising (e.g.
// the agents catalog). getMock is a single jest.fn shared across every
// apiClient.GET call, so it must route by path — otherwise a company fetch
// silently consumes a mockResolvedValueOnce/mockReturnValueOnce meant for a
// different endpoint and the test observes the wrong response.
const DEFAULT_AGENT_CATALOG_RESPONSE = {
	data: {
		supported: [
			{ id: "claude-code", label: "Claude Code" },
			{ id: "codex", label: "Codex" },
		],
		installed: [
			{ id: "claude-code", label: "Claude Code" },
			{ id: "codex", label: "Codex" },
		],
		authorized: [
			{ id: "claude-code", label: "Claude Code", authStatus: "authorized" },
			{ id: "codex", label: "Codex", authStatus: "authorized" },
		],
	},
	error: undefined,
};

beforeEach(() => {
	getMock.mockReset();
	getMock.mockImplementation(async (path: string) => {
		if (path === "/api/v1/companies") return { data: { companies: [] }, error: undefined };
		return DEFAULT_AGENT_CATALOG_RESPONSE;
	});
	navigateMock.mockReset();
	renameSessionMock.mockReset().mockResolvedValue(undefined);
	mockParams.projectId = undefined;
	vi.spyOn(window, "confirm").mockReturnValue(true);
	vi.spyOn(window, "alert").mockImplementation(() => undefined);
});

afterEach(() => {
	vi.restoreAllMocks();
});

describe("Sidebar", () => {
	it("confirms project removal before calling the remove handler", async () => {
		const user = userEvent.setup();
		const onRemoveProject = renderSidebar();

		await user.click(screen.getByLabelText("Project actions for Project One"));
		await user.click(await screen.findByRole("menuitem", { name: "Remove project" }));

		expect(window.confirm).toHaveBeenCalledWith(
			"Remove project Project One? This stops its live sessions and removes it from the sidebar, but keeps the repository folder and stored history on disk.",
		);
		await waitFor(() => expect(onRemoveProject).toHaveBeenCalledTimes(1));
	});

	it("does not remove the project when confirmation is cancelled", async () => {
		vi.mocked(window.confirm).mockReturnValue(false);
		const user = userEvent.setup();
		const onRemoveProject = renderSidebar();

		await user.click(screen.getByLabelText("Project actions for Project One"));
		await user.click(await screen.findByRole("menuitem", { name: "Remove project" }));

		expect(onRemoveProject).not.toHaveBeenCalled();
	});

	it("reveals dashboard and orchestrator buttons alongside the kebab on the project row", () => {
		renderSidebar();

		expect(screen.getByLabelText("Open Project One dashboard")).toBeInTheDocument();
		expect(screen.getByLabelText("Spawn Project One orchestrator")).toBeInTheDocument();
		expect(screen.getByLabelText("Project actions for Project One")).toBeInTheDocument();
	});

	it("navigates to the project board when the dashboard button is clicked", async () => {
		const user = userEvent.setup();
		renderSidebar();

		await user.click(screen.getByLabelText("Open Project One dashboard"));

		expect(navigateMock).toHaveBeenCalledWith({ to: "/projects/$projectId", params: { projectId: "proj-1" } });
	});

	it("requires explicit worker and orchestrator agents when creating a project", async () => {
		const user = userEvent.setup();
		const onCreateProject = vi.fn().mockResolvedValue(undefined) as CreateProjectHandler;
		window.ao!.app.chooseDirectory = vi.fn().mockResolvedValue("/repo/new-project");
		renderSidebar({ onCreateProject });

		await user.click(screen.getByLabelText("New project"));

		expect(await screen.findByText("/repo/new-project")).toBeInTheDocument();
		const dialog = screen.getByRole("dialog", { name: "Project agents" });
		expect(dialog).toHaveClass("left-1/2", "top-1/2", "-translate-x-1/2", "-translate-y-1/2");
		await chooseOption(screen.getByRole("combobox", { name: "Worker agent" }), "Codex");
		await chooseOption(screen.getByRole("combobox", { name: "Orchestrator agent" }), "Claude Code");
		await user.click(screen.getByRole("button", { name: "Create and start" }));

		await waitFor(() =>
			expect(onCreateProject).toHaveBeenCalledWith({
				path: "/repo/new-project",
				workerAgent: "codex",
				orchestratorAgent: "claude-code",
			}),
		);
	});

	it("shows needs-auth agents as unavailable while keeping authorized agents selectable", async () => {
		const user = userEvent.setup();
		const onCreateProject = vi.fn().mockResolvedValue(undefined) as CreateProjectHandler;
		window.ao!.app.chooseDirectory = vi.fn().mockResolvedValue("/repo/new-project");
		getMock.mockImplementation(async (path: string) => {
			if (path === "/api/v1/companies") return { data: { companies: [] }, error: undefined };
			return {
				data: {
					supported: [
						{ id: "claude-code", label: "Claude Code" },
						{ id: "cursor", label: "Cursor" },
						{ id: "aider", label: "Aider" },
					],
					installed: [
						{ id: "claude-code", label: "Claude Code", authStatus: "authorized" },
						{ id: "cursor", label: "Cursor", authStatus: "unauthorized" },
					],
					authorized: [{ id: "claude-code", label: "Claude Code", authStatus: "authorized" }],
				},
				error: undefined,
			};
		});
		renderSidebar({ onCreateProject, seedAgents: false });

		await user.click(screen.getByLabelText("New project"));
		expect(await screen.findByText("/repo/new-project")).toBeInTheDocument();

		await user.click(screen.getByRole("combobox", { name: "Worker agent" }));
		const options = await screen.findAllByRole("option");
		expect(options.map((option) => option.textContent)).toEqual([
			"Claude Code",
			"CursorNeeds auth",
			"AiderNeeds install",
		]);
		expect(options[1]).toHaveAttribute("aria-disabled", "true");
		expect(options[2]).toHaveAttribute("aria-disabled", "true");
		await user.keyboard("{Escape}");

		await chooseOption(screen.getByRole("combobox", { name: "Worker agent" }), "Claude Code");
		await chooseOption(screen.getByRole("combobox", { name: "Orchestrator agent" }), "Claude Code");
		await user.click(screen.getByRole("button", { name: "Create and start" }));

		await waitFor(() =>
			expect(onCreateProject).toHaveBeenCalledWith(expect.objectContaining({ workerAgent: "claude-code" })),
		);
	});

	it("updates project agent options when the catalog loads after the dialog opens", async () => {
		const user = userEvent.setup();
		const onCreateProject = vi.fn().mockResolvedValue(undefined) as CreateProjectHandler;
		window.ao!.app.chooseDirectory = vi.fn().mockResolvedValue("/repo/new-project");
		let resolveAgents!: (value: {
			data: {
				supported: { id: string; label: string }[];
				installed: { id: string; label: string }[];
				authorized: { id: string; label: string; authStatus: "authorized" }[];
			};
			error: undefined;
		}) => void;
		getMock.mockImplementation((path: string) => {
			if (path === "/api/v1/companies") return Promise.resolve({ data: { companies: [] }, error: undefined });
			return new Promise((resolve) => {
				resolveAgents = resolve;
			});
		});
		renderSidebar({ onCreateProject, seedAgents: false });

		await user.click(screen.getByLabelText("New project"));
		expect(await screen.findByText("/repo/new-project")).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Create and start" })).toBeDisabled();

		resolveAgents({
			data: {
				supported: [
					{ id: "claude-code", label: "Claude Code" },
					{ id: "codex", label: "Codex" },
				],
				installed: [
					{ id: "claude-code", label: "Claude Code" },
					{ id: "codex", label: "Codex" },
				],
				authorized: [
					{ id: "claude-code", label: "Claude Code", authStatus: "authorized" },
					{ id: "codex", label: "Codex", authStatus: "authorized" },
				],
			},
			error: undefined,
		});

		await chooseOption(screen.getByRole("combobox", { name: "Worker agent" }), "Codex");
		await chooseOption(screen.getByRole("combobox", { name: "Orchestrator agent" }), "Claude Code");
		await user.click(screen.getByRole("button", { name: "Create and start" }));

		await waitFor(() =>
			expect(onCreateProject).toHaveBeenCalledWith({
				path: "/repo/new-project",
				workerAgent: "codex",
				orchestratorAgent: "claude-code",
			}),
		);
	});

	it("renames a session inline and persists via the daemon", async () => {
		const user = userEvent.setup();
		const workspaceWithSession = { ...workspace, sessions: [session] };
		renderSidebar({ workspaces: [workspaceWithSession] });

		await user.click(screen.getByLabelText("Rename fix login"));
		const input = screen.getByLabelText("Rename fix login");
		await user.clear(input);
		await user.type(input, "polish login{Enter}");

		await waitFor(() => expect(renameSessionMock).toHaveBeenCalledWith("proj-1-1", "polish login"));
	});

	it("caps the inline rename input at 20 characters", async () => {
		const user = userEvent.setup();
		const workspaceWithSession = { ...workspace, sessions: [session] };
		renderSidebar({ workspaces: [workspaceWithSession] });

		await user.click(screen.getByLabelText("Rename fix login"));
		expect(screen.getByLabelText("Rename fix login")).toHaveAttribute("maxlength", "20");
	});

	it("cancels the inline rename on Escape without calling the daemon", async () => {
		const user = userEvent.setup();
		const workspaceWithSession = { ...workspace, sessions: [session] };
		renderSidebar({ workspaces: [workspaceWithSession] });

		await user.click(screen.getByLabelText("Rename fix login"));
		const input = screen.getByLabelText("Rename fix login");
		await user.clear(input);
		await user.type(input, "discard me{Escape}");

		expect(renameSessionMock).not.toHaveBeenCalled();
		expect(screen.getByLabelText("Open fix login")).toBeInTheDocument();
	});

	it("always shows action icons and reserves padding for them", () => {
		renderSidebar();

		const projectRow = screen.getByText("Project One").closest("button");

		if (!projectRow) throw new Error("Project row button not found");
		// Padding is always reserved for the action cluster (not hover-gated)
		expect(projectRow).toHaveClass("pr-[84px]");
	});

	it("renders a flat project list with no company headers when no companies exist (no regression)", () => {
		renderSidebar({ companies: [] });

		expect(screen.getByText("Project One")).toBeInTheDocument();
		expect(screen.queryByText("Unassigned")).not.toBeInTheDocument();
		// Dashboard/orchestrator/kebab actions still work exactly as before grouping existed.
		expect(screen.getByLabelText("Open Project One dashboard")).toBeInTheDocument();
	});

	it("groups projects under their company, with unassigned projects trailing", () => {
		const assigned: WorkspaceSummary = { ...workspace, companyId: "co-1" };
		const other: WorkspaceSummary = {
			id: "proj-2",
			name: "Project Two",
			path: "/repo/project-two",
			sessions: [],
		};
		renderSidebar({
			companies: [{ id: "co-1", name: "OPEN-UPPU", createdAt: "2026-01-01T00:00:00Z" }],
			workspaces: [assigned, other],
		});

		expect(screen.getByText("OPEN-UPPU")).toBeInTheDocument();
		expect(screen.getByText("Unassigned")).toBeInTheDocument();
		expect(screen.getByText("Project One")).toBeInTheDocument();
		expect(screen.getByText("Project Two")).toBeInTheDocument();
	});

	it("navigates to Live Terminals from the settings menu", async () => {
		const user = userEvent.setup();
		renderSidebar();

		// Both the expanded and icon-rail Settings triggers exist in the DOM
		// simultaneously (the icon-rail one is only Tailwind-`hidden`, which jsdom
		// doesn't compute) — the expanded variant renders first, so index [0] is it.
		await user.click(screen.getAllByLabelText("Settings")[0]);
		await user.click(await screen.findByRole("menuitem", { name: "Live Terminals" }));

		expect(navigateMock).toHaveBeenCalledWith({ to: "/terminals", search: { sessions: "" } });
	});

	it("does not render company headers in the icon rail", () => {
		renderSidebar({
			companies: [{ id: "co-1", name: "OPEN-UPPU", createdAt: "2026-01-01T00:00:00Z" }],
			defaultOpen: false,
			workspaces: [{ ...workspace, companyId: "co-1" }],
		});

		// Collapsed to the icon rail: group headers would be dead chrome above
		// the letter-tile column, so CompanyGroup skips them and falls back to a
		// flat stack of ProjectItems, same as the no-companies case.
		expect(screen.queryByText("OPEN-UPPU")).not.toBeInTheDocument();
		expect(screen.getByText("P")).toBeInTheDocument();
	});
});

// CreateProjectFlow is only ever mounted directly (e.g.
// _shell.companies.$companyId.tsx), not from inside Sidebar itself — Sidebar
// accepts an onCreateProject prop but never renders a trigger for it. These
// tests mount it the way the app actually does.
describe("CreateProjectFlow", () => {
	function renderCreateProjectFlow(onCreateProject: CreateProjectHandler) {
		const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
		queryClient.setQueryData(agentsQueryKey, {
			supported: [
				{ id: "claude-code", label: "Claude Code" },
				{ id: "codex", label: "Codex" },
			],
			installed: [
				{ id: "claude-code", label: "Claude Code" },
				{ id: "codex", label: "Codex" },
			],
			authorized: [
				{ id: "claude-code", label: "Claude Code", authStatus: "authorized" },
				{ id: "codex", label: "Codex", authStatus: "authorized" },
			],
		});
		render(
			<QueryClientProvider client={queryClient}>
				<CreateProjectFlow onCreateProject={onCreateProject}>
					{({ disabled, choosePath, label }) => (
						<button onClick={choosePath} disabled={disabled} type="button">
							{label}
						</button>
					)}
				</CreateProjectFlow>
			</QueryClientProvider>,
		);
	}

	it("detects a workspace folder, pre-checks the checkbox, and submits asWorkspace: true", async () => {
		const user = userEvent.setup();
		const onCreateProject = vi.fn().mockResolvedValue(undefined) as CreateProjectHandler;
		window.ao!.app.chooseDirectory = vi.fn().mockResolvedValue("/repo/new-workspace");
		window.ao!.app.detectWorkspace = vi
			.fn()
			.mockResolvedValue({ looksLikeWorkspace: true, detectedChildNames: ["svc-a", "svc-b"] });
		renderCreateProjectFlow(onCreateProject);

		await user.click(screen.getByRole("button", { name: "New project" }));

		expect(await screen.findByText("/repo/new-workspace")).toBeInTheDocument();
		expect(screen.getByLabelText("Multi-repo workspace")).toBeChecked();
		expect(screen.getByText("Detected repos: svc-a, svc-b")).toBeInTheDocument();
		await chooseOption(screen.getByRole("combobox", { name: "Worker agent" }), "Codex");
		await chooseOption(screen.getByRole("combobox", { name: "Orchestrator agent" }), "Claude Code");
		await user.click(screen.getByRole("button", { name: "Create and start" }));

		await waitFor(() =>
			expect(onCreateProject).toHaveBeenCalledWith(
				expect.objectContaining({ path: "/repo/new-workspace", asWorkspace: true }),
			),
		);
	});

	it("leaves the workspace checkbox unchecked for a plain single-repo folder", async () => {
		const user = userEvent.setup();
		const onCreateProject = vi.fn().mockResolvedValue(undefined) as CreateProjectHandler;
		window.ao!.app.chooseDirectory = vi.fn().mockResolvedValue("/repo/new-project");
		window.ao!.app.detectWorkspace = vi
			.fn()
			.mockResolvedValue({ looksLikeWorkspace: false, detectedChildNames: [] });
		renderCreateProjectFlow(onCreateProject);

		await user.click(screen.getByRole("button", { name: "New project" }));

		expect(await screen.findByText("/repo/new-project")).toBeInTheDocument();
		expect(screen.getByLabelText("Multi-repo workspace")).not.toBeChecked();
		await chooseOption(screen.getByRole("combobox", { name: "Worker agent" }), "Codex");
		await chooseOption(screen.getByRole("combobox", { name: "Orchestrator agent" }), "Claude Code");
		await user.click(screen.getByRole("button", { name: "Create and start" }));

		await waitFor(() =>
			expect(onCreateProject).toHaveBeenCalledWith({
				path: "/repo/new-project",
				workerAgent: "codex",
				orchestratorAgent: "claude-code",
				trackerIntake: undefined,
				companyId: undefined,
				asWorkspace: undefined,
			}),
		);
	});
});
