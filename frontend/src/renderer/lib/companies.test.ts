import { describe, expect, it, vi, beforeEach } from "vitest";
import { assignProjectCompany, createCompany } from "./companies";
import { apiClient } from "./api-client";

vi.mock("./api-client", () => ({
	apiClient: { POST: vi.fn(), PUT: vi.fn() },
	apiErrorMessage: (error: unknown, fallback = "Request failed") => {
		if (typeof error === "object" && error !== null && "message" in error) {
			return String((error as { message: unknown }).message);
		}
		return fallback;
	},
}));

describe("createCompany", () => {
	beforeEach(() => vi.clearAllMocks());

	it("returns the created company", async () => {
		(apiClient.POST as ReturnType<typeof vi.fn>).mockResolvedValue({
			data: { company: { id: "co-1", name: "OPEN-UPPU", createdAt: "2026-01-01T00:00:00Z" } },
			error: undefined,
		});

		const company = await createCompany("OPEN-UPPU");

		expect(company).toEqual({ id: "co-1", name: "OPEN-UPPU", createdAt: "2026-01-01T00:00:00Z" });
		expect(apiClient.POST).toHaveBeenCalledWith("/api/v1/companies", { body: { name: "OPEN-UPPU" } });
	});

	it("surfaces daemon errors", async () => {
		(apiClient.POST as ReturnType<typeof vi.fn>).mockResolvedValue({
			data: undefined,
			error: { message: "name already taken" },
		});

		await expect(createCompany("OPEN-UPPU")).rejects.toThrow("name already taken");
	});
});

describe("assignProjectCompany", () => {
	beforeEach(() => vi.clearAllMocks());

	it("PUTs the company id to the project's company endpoint", async () => {
		(apiClient.PUT as ReturnType<typeof vi.fn>).mockResolvedValue({ data: { projectId: "proj-1" }, error: undefined });

		await assignProjectCompany("proj-1", "co-1");

		expect(apiClient.PUT).toHaveBeenCalledWith("/api/v1/projects/{id}/company", {
			params: { path: { id: "proj-1" } },
			body: { companyId: "co-1" },
		});
	});

	it("sends an empty companyId to unassign", async () => {
		(apiClient.PUT as ReturnType<typeof vi.fn>).mockResolvedValue({ data: { projectId: "proj-1" }, error: undefined });

		await assignProjectCompany("proj-1", "");

		expect(apiClient.PUT).toHaveBeenCalledWith("/api/v1/projects/{id}/company", {
			params: { path: { id: "proj-1" } },
			body: { companyId: "" },
		});
	});

	it("surfaces daemon errors", async () => {
		(apiClient.PUT as ReturnType<typeof vi.fn>).mockResolvedValue({
			data: undefined,
			error: { message: "company not found" },
		});

		await expect(assignProjectCompany("proj-1", "co-missing")).rejects.toThrow("company not found");
	});
});
