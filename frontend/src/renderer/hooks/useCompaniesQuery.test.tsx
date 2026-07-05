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

import { useCompaniesQuery } from "./useCompaniesQuery";

function wrapper({ children }: { children: ReactNode }) {
	const queryClient = new QueryClient({ defaultOptions: { queries: { retryDelay: 0 } } });
	return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

beforeEach(() => {
	getMock.mockReset();
});

describe("useCompaniesQuery", () => {
	it("returns the companies list from the daemon", async () => {
		getMock.mockResolvedValue({
			data: { companies: [{ id: "co-1", name: "OPEN-UPPU", createdAt: "2026-01-01T00:00:00Z" }] },
			error: undefined,
		});

		const { result } = renderHook(() => useCompaniesQuery(), { wrapper });
		await waitFor(() => expect(result.current.isSuccess).toBe(true));

		expect(result.current.data).toEqual([{ id: "co-1", name: "OPEN-UPPU", createdAt: "2026-01-01T00:00:00Z" }]);
		expect(getMock).toHaveBeenCalledWith("/api/v1/companies");
	});

	it("defaults to an empty list when the daemon returns none", async () => {
		getMock.mockResolvedValue({ data: { companies: [] }, error: undefined });

		const { result } = renderHook(() => useCompaniesQuery(), { wrapper });
		await waitFor(() => expect(result.current.isSuccess).toBe(true));

		expect(result.current.data).toEqual([]);
	});

	it("surfaces a fetch error", async () => {
		getMock.mockResolvedValue({ data: undefined, error: { message: "companies backend down" } });

		const { result } = renderHook(() => useCompaniesQuery(), { wrapper });

		await waitFor(() => expect(result.current.isError).toBe(true));
		expect(result.current.error).toEqual(new Error("companies backend down"));
	});
});
