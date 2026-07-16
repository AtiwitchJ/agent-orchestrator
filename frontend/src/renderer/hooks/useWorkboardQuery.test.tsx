import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const getMock = vi.fn();

vi.mock("../lib/api-client", () => ({
	apiClient: { GET: (...args: unknown[]) => getMock(...args) },
	apiErrorMessage: () => "Request failed",
}));

import { useWorkboardCards } from "./useWorkboardQuery";

beforeEach(() => getMock.mockReset());

describe("useWorkboardCards", () => {
	it("loads cards from the project workboard endpoint", async () => {
		getMock.mockResolvedValue({ data: { cards: [] }, error: undefined });
		const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{children}</QueryClientProvider>;
		const { result } = renderHook(() => useWorkboardCards("proj-1"), { wrapper });

		await waitFor(() => expect(result.current.isSuccess).toBe(true));
		expect(getMock).toHaveBeenCalledWith("/api/v1/projects/{projectId}/workboard/cards", { params: { path: { projectId: "proj-1" } } });
		expect(result.current.data).toEqual([]);
	});
});
