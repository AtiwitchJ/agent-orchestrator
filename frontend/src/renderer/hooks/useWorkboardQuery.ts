import { useQuery } from "@tanstack/react-query";
import type { components } from "../../api/schema";
import { apiClient, apiErrorMessage } from "../lib/api-client";

export type WorkCard = components["schemas"]["WorkCardResponse"];

// Shared cache key for the project-scoped Workboard query.
// The Workboard UI owns the query; the SSE transport only needs this key to
// invalidate it when a card changes.
export const workboardQueryKey = (projectId?: string) =>
	projectId ? (["workboard", projectId] as const) : (["workboard"] as const);

async function fetchWorkboardCards(projectId: string): Promise<WorkCard[]> {
	const { data, error } = await apiClient.GET("/api/v1/projects/{projectId}/workboard/cards", {
		params: { path: { projectId } },
	});
	if (error) throw new Error(apiErrorMessage(error, "Could not load workboard."));
	return data?.cards ?? [];
}

export function useWorkboardCards(projectId?: string) {
	return useQuery({
		queryKey: workboardQueryKey(projectId),
		queryFn: () => fetchWorkboardCards(projectId as string),
		enabled: Boolean(projectId),
	});
}
