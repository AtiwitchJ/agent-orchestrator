import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { TerminalTile } from "./TerminalTile";
import type { WorkspaceSession } from "../types/workspace";

const { postMock } = vi.hoisted(() => ({ postMock: vi.fn() }));

vi.mock("../lib/api-client", () => ({
	apiClient: { POST: postMock },
	apiErrorMessage: (error: unknown, fallback = "Request failed") => {
		if (error instanceof Error) return error.message;
		if (typeof error === "object" && error !== null && typeof (error as { message?: unknown }).message === "string") {
			return (error as { message: string }).message;
		}
		return fallback;
	},
}));

vi.mock("./TerminalPane", () => ({ TerminalPane: () => <div>terminal body</div> }));

const session: WorkspaceSession = {
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
};

function renderTile(overrides: Partial<Parameters<typeof TerminalTile>[0]> = {}) {
	const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
	const onRemove = vi.fn();
	render(
		<QueryClientProvider client={queryClient}>
			<TerminalTile
				sessionId="holding-hq-1"
				session={session}
				projectName="holding-hq"
				theme="dark"
				daemonReady
				fontSize={12}
				height={280}
				onRemove={onRemove}
				{...overrides}
			/>
		</QueryClientProvider>,
	);
	return { onRemove };
}

describe("TerminalTile", () => {
	it("renders the session header and terminal body", () => {
		renderTile();
		expect(screen.getByText("holding-hq")).toBeInTheDocument();
		expect(screen.getByText("holding-hq-1")).toBeInTheDocument();
		expect(screen.getByText("terminal body")).toBeInTheDocument();
	});

	it("sends a message via POST /sessions/{id}/send and clears the input", async () => {
		postMock.mockResolvedValue({ data: { ok: true, sessionId: "holding-hq-1" }, error: undefined });
		const user = userEvent.setup();
		renderTile();

		const input = screen.getByLabelText("Message holding-hq-1");
		await user.type(input, "check status");
		await user.click(screen.getByRole("button", { name: "Send" }));

		await waitFor(() =>
			expect(postMock).toHaveBeenCalledWith("/api/v1/sessions/{sessionId}/send", {
				params: { path: { sessionId: "holding-hq-1" } },
				body: { message: "check status" },
			}),
		);
		await waitFor(() => expect(input).toHaveValue(""));
	});

	it("shows a fallback and remove control when the session no longer exists", async () => {
		const user = userEvent.setup();
		const { onRemove } = renderTile({ session: undefined });

		expect(screen.getByText("Session no longer available")).toBeInTheDocument();
		expect(screen.queryByText("terminal body")).not.toBeInTheDocument();

		await user.click(screen.getByLabelText("Remove holding-hq-1"));
		expect(onRemove).toHaveBeenCalledTimes(1);
	});

	it("shows an inline error when the send fails, without affecting the rest of the tile", async () => {
		postMock.mockResolvedValue({ data: undefined, error: { code: "SESSION_NOT_FOUND", message: "Session not found" } });
		const user = userEvent.setup();
		renderTile();

		await user.type(screen.getByLabelText("Message holding-hq-1"), "check status");
		await user.click(screen.getByRole("button", { name: "Send" }));

		expect(await screen.findByText("Session not found")).toBeInTheDocument();
		// The terminal body and header stay intact — a send failure is scoped to
		// this tile's compose bar, not the whole tile.
		expect(screen.getByText("terminal body")).toBeInTheDocument();
		expect(screen.getByText("holding-hq-1")).toBeInTheDocument();
	});
});
