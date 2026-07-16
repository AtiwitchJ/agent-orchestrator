import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { getMock, postMock } = vi.hoisted(() => ({
	getMock: vi.fn(),
	postMock: vi.fn(),
}));

vi.mock("../lib/api-client", () => ({
	apiClient: {
		GET: (...args: unknown[]) => getMock(...args),
		POST: (...args: unknown[]) => postMock(...args),
	},
	apiErrorMessage: (error: unknown, fallback = "Request failed") =>
		typeof error === "object" && error !== null && "message" in error ? String(error.message) : fallback,
}));

import { CreateWorkCardDialog } from "./CreateWorkCardDialog";

function renderDialog() {
	render(
		<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
			<CreateWorkCardDialog open projectId="proj-1" onCreated={vi.fn()} onOpenChange={vi.fn()} />
		</QueryClientProvider>,
	);
}

beforeEach(() => {
	getMock.mockReset().mockResolvedValue({
		data: {
			supported: [{ id: "codex", label: "Codex" }],
			installed: [{ id: "codex", label: "Codex", authStatus: "authorized" }],
			authorized: [{ id: "codex", label: "Codex", authStatus: "authorized" }],
		},
		error: undefined,
	});
	postMock.mockReset();
});

describe("CreateWorkCardDialog", () => {
	it("refuses submit until an agent is selected", async () => {
		renderDialog();
		const user = userEvent.setup();

		await user.type(screen.getByLabelText("Title"), "Repair build diagnostics");
		await user.type(screen.getByLabelText("Notes"), "Keep compiler errors actionable.");
		await user.type(screen.getByLabelText("Folder"), "/repo/project");
		await user.type(screen.getByLabelText("Labels"), "frontend{Enter}");
		await user.click(screen.getByRole("button", { name: "Create card" }));

		expect(await screen.findByText("Select an agent before creating this card.")).toBeInTheDocument();
		expect(postMock).not.toHaveBeenCalled();
	});
});
