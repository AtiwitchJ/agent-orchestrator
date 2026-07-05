import { useQuery } from "@tanstack/react-query";
import type { components } from "../../api/schema";
import { apiClient, apiErrorMessage } from "../lib/api-client";

export type SessionMessage = components["schemas"]["SessionMessage"];

// No-arg form (`["session-messages"]`) is a prefix match used by the SSE
// transport to invalidate every project's cached messages at once, mirroring
// sessionScmSummaryQueryKey's no-arg broad-invalidate form.
export const projectMessagesQueryKey = (projectId?: string) =>
	projectId ? (["session-messages", projectId] as const) : (["session-messages"] as const);

const usePreviewData = import.meta.env.VITE_NO_ELECTRON === "1";

export async function fetchProjectMessages(projectId: string): Promise<SessionMessage[]> {
	if (usePreviewData) return [];
	const { data, error } = await apiClient.GET("/api/v1/projects/{id}/messages", {
		params: { path: { id: projectId }, query: { limit: 100 } },
	});
	if (error) throw new Error(apiErrorMessage(error, "Could not load messages"));
	return data?.messages ?? [];
}

export function useProjectMessagesQuery(projectId?: string) {
	return useQuery({
		queryKey: projectMessagesQueryKey(projectId),
		queryFn: () => fetchProjectMessages(projectId!),
		enabled: Boolean(projectId) && !usePreviewData,
		retry: 1,
		// SSE (`session_message_created`, wired in lib/event-transport.ts)
		// invalidates this query on every new message; the interval is just a
		// fallback for a dropped/reconnecting stream, mirroring
		// useWorkspaceQuery's polling fallback.
		refetchInterval: 15_000,
	});
}
