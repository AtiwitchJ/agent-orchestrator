import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { WorkCard as WorkboardCard } from "../hooks/useWorkboardQuery";

const { postMock, useWorkboardCardsMock, useWorkspaceQueryMock } = vi.hoisted(() => ({
	postMock: vi.fn(),
	useWorkboardCardsMock: vi.fn(),
	useWorkspaceQueryMock: vi.fn(),
}));

vi.mock("../hooks/useWorkboardQuery", () => ({
	workboardQueryKey: (projectId?: string) => (projectId ? ["workboard", projectId] : ["workboard"]),
	useWorkboardCards: (...args: unknown[]) => useWorkboardCardsMock(...args),
}));

vi.mock("../lib/api-client", () => ({
	apiClient: { POST: (...args: unknown[]) => postMock(...args) },
	apiErrorMessage: () => "Request failed",
}));

vi.mock("../hooks/useWorkspaceQuery", () => ({ useWorkspaceQuery: (...args: unknown[]) => useWorkspaceQueryMock(...args) }));
vi.mock("../lib/shell-context", () => ({ useShell: () => ({ daemonStatus: { state: "ready" } }) }));
vi.mock("./TerminalPane", () => ({ TerminalPane: () => <div>live terminal preview</div> }));

import { Workboard } from "./Workboard";

const card: WorkboardCard = {
	id: "card-1",
	projectId: "proj-1",
	boardId: "default",
	title: "Repair diagnostics",
	notes: "Preserve actionable errors.",
	priority: "high",
	labels: ["frontend"],
	status: "triage",
	position: 0,
	targetPath: "/repo/project",
	agent: "codex",
	waitingForInput: false,
	pausedRetarget: false,
	goalVersion: 1,
	createdAt: "2026-01-01T00:00:00Z",
	updatedAt: "2026-01-01T00:00:00Z",
};

function renderBoard(onShowSessions?: () => void) {
	render(
		<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
			<Workboard onShowSessions={onShowSessions} projectId="proj-1" />
		</QueryClientProvider>,
	);
}

beforeEach(() => {
	useWorkboardCardsMock.mockReset().mockReturnValue({ data: [card], isError: false });
	useWorkspaceQueryMock.mockReset().mockReturnValue({ data: [] });
	postMock.mockReset().mockResolvedValue({ data: { ...card, status: "ready" }, error: undefined });
});

describe("Workboard", () => {
	it("keeps the OpenClaw flow order and moves a dropped card", async () => {
		renderBoard();
		expect(screen.getAllByText(/^(Triage|Backlog|To do|Scheduled|Ready|Running|Review|Blocked|Done)$/).map((node) => node.textContent)).toEqual([
			"Triage", "Backlog", "To do", "Scheduled", "Ready", "Running", "Review", "Blocked", "Done",
		]);

		const values = new Map<string, string>();
		const dataTransfer = {
			effectAllowed: "",
			setData: (type: string, value: string) => values.set(type, value),
			getData: (type: string) => values.get(type) ?? "",
		};
		fireEvent.dragStart(screen.getByRole("article", { name: /Repair diagnostics/i }), { dataTransfer });
		const readyColumn = screen.getByText("Ready").closest("section");
		expect(readyColumn).not.toBeNull();
		fireEvent.dragOver(readyColumn!, { dataTransfer });
		fireEvent.drop(readyColumn!, { dataTransfer });

		await waitFor(() => expect(postMock).toHaveBeenCalledWith("/api/v1/workboard/cards/{cardId}/move", {
			params: { path: { cardId: "card-1" } },
			body: { status: "ready", position: 0 },
		}));
	});

	it("moves a focused card to the adjacent column with the arrow keys", async () => {
		renderBoard();
		const workCard = screen.getByRole("article", { name: /Repair diagnostics/i });

		fireEvent.keyDown(workCard, { key: "ArrowRight" });

		await waitFor(() => expect(postMock).toHaveBeenCalledWith("/api/v1/workboard/cards/{cardId}/move", {
			params: { path: { cardId: "card-1" } },
			body: { status: "backlog", position: 0 },
		}));
		expect(await screen.findByText("Moved card to Backlog.")).toBeInTheDocument();
		expect(workCard).toHaveClass("motion-reduce:transition-none");
	});

	it("opens a live terminal preview for a selected running card", () => {
		const runningCard = { ...card, status: "running" as const, sessionId: "session-1" };
		useWorkboardCardsMock.mockReturnValue({ data: [runningCard], isError: false });
		useWorkspaceQueryMock.mockReturnValue({
			data: [{ id: "proj-1", name: "Project", path: "/repo/project", sessions: [{
				id: "session-1",
				workspaceId: "proj-1",
				workspaceName: "Project",
				title: "Repair diagnostics",
				provider: "codex",
				branch: "session/session-1",
				status: "working",
				updatedAt: "2026-01-01T00:00:00Z",
				prs: [],
			}] }],
		});

		renderBoard();
		fireEvent.click(screen.getByRole("article", { name: /Repair diagnostics/i }));

		expect(screen.getByRole("complementary", { name: /Focus panel for Repair diagnostics/i })).toBeInTheDocument();
		expect(screen.getByText("live terminal preview")).toBeInTheDocument();
	});

	it("keeps non-focused cards off the live terminal and offers the fallback terminal action", async () => {
		const onShowSessions = vi.fn();
		const linkedCard = { ...card, status: "review" as const, sessionId: "session-1" };
		useWorkboardCardsMock.mockReturnValue({ data: [linkedCard], isError: false });

		renderBoard(onShowSessions);

		expect(screen.queryByText("live terminal preview")).not.toBeInTheDocument();

		fireEvent.click(screen.getByRole("article", { name: /Repair diagnostics/i }));

		expect(screen.queryByText("live terminal preview")).not.toBeInTheDocument();
		await waitFor(() => expect(screen.getByRole("button", { name: "Open terminal" })).toBeInTheDocument());

		fireEvent.click(screen.getByRole("button", { name: "Open terminal" }));
		expect(onShowSessions).toHaveBeenCalledTimes(1);
	});
});
