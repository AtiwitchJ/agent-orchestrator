import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Building2, Plus, ArrowRight, Briefcase } from "lucide-react";
import { useCompaniesQuery, companiesQueryKey } from "../hooks/useCompaniesQuery";
import { useWorkspaceQuery } from "../hooks/useWorkspaceQuery";
import { apiClient, apiErrorMessage } from "../lib/api-client";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/input";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../components/ui/card";
import { groupWorkspacesByCompany, sessionIsActive } from "../types/workspace";
import { MigrationPopup } from "../components/MigrationPopup";
import { HQSection } from "../components/HQSection";
import { HeartbeatPauseSwitch } from "../components/HeartbeatPauseSwitch";

export const Route = createFileRoute("/_shell/")({
	component: CEODashboard,
});

function CEODashboard() {
	const companiesQuery = useCompaniesQuery();
	const workspacesQuery = useWorkspaceQuery();
	const queryClient = useQueryClient();
	const navigate = useNavigate();
	const [newCompanyName, setNewCompanyName] = useState("");
	const [isCreating, setIsCreating] = useState(false);
	const [error, setError] = useState<string | null>(null);

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

	return (
		<div className="flex h-full flex-col overflow-y-auto bg-slate-950 p-8 text-slate-100">
			<MigrationPopup />
			
			<div className="mx-auto w-full max-w-6xl space-y-8">
				<div className="flex items-center justify-between">
					<div>
						<h1 className="text-3xl font-bold tracking-tight">UPPU Holdings CEO Dashboard</h1>
						<p className="text-slate-400 mt-2">Overview of all companies and operations</p>
					</div>
					
					{isCreating ? (
						<form onSubmit={handleCreateCompany} className="flex items-center gap-2">
							<Input
								autoFocus
								placeholder="Company Name..."
								value={newCompanyName}
								onChange={(e) => setNewCompanyName(e.target.value)}
								className="w-48 bg-slate-900 border-slate-700"
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
						<div className="flex items-center gap-4">
							<HeartbeatPauseSwitch />
							<Button onClick={() => setIsCreating(true)} className="gap-2">
								<Plus size={16} />
								Add Company
							</Button>
						</div>
					)}
				</div>

				{error && (
					<div className="p-4 bg-red-950/50 border border-red-900/50 text-red-200 rounded-md">
						{error}
					</div>
				)}

				<HQSection scope={{ kind: "holding" }} />

				{companiesQuery.isLoading || workspacesQuery.isLoading ? (
					<div className="text-slate-500">Loading holdings data...</div>
				) : (
					<div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
						{companyGroups.map((group) => {
							const isUnassigned = group.id === "unassigned";
							const title = group.name;
							const projectCount = group.workspaces.length;
							const activeSessions = group.workspaces.reduce((acc, ws) => acc + ws.sessions.filter(s => sessionIsActive(s)).length, 0);

							return (
								<Card key={group.id} className="bg-slate-900/50 border-slate-800 hover:border-blue-500/50 transition-colors cursor-pointer" onClick={() => {
									if (!isUnassigned) {
										navigate({ to: "/companies/$companyId", params: { companyId: group.id } });
									}
								}}>
									<CardHeader className="pb-2">
										<div className="flex items-center justify-between">
											<div className="p-2 bg-blue-500/10 rounded-lg">
												{isUnassigned ? <Briefcase className="text-blue-400" size={24} /> : <Building2 className="text-blue-400" size={24} />}
											</div>
											{!isUnassigned && <ArrowRight className="text-slate-600" size={20} />}
										</div>
										<CardTitle className="text-xl mt-4 text-slate-200">{title}</CardTitle>
										<CardDescription className="text-slate-400">
											{isUnassigned ? "Projects without a company" : `Company ID: ${group.id}`}
										</CardDescription>
									</CardHeader>
									<CardContent>
										<div className="flex gap-4 mt-4">
											<div>
												<div className="text-2xl font-semibold text-slate-200">{projectCount}</div>
												<div className="text-xs text-slate-500 uppercase tracking-wider font-semibold">Products</div>
											</div>
											<div>
												<div className="text-2xl font-semibold text-slate-200">{activeSessions}</div>
												<div className="text-xs text-slate-500 uppercase tracking-wider font-semibold">Active Agents</div>
											</div>
										</div>
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
