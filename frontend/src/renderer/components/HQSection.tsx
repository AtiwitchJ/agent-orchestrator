import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { Building2, Crown, Play, RotateCw } from "lucide-react";
import type { components } from "../../api/schema";
import { useWorkspaceQuery, workspaceQueryKey } from "../hooks/useWorkspaceQuery";
import { apiClient, apiErrorMessage } from "../lib/api-client";
import { restartProjectOrchestrator } from "../lib/restart-orchestrator";
import { spawnOrchestrator } from "../lib/spawn-orchestrator";
import { useUiStore } from "../stores/ui-store";
import { newestActiveOrchestrator, type WorkspaceSummary } from "../types/workspace";
import { Button } from "./ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "./ui/card";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "./ui/select";

type Project = components["schemas"]["Project"];
type HeartbeatConfig = components["schemas"]["DomainHeartbeatConfig"];

/** Which HQ this section renders: the single holding CEO, or one company's PM. */
export type HQScope = { kind: "holding" } | { kind: "company"; companyId: string };

const projectQueryKey = (id: string) => ["project", id] as const;

const INTERVAL_OPTIONS = [
	{ value: "15m", label: "Every 15 minutes" },
	{ value: "30m", label: "Every 30 minutes" },
	{ value: "1h", label: "Every hour" },
	{ value: "2h", label: "Every 2 hours" },
];

function roleLabel(scope: HQScope): string {
	return scope.kind === "holding" ? "CEO" : "PM";
}

/**
 * The HQ project for this scope, or undefined when none has been created yet.
 * Renders either a "Create HQ" prompt or the HQ's orchestrator + heartbeat
 * controls.
 */
export function HQSection({ scope }: { scope: HQScope }) {
	const workspacesQuery = useWorkspaceQuery();
	const workspaces = workspacesQuery.data ?? [];
	const hq = workspaces.find((ws) =>
		scope.kind === "holding" ? ws.hqRole === "holding" : ws.hqRole === "company" && ws.companyId === scope.companyId,
	);

	if (!hq) {
		return <CreateHQCard scope={scope} />;
	}
	return <HQCard scope={scope} hq={hq} />;
}

function CreateHQCard({ scope }: { scope: HQScope }) {
	const queryClient = useQueryClient();
	const navigate = useNavigate();
	const [isProvisioning, setIsProvisioning] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const role = roleLabel(scope);

	// No folder picker: the CEO and each company's PM are structural parts of
	// the org, not ordinary delivery projects a human sets up by hand — the
	// daemon provisions the HQ repo itself under its own data dir.
	const handleCreate = async () => {
		setIsProvisioning(true);
		setError(null);
		try {
			const { data, error: apiError } =
				scope.kind === "holding"
					? await apiClient.POST("/api/v1/org/holding-hq")
					: await apiClient.POST("/api/v1/org/companies/{companyId}/hq", {
							params: { path: { companyId: scope.companyId } },
						});
			if (apiError || !data?.projectId) throw new Error(apiErrorMessage(apiError, `Could not create ${role} HQ`));
			await queryClient.invalidateQueries({ queryKey: workspaceQueryKey });
			const sessionId = await spawnOrchestrator(data.projectId);
			await queryClient.invalidateQueries({ queryKey: workspaceQueryKey });
			void navigate({
				to: "/projects/$projectId/sessions/$sessionId",
				params: { projectId: data.projectId, sessionId },
			});
		} catch (err) {
			setError(err instanceof Error ? err.message : `Could not create ${role} HQ`);
		} finally {
			setIsProvisioning(false);
		}
	};

	return (
		<Card className="border-dashed border-slate-800 bg-slate-900/50">
			<CardHeader className="pb-2">
				<CardTitle className="flex items-center gap-2 text-lg text-slate-200">
					{scope.kind === "holding" ? (
						<Crown size={18} className="text-amber-400" />
					) : (
						<Building2 size={18} className="text-blue-400" />
					)}
					{scope.kind === "holding" ? "Holding Headquarters (CEO)" : "Company Headquarters (PM)"}
				</CardTitle>
			</CardHeader>
			<CardContent>
				<p className="mb-4 text-sm text-slate-400">
					No {role} headquarters yet. AO sets one up automatically — a small local repo it manages — to run an
					autonomous {role} orchestrator that coordinates{" "}
					{scope.kind === "holding" ? "every company" : "this company's projects"}.
				</p>
				<Button onClick={() => void handleCreate()} disabled={isProvisioning} className="gap-2">
					{isProvisioning ? "Setting up…" : `Create ${role} HQ`}
				</Button>
				{error && <p className="mt-2 text-sm text-red-400">{error}</p>}
			</CardContent>
		</Card>
	);
}

