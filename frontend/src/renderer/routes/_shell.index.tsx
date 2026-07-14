import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Building2, Plus, ArrowRight, Briefcase, Terminal } from "lucide-react";
import { useCompaniesQuery, companiesQueryKey } from "../hooks/useCompaniesQuery";
import { useWorkspaceQuery } from "../hooks/useWorkspaceQuery";
import { apiClient, apiErrorMessage } from "../lib/api-client";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/input";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../components/ui/card";
import { DashboardSubhead } from "../components/DashboardSubhead";
import { groupWorkspacesByCompany, sessionIsActive } from "../types/workspace";
import { MigrationPopup } from "../components/MigrationPopup";
import { HQSection } from "../components/HQSection";
import { HeartbeatPauseSwitch } from "../components/HeartbeatPauseSwitch";
import { cn } from "../lib/utils";

export const Route = createFileRoute("/_shell/")({
	component: CEODashboard,
});

export function CEODashboard() {
	const companiesQuery = useCompaniesQuery();
	const workspacesQuery = useWorkspaceQuery();
	const queryClient = useQueryClient();
	const navigate = useNavigate();
	const [newCompanyName, setNewCompanyName] = useState("");
	const [isCreating, setIsCreating] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const [watchLiveError, setWatchLiveError] = useState<string | null>(null);

	const watchLive = async (companyId: string) => {
		setWatchLiveError(null);
		const { data, error } = await apiClient.GET("/api/v1/org/overview");
		if (error) {
			setWatchLiveError(apiErrorMessage(error, "Could not load org overview"));
			return;
		}
		const overview = data?.overview;
		const company = overview?.companies.find((c) => c.id === companyId);
		const activeProject = company?.projects.find((p) => p.activeSessions > 0);
		const ids = [
			overview?.holdingHq?.orchestratorSessionId,
			company?.hq?.orchestratorSessionId,
			activeProject?.orchestratorSessionId,
		].filter((id): id is string => Boolean(id));
		navigate({ to: "/terminals", search: { sessions: ids.join(",") } });
	};

	const createCompanyMutation = useMutation({
		mutationFn: async (name: string) => {
			const { data, error } = await apiClient.POST("/api/v1/companies", {
				body: { name },
			});
			if (error) throw new Error(apiErrorMessage(error, "Failed to create company"));
			return data?.company;
		},
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: companiesQueryKey });
			setNewCompanyName("");
			setIsCreating(false);
		},
		onError: (err) => {
			setError(err instanceof Error ? err.message : "An error occurred");
		},
	});

	const handleCreateCompany = (e: React.FormEvent) => {
		e.preventDefault();
		if (!newCompanyName.trim()) return;
		setError(null);
		createCompanyMutation.mutate(newCompanyName.trim());
	};

	const companies = companiesQuery.data ?? [];
	const workspaces = workspacesQuery.data ?? [];
	const companyGroups = groupWorkspacesByCompany(workspaces, companies);

	const headerActions = isCreating ? (
		<form onSubmit={handleCreateCompany} className="flex items-center gap-2">
			<Input
				autoFocus
				placeholder="Company name..."
				value={newCompanyName}
				onChange={(e) => setNewCompanyName(e.target.value)}
				className="w-48"
				disabled={createCompanyMutation.isPending}
			/>
			<Button type="submit" disabled={createCompanyMutation.isPending || !newCompanyName.trim()}>
				Save
			</Button>
			<Button type="button" variant="ghost" onClick={() => setIsCreating(false)}>
				Cancel
			</Button>
		</form>
	) : (
		<>
			<HeartbeatPauseSwitch />
			<Button onClick={() => setIsCreating(true)} className="gap-2">
				<Plus size={16} />
				Add Company
			</Button>
		</>
	);

	return (
		<div className="flex h-full min-h-0 flex-col overflow-y-auto bg-background text-foreground">
			<MigrationPopup />
			<DashboardSubhead
				title="UPPU Holdings CEO Dashboard"
				subtitle="Overview of all companies and operations"
				actions={headerActions}
			/>

			<div className="mx-auto w-full max-w-6xl space-y-8 px-[18px] pb-8 pt-6">
				{error && (
					<div className="rounded-md border border-destructive/30 bg-destructive/10 p-4 text-destructive">
						{error}
					</div>
				)}
				{watchLiveError && (
					<div className="rounded-md border border-destructive/30 bg-destructive/10 p-4 text-destructive">
						{watchLiveError}
					</div>
				)}

				<HQSection scope={{ kind: "holding" }} />

				{companiesQuery.isLoading || workspacesQuery.isLoading ? (
					<div className="text-passive">Loading holdings data...</div>
				) : (
					<div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
						{companyGroups.map((group) => {
							const isUnassigned = group.id === "unassigned";
							const title = group.name;
							const projectCount = group.workspaces.length;
							const activeSessions = group.workspaces.reduce((acc, ws) => acc + ws.sessions.filter(s => sessionIsActive(s)).length, 0);

							return (
								<Card
									key={group.id}
									className={cn("transition-colors", !isUnassigned && "cursor-pointer hover:border-accent/50")}
									onClick={() => {
										if (!isUnassigned) {
											navigate({ to: "/companies/$companyId", params: { companyId: group.id } });
										}
									}}
								>
									<CardHeader className="pb-2">
										<div className="flex items-center justify-between">
											<div className="p-2 bg-accent/10 rounded-lg">
												{isUnassigned ? <Briefcase className="text-accent" size={24} /> : <Building2 className="text-accent" size={24} />}
											</div>
											{!isUnassigned && <ArrowRight className="text-passive" size={20} />}
										</div>
										<CardTitle className="text-xl mt-4 text-foreground">{title}</CardTitle>
										<CardDescription>
											{isUnassigned ? "Projects without a company" : `Company ID: ${group.id}`}
										</CardDescription>
									</CardHeader>
									<CardContent>
										<div className="flex gap-4 mt-4">
											<div>
												<div className="text-2xl font-semibold text-foreground">{projectCount}</div>
												<div className="text-xs text-passive uppercase tracking-wider font-semibold">Products</div>
											</div>
											<div>
												<div className="text-2xl font-semibold text-foreground">{activeSessions}</div>
												<div className="text-xs text-passive uppercase tracking-wider font-semibold">Active Agents</div>
											</div>
										</div>
										{!isUnassigned && (
											<Button
												aria-label={`Watch ${group.name} live`}
												className="mt-4 w-full gap-2"
												onClick={(e) => {
													e.stopPropagation();
													void watchLive(group.id);
												}}
												size="sm"
												variant="outline"
											>
												<Terminal size={14} />
												Watch Live
											</Button>
										)}
									</CardContent>
								</Card>
							);
						})}
					</div>
				)}
			</div>
		</div>
	);
}
