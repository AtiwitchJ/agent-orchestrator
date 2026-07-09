import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, Plus, FolderGit2, Trash2 } from "lucide-react";
import { useCompaniesQuery, companiesQueryKey } from "../hooks/useCompaniesQuery";
import { useWorkspaceQuery } from "../hooks/useWorkspaceQuery";
import { Button } from "../components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../components/ui/card";
import { DashboardSubhead } from "../components/DashboardSubhead";
import { CreateProjectFlow } from "../components/Sidebar";
import { HQSection } from "../components/HQSection";
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
	// All of the company's projects, including its HQ — used for the
	// delete-blocking check so deleting a company never orphans its HQ project.
	const allCompanyProjects = workspaces.filter((ws) => (isUnassigned ? !ws.companyId : ws.companyId === companyId));
	// The ordinary project grid excludes the HQ project — it renders in
	// HQSection above the grid instead.
	const companyProjects = allCompanyProjects.filter((ws) => !ws.hqRole);

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
		if (allCompanyProjects.length > 0) {
			alert("Cannot delete a company with active projects. Please reassign or delete the projects first.");
			return;
		}
		if (window.confirm(`Are you sure you want to delete the company "${company?.name}"?`)) {
			deleteCompanyMutation.mutate();
		}
	};

	if (!company && !isUnassigned && !companiesQuery.isLoading) {
		return (
			<div className="flex h-full flex-col items-center justify-center gap-4 bg-background text-foreground">
				<div className="text-xl font-bold">Company not found</div>
				<Button className="gap-2" onClick={() => navigate({ to: "/" })}>
					<ArrowLeft size={16} />
					Back to Holdings Dashboard
				</Button>
			</div>
		);
	}

	const headerActions = (
		<>
			{!isUnassigned && company && (
				<Button
					variant="outline"
					className="gap-2 border-destructive/30 text-destructive hover:bg-destructive/10 hover:text-destructive"
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
		</>
	);

	return (
		<div className="flex h-full min-h-0 flex-col overflow-y-auto bg-background text-foreground">
			<div className="flex items-center gap-1 px-[18px] pt-[22px]">
				<Button variant="ghost" className="gap-1.5 text-passive hover:text-foreground" onClick={() => navigate({ to: "/" })}>
					<ArrowLeft size={15} />
					Holdings Dashboard
				</Button>
			</div>
			<DashboardSubhead
				title={isUnassigned ? "Unassigned Projects" : (company?.name ?? "")}
				subtitle={isUnassigned ? "Projects not linked to any company" : `Company ID: ${company?.id}`}
				actions={headerActions}
			/>

			<div className="mx-auto w-full max-w-5xl space-y-8 px-[18px] pb-8 pt-6">
				{!isUnassigned && <HQSection scope={{ kind: "company", companyId }} />}

				{workspacesQuery.isLoading ? (
					<div className="text-passive">Loading projects...</div>
				) : (
					<div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
						{companyProjects.map((project) => {
							const activeSessions = project.sessions.filter(s => sessionIsActive(s)).length;
							return (
								<Card
									key={project.id}
									className="cursor-pointer transition-colors hover:border-accent/50"
									onClick={() => {
										navigate({ to: "/projects/$projectId", params: { projectId: project.id } });
									}}
								>
									<CardHeader className="pb-2">
										<div className="flex items-center justify-between">
											<div className="p-2 bg-accent/10 rounded-lg">
												<FolderGit2 className="text-accent" size={24} />
											</div>
										</div>
										<CardTitle className="text-xl mt-4 text-foreground truncate" title={project.name || project.id}>
											{project.name || project.id}
										</CardTitle>
										<CardDescription className="truncate">
											{project.path}
										</CardDescription>
									</CardHeader>
									<CardContent>
										<div className="flex gap-4 mt-4">
											<div>
												<div className="text-2xl font-semibold text-foreground">{project.sessions.length}</div>
												<div className="text-xs text-passive uppercase tracking-wider font-semibold">Total Agents</div>
											</div>
											<div>
												<div className="text-2xl font-semibold text-foreground">{activeSessions}</div>
												<div className="text-xs text-passive uppercase tracking-wider font-semibold">Active</div>
											</div>
										</div>
									</CardContent>
								</Card>
							);
						})}
						{companyProjects.length === 0 && (
							<div className="col-span-full py-12 text-center text-passive bg-surface/50 border border-border rounded-lg border-dashed">
								No projects found for this company.
							</div>
						)}
					</div>
				)}
			</div>
		</div>
	);
}