function HQCard({ scope, hq }: { scope: HQScope; hq: WorkspaceSummary }) {
	const navigate = useNavigate();
	const queryClient = useQueryClient();
	const orchestrator = newestActiveOrchestrator(hq.sessions);
	const restartingProjectIds = useUiStore((s) => s.restartingProjectIds);
	const setProjectRestarting = useUiStore((s) => s.setProjectRestarting);
	const setOrchestratorReplacementError = useUiStore((s) => s.setOrchestratorReplacementError);
	const isRestarting = restartingProjectIds.has(hq.id);
	const [isSpawning, setIsSpawning] = useState(false);
	const [spawnError, setSpawnError] = useState<string | null>(null);
	const role = roleLabel(scope);

	const projectQuery = useQuery({
		queryKey: projectQueryKey(hq.id),
		queryFn: async () => {
			const { data, error } = await apiClient.GET("/api/v1/projects/{id}", { params: { path: { id: hq.id } } });
			if (error) throw new Error(apiErrorMessage(error));
			if (data?.status !== "ok") throw new Error("Project config is unavailable (degraded).");
			return data.project as Project;
		},
	});
	const heartbeat: HeartbeatConfig = projectQuery.data?.config?.heartbeat ?? {};

	const heartbeatMutation = useMutation({
		mutationFn: async (next: HeartbeatConfig) => {
			const project = projectQuery.data;
			if (!project) throw new Error("Project not loaded yet");
			// PUT replaces the whole config; merge over what loaded so we don't
			// drop fields this section doesn't expose (worker/orchestrator agent,
			// symlinks, etc).
			const { error } = await apiClient.PUT("/api/v1/projects/{id}/config", {
				params: { path: { id: hq.id } },
				body: { config: { ...project.config, heartbeat: next } },
			});
			if (error) throw new Error(apiErrorMessage(error));
		},
		onSuccess: () => queryClient.invalidateQueries({ queryKey: projectQueryKey(hq.id) }),
	});

	const handleStart = async () => {
		setIsSpawning(true);
		setSpawnError(null);
		try {
			const sessionId = await spawnOrchestrator(hq.id);
			await queryClient.invalidateQueries({ queryKey: workspaceQueryKey });
			void navigate({ to: "/projects/$projectId/sessions/$sessionId", params: { projectId: hq.id, sessionId } });
		} catch (err) {
			setSpawnError(err instanceof Error ? err.message : `Could not start ${role}`);
		} finally {
			setIsSpawning(false);
		}
	};

	const handleRestart = () => {
		void restartProjectOrchestrator({
			projectId: hq.id,
			queryClient,
			navigate,
			setProjectRestarting,
			setOrchestratorReplacementError,
		});
	};

	return (
		<Card className="border-slate-800 bg-slate-900/50">
			<CardHeader className="pb-2">
				<CardTitle className="flex items-center gap-2 text-lg text-slate-200">
					{scope.kind === "holding" ? (
						<Crown size={18} className="text-amber-400" />
					) : (
						<Building2 size={18} className="text-blue-400" />
					)}
					{role} — {hq.name || hq.id}
				</CardTitle>
			</CardHeader>
			<CardContent className="space-y-4">
				<div className="flex flex-wrap items-center gap-3">
					{orchestrator ? (
						<>
							<span className="text-sm text-slate-400">
								Orchestrator {orchestrator.id} — {orchestrator.activity?.state ?? "unknown"}
							</span>
							<Button
								size="sm"
								variant="outline"
								onClick={() =>
									navigate({
										to: "/projects/$projectId/sessions/$sessionId",
										params: { projectId: hq.id, sessionId: orchestrator.id },
									})
								}
							>
								Open terminal
							</Button>
							<Button size="sm" variant="ghost" className="gap-1" onClick={handleRestart} disabled={isRestarting}>
								<RotateCw size={14} />
								{isRestarting ? "Replacing…" : "Replace"}
							</Button>
						</>
					) : (
						<>
							<Button size="sm" className="gap-1" onClick={() => void handleStart()} disabled={isSpawning}>
								<Play size={14} />
								{isSpawning ? "Starting…" : `Start ${role}`}
							</Button>
							{spawnError && <span className="text-sm text-red-400">{spawnError}</span>}
						</>
					)}
				</div>

				<div className="flex flex-wrap items-center gap-3 border-t border-slate-800 pt-3">
					<label className="flex items-center gap-2 text-sm text-slate-400">
						<input
							type="checkbox"
							className="h-4 w-4 accent-accent"
							checked={heartbeat.enabled ?? false}
							disabled={projectQuery.isLoading || heartbeatMutation.isPending}
							onChange={(e) => heartbeatMutation.mutate({ enabled: e.target.checked, interval: heartbeat.interval ?? "30m" })}
						/>
						Heartbeat
					</label>
					<Select
						value={heartbeat.interval ?? "30m"}
						disabled={!heartbeat.enabled || heartbeatMutation.isPending}
						onValueChange={(interval) => heartbeatMutation.mutate({ enabled: true, interval })}
					>
						<SelectTrigger className="h-8 w-44 text-sm">
							<SelectValue />
						</SelectTrigger>
						<SelectContent>
							{INTERVAL_OPTIONS.map((opt) => (
								<SelectItem key={opt.value} value={opt.value}>
									{opt.label}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
					{heartbeatMutation.isError && (
						<span className="text-sm text-red-400">
							{heartbeatMutation.error instanceof Error ? heartbeatMutation.error.message : "Could not update heartbeat"}
						</span>
					)}
				</div>
			</CardContent>
		</Card>
	);
}
