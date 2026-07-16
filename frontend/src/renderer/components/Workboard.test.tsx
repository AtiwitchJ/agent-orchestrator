import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { WorkCard as WorkboardCard } from "../hooks/useWorkboardQuery";

const { postMock, useWorkboardCardsMock } = vi.hoisted(() => ({
	postMock: vi.fn(),
	useWorkboardCardsMock: vi.fn(),
}));

vi.mock("../hooks/useWorkboardQuery", () => ({
	workboardQueryKey: (projectId?: string) => (projectId ? ["workboard", projectId] : ["workboard"]),
	useWorkboardCards: (...args: unknown[]) => useWorkboardCardsMock(...args),
}));

vi.mock("../lib/api-client", () => ({
	apiClient: { POST: (...args: unknown[]) => postMock(...args) },
	apiErrorMessage: () => "Request failed",
}));

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

function renderBoard() {
	render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><Workboard projectId="proj-1" /></QueryClientProvider>);
}

beforeEach(() => {
	useWorkboardCardsMock.mockReset().mockReturnValue({ data: [card], isError: false });
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
});
