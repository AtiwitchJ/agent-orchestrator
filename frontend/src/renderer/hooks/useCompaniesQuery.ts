import { useQuery } from "@tanstack/react-query";
import type { components } from "../../api/schema";
import { apiClient, apiErrorMessage } from "../lib/api-client";

export type Company = components["schemas"]["Company"];

export const companiesQueryKey = ["companies"] as const;
const usePreviewData = import.meta.env.VITE_NO_ELECTRON === "1";

export async function fetchCompanies(): Promise<Company[]> {
	if (usePreviewData) return [];
	const { data, error } = await apiClient.GET("/api/v1/companies");
	if (error) throw new Error(apiErrorMessage(error, "Could not load companies"));
	return data?.companies ?? [];
}

// Shared so callers (Sidebar, ProjectSettingsForm) read the same cache and
// mutations can invalidate it by key without importing the hook.
export const companiesQueryOptions = {
	queryKey: companiesQueryKey,
	queryFn: fetchCompanies,
	retry: 1,
};

export function useCompaniesQuery() {
	return useQuery(companiesQueryOptions);
}
