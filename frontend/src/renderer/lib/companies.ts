import type { components } from "../../api/schema";
import { apiClient, apiErrorMessage } from "./api-client";

export type Company = components["schemas"]["Company"];

/** Register a new company via the daemon API. */
export async function createCompany(name: string): Promise<Company> {
	const { data, error } = await apiClient.POST("/api/v1/companies", { body: { name } });
	if (error || !data?.company) {
		throw new Error(apiErrorMessage(error, "Failed to create company"));
	}
	return data.company;
}

/** Assign (or, with an empty companyId, unassign) a project's company. */
export async function assignProjectCompany(projectId: string, companyId: string): Promise<void> {
	const { error } = await apiClient.PUT("/api/v1/projects/{id}/company", {
		params: { path: { id: projectId } },
		body: { companyId },
	});
	if (error) {
		throw new Error(apiErrorMessage(error, "Failed to assign company"));
	}
}
