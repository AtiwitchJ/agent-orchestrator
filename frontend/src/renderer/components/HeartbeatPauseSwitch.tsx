import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiClient, apiErrorMessage } from "../lib/api-client";

const heartbeatQueryKey = ["org-heartbeat"] as const;

/** Global heartbeat kill switch, shown once on the CEO dashboard. Pausing
 * stops every HQ orchestrator's wake-up nudges daemon-wide until resumed. */
export function HeartbeatPauseSwitch() {
	const queryClient = useQueryClient();
	const query = useQuery({
		queryKey: heartbeatQueryKey,
		queryFn: async () => {
			const { data, error } = await apiClient.GET("/api/v1/org/heartbeat");
			if (error) throw new Error(apiErrorMessage(error));
			return data?.paused ?? false;
		},
	});

	const mutation = useMutation({
		mutationFn: async (paused: boolean) => {
			const { data, error } = await apiClient.PUT("/api/v1/org/heartbeat", { body: { paused } });
			if (error) throw new Error(apiErrorMessage(error));
			return data?.paused ?? paused;
		},
		onSuccess: (paused) => queryClient.setQueryData(heartbeatQueryKey, paused),
	});

	const paused = mutation.isPending ? mutation.variables : (query.data ?? false);

	return (
		<label className="flex items-center gap-2 text-[13px] text-muted-foreground">
			<input
				type="checkbox"
				className="h-4 w-4 accent-accent"
				checked={!paused}
				disabled={query.isLoading || mutation.isPending}
				onChange={(e) => mutation.mutate(!e.target.checked)}
			/>
			Heartbeats {paused ? "paused" : "running"}
			{mutation.isError && (
				<span className="text-destructive">{mutation.error instanceof Error ? mutation.error.message : "Failed"}</span>
			)}
		</label>
	);
}
