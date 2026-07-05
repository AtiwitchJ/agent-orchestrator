import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";

const { getMock } = vi.hoisted(() => ({ getMock: vi.fn() }));

vi.mock("../lib/api-client", () => ({
	apiClient: { GET: getMock },
	apiErrorMessage: (error: unknown, fallback = "Request failed") => {
		if (typeof error === "object" && error !== null && "message" in error) {
			return String((error as { message: unknown }).message);
		}
		return fallback;
	},
}));

import { useProjectMessagesQuery, projectMessagesQueryKey } from "./useProjectMessagesQuery";

function wrapper({ children }: { children: ReactNode }) {
	const queryClient = new QueryClient({ defaultOptions: { queries: { retryDelay: 0 } } });
	return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

beforeEach(() => {
	getMock.mockReset();
});

describe("projectMessagesQueryKey", () => {
	it("scopes to a project id when given one", () => {
		expect(projectMessagesQueryKey("proj-1")).toEqual(["session-messages", "proj-1"]);
	});

	it("is a bare prefix with no project id, for broad invalidation", () => {
		expect(projectMessagesQueryKey()).toEqual(["session-messages"]);
	});
});

describe("useProjectMessagesQuery", () => {
	it("is disabled and returns no data when no projectId is given", () => {
		const { result } = renderHook(() => useProjectMessagesQuery(undefined), { wrapper });

		expect(result.current.fetchStatus).toBe("idle");
		expect(getMock).not.toHaveBeenCalled();
	});

	it("fetches messages for the given project, newest-agnostic order preserved from the wire", async () => {
		getMock.mockResolvedValue({
			data: {
				messages: [
					{ id: "m1", targetSessionId: "sess-b", content: "hi", createdAt: "2026-01-01T00:00:00Z" },
					{
						id: "m2",
						senderSessionId: "sess-a",
						targetSessionId: "sess-b",
						content: "hello back",
						createdAt: "2026-01-01T00:01:00Z",
					},
				],
			},
			error: undefined,
		});

		const { result } = renderHook(() => useProjectMessagesQuery("proj-1"), { wrapper });
		await waitFor(() => expect(result.current.isSuccess).toBe(true));

		expect(getMock).toHaveBeenCalledWith("/api/v1/projects/{id}/messages", {
			params: { path: { id: "proj-1" }, query: { limit: 100 } },
		});
		expect(result.current.data).toHaveLength(2);
		expect(result.current.data?.[0]).toMatchObject({ id: "m1", targetSessionId: "sess-b" });
	});

	it("surfaces a fetch error", async () => {
		getMock.mockResolvedValue({ data: undefined, error: { message: "messages backend down" } });

		const { result } = renderHook(() => useProjectMessagesQuery("proj-1"), { wrapper });

		await waitFor(() => expect(result.current.isError).toBe(true));
		expect(result.current.error).toEqual(new Error("messages backend down"));
	});
});
