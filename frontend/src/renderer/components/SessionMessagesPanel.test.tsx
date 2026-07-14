import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { WorkspaceSummary } from "../types/workspace";

const { messagesQueryMock, workspaceQueryMock } = vi.hoisted(() => ({
	messagesQueryMock: vi.fn(),
	workspaceQueryMock: vi.fn(),
}));

vi.mock("../hooks/useProjectMessagesQuery", () => ({
	useProjectMessagesQuery: messagesQueryMock,
}));

vi.mock("../hooks/useWorkspaceQuery", () => ({
	useWorkspaceQuery: workspaceQueryMock,
}));

import { SessionMessagesPanel } from "./SessionMessagesPanel";

const workspace: WorkspaceSummary = {
	id: "proj-1",
	name: "proj-1",
	path: "/repo/proj-1",
	sessions: [
		{
			id: "sess-a",
			workspaceId: "proj-1",
			workspaceName: "proj-1",
			title: "orchestrator",
			provider: "claude-code",
			kind: "orchestrator",
			branch: "session/sess-a",
			status: "working",
			updatedAt: "2026-07-04T00:00:00Z",
			prs: [],
		},
		{
			id: "sess-b",
			workspaceId: "proj-1",
			workspaceName: "proj-1",
			title: "fix login",
			provider: "codex",
			kind: "worker",
			branch: "session/sess-b",
			status: "working",
			updatedAt: "2026-07-04T00:00:00Z",
			prs: [],
		},
	],
};

beforeEach(() => {
	messagesQueryMock.mockReset().mockReturnValue({ data: [], isError: false });
	workspaceQueryMock.mockReset().mockReturnValue({ data: [workspace] });
});

describe("SessionMessagesPanel", () => {
	it("shows an empty state when there are no messages", () => {
		render(<SessionMessagesPanel projectId="proj-1" />);

		expect(screen.getByText("No agent messages yet.")).toBeInTheDocument();
	});

	it("shows an error state when the query fails", () => {
		messagesQueryMock.mockReturnValue({ data: undefined, isError: true });

		render(<SessionMessagesPanel projectId="proj-1" />);

		expect(screen.getByText("Could not load messages.")).toBeInTheDocument();
	});

	it("renders messages newest first with resolved session titles", () => {
		messagesQueryMock.mockReturnValue({
			data: [
				{ id: "m1", targetSessionId: "sess-b", content: "first", createdAt: "2026-07-04T00:00:00Z" },
				{
					id: "m2",
					senderSessionId: "sess-a",
					targetSessionId: "sess-b",
					content: "second",
					createdAt: "2026-07-04T00:05:00Z",
				},
			],
			isError: false,
		});

		render(<SessionMessagesPanel projectId="proj-1" />);

		// The "→" glyph sits in its own <span>, so getAllByText matches that leaf
		// node rather than the row; walk up to the row (its parent div) to
		// assert against the full sender/target/time line.
		const rows = screen.getAllByText("→", { exact: true }).map((arrow) => arrow.parentElement as HTMLElement);
		expect(rows).toHaveLength(2);
		// Newest (m2, from orchestrator) renders first.
		expect(rows[0]).toHaveTextContent("orchestrator");
		expect(screen.getByText("second")).toBeInTheDocument();
		expect(screen.getByText("first")).toBeInTheDocument();
		// m1 has no senderSessionId: renders as "you", not the raw session id.
		expect(rows[1]).toHaveTextContent("you");
	});

	it("falls back to the raw session id when the session is unknown", () => {
		messagesQueryMock.mockReturnValue({
			data: [
				{
					id: "m1",
					senderSessionId: "sess-gone",
					targetSessionId: "sess-b",
					content: "hi",
					createdAt: "2026-07-04T00:00:00Z",
				},
			],
			isError: false,
		});

		render(<SessionMessagesPanel projectId="proj-1" />);

		expect(screen.getByText(/sess-gone/)).toBeInTheDocument();
	});
});
