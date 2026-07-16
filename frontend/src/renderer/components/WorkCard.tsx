import type { DragEvent } from "react";
import { cn } from "../lib/utils";
import type { WorkCard as WorkboardCard } from "../hooks/useWorkboardQuery";

const PRIORITY: Record<WorkboardCard["priority"], { label: string; className: string }> = {
	urgent: { label: "Urgent", className: "bg-error" },
	high: { label: "High", className: "bg-warning" },
	normal: { label: "Normal", className: "bg-accent" },
	low: { label: "Low", className: "bg-passive" },
};

export function WorkCard({ card, onDragStart }: { card: WorkboardCard; onDragStart: (cardId: string) => void }) {
	const priority = PRIORITY[card.priority];
	const handleDragStart = (event: DragEvent<HTMLElement>) => {
		event.dataTransfer.effectAllowed = "move";
		event.dataTransfer.setData("text/plain", card.id);
		onDragStart(card.id);
	};

	return (
		<article
			aria-label={`${card.title}, ${priority.label} priority`}
			className="group cursor-grab rounded-[7px] border border-border bg-surface text-left shadow-[0_1px_0_rgb(0_0_0_/_0.18)] transition-colors hover:border-border-strong focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-weak active:cursor-grabbing"
			draggable
			onDragStart={handleDragStart}
			tabIndex={0}
		>
			<div className="flex items-center gap-2 px-3 pb-2 pt-2.5">
				<span className={cn("size-1.5 shrink-0 rounded-full", priority.className)} title={`${priority.label} priority`} />
				<span className="font-mono text-[10px] uppercase tracking-[0.06em] text-passive">{priority.label}</span>
				<span className="ml-auto max-w-[7.5rem] truncate font-mono text-[10px] text-passive">{card.agent}</span>
			</div>
			<h2 className="line-clamp-2 px-3 pb-2 text-[13px] font-medium leading-[1.42] tracking-[-0.01em] text-foreground">
				{card.title}
			</h2>
			{card.notes ? <p className="line-clamp-2 px-3 pb-2.5 text-[11.5px] leading-[1.45] text-muted-foreground">{card.notes}</p> : null}
			<div className="flex min-w-0 flex-wrap gap-1 border-t border-border px-3 py-2">
				{card.labels.map((label) => (
					<span key={label} className="max-w-full truncate rounded-[3px] bg-raised px-1.5 py-0.5 font-mono text-[9.5px] text-muted-foreground">
						{label}
					</span>
				))}
				<span className="ml-auto max-w-full truncate font-mono text-[9.5px] leading-5 text-passive" title={card.targetPath}>
					{card.targetPath.split("/").filter(Boolean).at(-1) || card.targetPath}
				</span>
			</div>
		</article>
	);
}
