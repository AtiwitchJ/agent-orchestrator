import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, Plus, FolderGit2, Trash2 } from "lucide-react";
import { useCompaniesQuery, companiesQueryKey } from "../hooks/useCompaniesQuery";
import { useWorkspaceQuery } from "../hooks/useWorkspaceQuery";
import { Button } from "../components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../components/ui/card";
import { CreateProjectFlow } from "../components/Sidebar";
import { useShell } from "../lib/shell-context";
import { sessionIsActive } from "../types/workspace";
import { apiClient, apiErrorMessage } from "../lib/api-client";

export const Route = createFileRoute("/_shell/companies/$companyId")({
	component: CompanyDashboard,
});

function CompanyDashboard() {
	const { companyId } = Route.useParams();
	const navigate = useNavigate();
	const companiesQuery = useCompaniesQuery();
	const workspacesQuery = useWorkspaceQuery();
	const queryClient = useQueryClient();
	const { createProject } = useShell();
	
	const company = companiesQuery.data?.find((c) => c.id === companyId);
	const isUnassigned = companyId === "unassigned";
	
	const workspaces = workspacesQuery.data ?? [];
	const companyProjects = workspaces.filter((ws) => {
		if (isUnassigned) return !ws.companyId;
		return ws.companyId === companyId;
	});

	const deleteCompanyMutation = useMutation({
		mutationFn: async () => {
			const { error } = await apiClient.DELETE("/api/v1/companies/{id}", {
				params: { path: { id: companyId } },
			});
			if (error) throw new Error(apiErrorMessage(error, "Failed to delete company"));
		},
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: companiesQueryKey });
			navigate({ to: "/" });
		},
		onError: (err) => {
			alert(err instanceof Error ? err.message : "An error occurred");
		},
	});

	const handleDeleteCompany = () => {
		if (companyProjects.length > 0) {
			alert("Cannot delete a company with active projects. Please reassign or delete the projects first.");
			return;
		}
		if (window.confirm(`Are you sure you want to delete the company "${company?.name}"?`)) {
			deleteCompanyMutation.mutate();
		}
	};

	if (!company && !isUnassigned && !companiesQuery.isLoading) {
		return (
			<div className="flex h-full flex-col items-center justify-center bg-slate-950 text-slate-100">
				<div className="text-xl font-bold">Company not found</div>
				<Button className="mt-4" onClick={() => navigate({ to: "/" })}>Back to CEO Dashboard</Button>
			</div>
		);
	}

	return (
		<div className="flex h-full flex-col overflow-y-auto bg-slate-950 p-8 text-slate-100">
			<div className="mx-auto w-full max-w-5xl space-y-8">
				<div className="flex items-center gap-4">
					<Button variant="ghost" size="icon" onClick={() => navigate({ to: "/" })}>
						<ArrowLeft className="h-5 w-5" />
					</Button>
					<div>
						<h1 className="text-3xl font-bold tracking-tight">
							{isUnassigned ? "Unassigned Projects" : company?.name}
						</h1>
						<p className="text-slate-400 mt-2">
							{isUnassigned ? "Projects not linked to any company" : `Company ID: ${company?.id}`}
						</p>
					</div>
					
					<div className="ml-auto flex items-center gap-4">
						{!isUnassigned && company && (
							<Button 
								variant="outline" 
								className="gap-2 text-red-500 hover:text-red-400 hover:bg-red-950/50 border-red-900/30"
								onClick={handleDeleteCompany}
								disabled={deleteCompanyMutation.isPending}
							>
								<Trash2 size={16} />
								Delete Company
							</Button>
						)}
						<CreateProjectFlow onCreateProject={(args) => createProject({ ...args, companyId: isUnassigned ? undefined : companyId })}>
							{({ disabled, choosePath, label }) => (
								<Button onClick={choosePath} disabled={disabled} className="gap-2">
									<Plus size={16} />
									{label === "New project" ? "Add Project" : label}
								</Button>
							)}
						</CreateProjectFlow>
					</div>
				</div>

				{workspacesQuery.isLoading ? (
					<div className="text-slate-500">Loading projects...</div>
				) : (
					<div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
						{companyProjects.map((project) => {
							const activeSessions = project.sessions.filter(s => sessionIsActive(s)).length;
							return (
								<Card key={project.id} className="bg-slate-900/50 border-slate-800 hover:border-blue-500/50 transition-colors cursor-pointer" onClick={() => {
									navigate({ to: "/projects/$projectId", params: { projectId: project.id } });
								}}>
									<CardHeader className="pb-2">
										<div className="flex items-center justify-between">
											<div className="p-2 bg-purple-500/10 rounded-lg">
												<FolderGit2 className="text-purple-400" size={24} />
											</div>
										</div>
										<CardTitle className="text-xl mt-4 text-slate-200 truncate" title={project.name || project.id}>
											{project.name || project.id}
										</CardTitle>
										<CardDescription className="text-slate-400 truncate">
											{project.path}
										</CardDescription>
									</CardHeader>
									<CardContent>
										<div className="flex gap-4 mt-4">
											<div>
												<div className="text-2xl font-semibold text-slate-200">{project.sessions.length}</div>
												<div className="text-xs text-slate-500 uppercase tracking-wider font-semibold">Total Agents</div>
											</div>
											<div>
												<div className="text-2xl font-semibold text-slate-200">{activeSessions}</div>
												<div className="text-xs text-slate-500 uppercase tracking-wider font-semibold">Active</div>
											</div>
										</div>
									</CardContent>
								</Card>
							);
						})}
						{companyProjects.length === 0 && (
							<div className="col-span-full py-12 text-center text-slate-500 bg-slate-900/20 border border-slate-800/50 rounded-lg border-dashed">
								No projects found for this company.
							</div>
						)}
					</div>
				)}
			</div>
		</div>
	);
}
