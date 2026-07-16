import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { type DragEvent, useMemo, useState } from "react";
import type { components } from "../../api/schema";
import { useWorkboardCards, workboardQueryKey, type WorkCard as WorkboardCard } from "../hooks/useWorkboardQuery";
import { apiClient, apiErrorMessage } from "../lib/api-client";
import { cn } from "../lib/utils";
import { CreateWorkCardDialog } from "./CreateWorkCardDialog";
import { DashboardSubhead } from "./DashboardSubhead";
import { WorkCard } from "./WorkCard";
import { Button } from "./ui/button";

type CardStatus = WorkboardCard["status"];
type MoveWorkCardRequest = components["schemas"]["MoveWorkCardRequest"];

export const WORKBOARD_COLUMNS: { status: CardStatus; label: string; rail: string }[] = [
	{ status: "triage", label: "Triage", rail: "var(--purple)" },
	{ status: "backlog", label: "Backlog", rail: "var(--fg-passive)" },
	{ status: "todo", label: "To do", rail: "var(--accent)" },
	{ status: "scheduled", label: "Scheduled", rail: "var(--amber)" },
	{ status: "ready", label: "Ready", rail: "var(--green)" },
	{ status: "running", label: "Running", rail: "var(--orange)" },
	{ status: "review", label: "Review", rail: "var(--accent)" },
	{ status: "blocked", label: "Blocked", rail: "var(--red)" },
	{ status: "done", label: "Done", rail: "var(--fg-passive)" },
];

export function Workboard({ projectId, onShowSessions }: { projectId: string; onShowSessions?: () => void }) {
	const queryClient = useQueryClient();
	const cardsQuery = useWorkboardCards(projectId);
	const [isCreateOpen, setIsCreateOpen] = useState(false);
	const [draggedCardId, setDraggedCardId] = useState<string>();
	const [moveError, setMoveError] = useState<string>();
	const cardsByStatus = useMemo(() => {
		const grouped = new Map<CardStatus, WorkboardCard[]>();
		for (const card of cardsQuery.data ?? []) (grouped.get(card.status) ?? grouped.set(card.status, []).get(card.status)!).push(card);
		for (const cards of grouped.values()) cards.sort((a, b) => a.position - b.position);
		return grouped;
	}, [cardsQuery.data]);
	const moveCard = useMutation({
		mutationFn: async ({ cardId, status, position }: { cardId: string; status: CardStatus; position: number }) => {
			const body: MoveWorkCardRequest = { status, position };
			const { error } = await apiClient.POST("/api/v1/workboard/cards/{cardId}/move", { params: { path: { cardId } }, body });
			if (error) throw new Error(apiErrorMessage(error, "Could not move work card."));
		},
		onSuccess: () => queryClient.invalidateQueries({ queryKey: workboardQueryKey(projectId) }),
		onError: (error) => setMoveError(error instanceof Error ? error.message : "Could not move work card."),
	});
	const handleDrop = (event: DragEvent<HTMLElement>, status: CardStatus, position: number) => {
		event.preventDefault();
		const cardId = event.dataTransfer.getData("text/plain") || draggedCardId;
		setDraggedCardId(undefined);
		if (!cardId) return;
		setMoveError(undefined);
		moveCard.mutate({ cardId, status, position });
	};

	return (
		<div className="flex h-full min-h-0 flex-col bg-background text-foreground">
			<DashboardSubhead
				title="Workboard"
				subtitle="Durable work cards in OpenClaw flow order."
				actions={<><Button onClick={() => setIsCreateOpen(true)} size="sm"><Plus className="size-3.5" aria-hidden="true" />Create card</Button>{onShowSessions ? <Button onClick={onShowSessions} size="sm" variant="ghost">Sessions</Button> : null}</>}
			/>
			<div className="min-h-0 flex-1 overflow-x-auto overflow-y-hidden p-[18px]">
				{cardsQuery.isError ? <p className="py-10 text-center text-[12px] text-passive">Could not load workboard.</p> : (
					<div className="grid h-full min-w-[1540px] grid-cols-9 gap-2">
						{WORKBOARD_COLUMNS.map((column) => {
							const cards = cardsByStatus.get(column.status) ?? [];
							return <section className={cn("relative flex min-w-0 flex-col overflow-hidden rounded-[10px] bg-[var(--kanban-column-bg)]")} key={column.status} onDragOver={(event) => event.preventDefault()} onDrop={(event) => handleDrop(event, column.status, cards.length)}>
								<div className="absolute inset-y-0 left-0 w-[2px]" style={{ background: column.rail }} />
								<div className="flex shrink-0 items-center gap-2 px-3 pb-2.5 pt-3">
									<span className="text-[10.5px] font-semibold uppercase tracking-[0.08em] text-muted-foreground">{column.label}</span>
									<span className="ml-auto font-mono text-[10px] text-passive">{cards.length}</span>
								</div>
								<div className="min-h-0 flex-1 overflow-y-auto px-2 pb-3"><div className="flex flex-col gap-2">{cards.map((card) => <WorkCard card={card} key={card.id} onDragStart={setDraggedCardId} />)}</div></div>
							</section>;
						})}
					</div>
				)}
			</div>
			{moveError ? <p className="px-[18px] pb-3 text-[12px] text-destructive" role="alert">{moveError}</p> : null}
			<CreateWorkCardDialog open={isCreateOpen} projectId={projectId} onCreated={() => undefined} onOpenChange={setIsCreateOpen} />
		</div>
	);
}
