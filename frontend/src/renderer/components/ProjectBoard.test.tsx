import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

vi.mock("./Workboard", () => ({
	Workboard: ({ onShowSessions }: { onShowSessions?: () => void }) => (
		<div>
			<p>Workboard primary</p>
			<button onClick={onShowSessions} type="button">Show sessions</button>
		</div>
	),
}));

vi.mock("./SessionsBoard", () => ({
	SessionsBoard: ({ onShowWorkboard }: { onShowWorkboard?: () => void }) => (
		<div>
			<p>Sessions board</p>
			<button onClick={onShowWorkboard} type="button">Return to Workboard</button>
		</div>
	),
}));

import { ProjectBoard } from "./ProjectBoard";

describe("ProjectBoard", () => {
	it("keeps Workboard primary and returns from Sessions without navigation", async () => {
		const user = userEvent.setup();
		render(<ProjectBoard projectId="proj-1" />);

		expect(screen.getByText("Workboard primary")).toBeInTheDocument();
		await user.click(screen.getByRole("button", { name: "Show sessions" }));
		expect(screen.getByText("Sessions board")).toBeInTheDocument();
		await user.click(screen.getByRole("button", { name: "Return to Workboard" }));
		expect(screen.getByText("Workboard primary")).toBeInTheDocument();
	});
});
