# Live Terminals Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a generic multi-pane "Live Terminals" view so any set of existing sessions (e.g. CEO HQ + a Company PM HQ + a Worker) can be watched and interacted with side by side, each with its own real PTY and a message-send compose bar.

**Architecture:** Frontend-only. A new route (`/terminals`) reads a comma-joined session-id list from a `sessions` search param, resolves each id against the existing `useWorkspaceQuery` cache, and renders one `TerminalTile` per id in a responsive grid. Each tile reuses the existing `TerminalPane` (real PTY, already interactive) plus a new compose bar that calls the already-implemented `POST /api/v1/sessions/{sessionId}/send` endpoint. Two entry points write to the same search param: a new "Live Terminals" Settings-menu item (empty/manual picker) and a "Watch Live" button on CEO Dashboard company cards (pre-fills CEO HQ + that company's PM HQ + its most recent active worker session via `GET /api/v1/org/overview`).

**Tech Stack:** React 19, TanStack Router (file-based routes, `validateSearch`), TanStack Query, `openapi-fetch` (`apiClient`), Vitest + Testing Library, existing shadcn-style `ui/*` primitives.

## Global Constraints

- No backend changes, no OpenAPI regen — every endpoint used already exists (`POST /api/v1/sessions/{sessionId}/send`, `GET /api/v1/org/overview`).
- Reuse `TerminalPane` as-is (`frontend/src/renderer/components/TerminalPane.tsx`) — do not modify its internals.
- Mock `TerminalPane` in tests exactly like existing tests do: `vi.mock("./TerminalPane", () => ({ TerminalPane: () => <div>terminal body</div> }))` (see `CenterPane.test.tsx:7`).
- Selected sessions live in the URL (`?sessions=id1,id2,id3`, comma-joined string, not a JSON/array search param) — reload-safe and linkable.
- Sending a message from this UI never sets `senderSessionId` (a human via the UI, not another agent session — mirrors how `ao send` from a human shell leaves it unset per `backend/internal/cli/send.go:46-49`).
- Follow existing formatting: tabs for indentation, `cn(...)` from `../lib/utils` for conditional classes, semantic design tokens (`text-foreground`, `text-passive`, `bg-surface`, `border-border`, `text-accent`, `text-destructive`) — never raw Tailwind colors like `slate-*`/`blue-*`.

---

### Task 1: `goTerminals` navigation, empty route, and Settings-menu entry point

**Files:**
- Modify: `frontend/src/renderer/components/Sidebar.tsx` (`useSelection()` ~line 92-108; Settings dropdown ~line 296-338 and ~line 355-396)
- Create: `frontend/src/renderer/routes/_shell.terminals.tsx`
- Create: `frontend/src/renderer/components/LiveTerminalsPage.tsx` (minimal shell for now — filled in by Task 3)
- Test: `frontend/src/renderer/components/Sidebar.test.tsx`

**Interfaces:**
- Produces: `useSelection().goTerminals(sessionIds?: string[]): void` — navigates to `/terminals` with `search: { sessions: (sessionIds ?? []).join(",") }`.
- Produces: route `/_shell/terminals` with typed search `{ sessions: string }` (default `""`), consumed by later tasks via the generic `useSearch({ from: "/_shell/terminals" })` hook (not by importing the `Route` object — see Task 3's circular-import note).
- Produces: `LiveTerminalsPage` component (named export, matching the `PullRequestsPage` convention) — Task 1 renders it as an empty placeholder; Task 3 fills in the real grid.

- [ ] **Step 1: Write the failing test**

Add to `frontend/src/renderer/components/Sidebar.test.tsx`, inside the `describe("Sidebar", ...)` block (near the other Settings-menu-driven tests):

```tsx
it("navigates to Live Terminals from the settings menu", async () => {
	const user = userEvent.setup();
	renderSidebar();

	// Both the expanded and icon-rail Settings triggers exist in the DOM
	// simultaneously (the icon-rail one is only Tailwind-`hidden`, which jsdom
	// doesn't compute) — the expanded variant renders first, so index [0] is it.
	await user.click(screen.getAllByLabelText("Settings")[0]);
	await user.click(await screen.findByRole("menuitem", { name: "Live Terminals" }));

	expect(navigateMock).toHaveBeenCalledWith({ to: "/terminals", search: { sessions: "" } });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run --config vite.renderer.config.ts src/renderer/components/Sidebar.test.tsx -t "Live Terminals"`
Expected: FAIL — `Unable to find role="menuitem" and name "Live Terminals"`.

- [ ] **Step 3: Add `goTerminals` to `useSelection()`**

In `frontend/src/renderer/components/Sidebar.tsx`, extend the `useSelection` function (it currently ends with `goSession`):

```ts
		goSession: (projectId: string, sessionId: string) =>
			void navigate({ to: "/projects/$projectId/sessions/$sessionId", params: { projectId, sessionId } }),
		goTerminals: (sessionIds?: string[]) =>
			void navigate({ to: "/terminals", search: { sessions: (sessionIds ?? []).join(",") } }),
	};
}
```

(Add the new line right after the existing `goSession` entry, before the closing `};`.)

- [ ] **Step 4: Add the Settings-menu item in both variants**

In the **expanded** Settings dropdown (`Sidebar.tsx`, the block containing `<DropdownMenuItem onSelect={selection.goPrs}>` around line 317-320), add immediately after it:

```tsx
							<DropdownMenuItem onSelect={selection.goPrs}>
								<GitPullRequest aria-hidden="true" />
								Pull requests
							</DropdownMenuItem>
							<DropdownMenuItem onSelect={() => selection.goTerminals()}>
								<Terminal aria-hidden="true" />
								Live Terminals
							</DropdownMenuItem>
```

Repeat identically in the **icon-rail** Settings dropdown (the second copy, around line 377-380).

Add `Terminal` to the existing `lucide-react` import at the top of `Sidebar.tsx`:

```ts
import {
	ChevronRight,
	GitPullRequest,
	LayoutDashboard,
	Moon,
	MoreVertical,
	Pencil,
	Search,
	Settings,
	Sun,
	Terminal,
	Trash2,
} from "lucide-react";
```

- [ ] **Step 5: Create the route file**

Create `frontend/src/renderer/routes/_shell.terminals.tsx`:

```tsx
import { createFileRoute } from "@tanstack/react-router";
import { LiveTerminalsPage } from "../components/LiveTerminalsPage";

export type TerminalsSearch = { sessions: string };

export const Route = createFileRoute("/_shell/terminals")({
	validateSearch: (search: Record<string, unknown>): TerminalsSearch => ({
		sessions: typeof search.sessions === "string" ? search.sessions : "",
	}),
	component: LiveTerminalsPage,
});
```

- [ ] **Step 6: Create a minimal `LiveTerminalsPage` placeholder**

Create `frontend/src/renderer/components/LiveTerminalsPage.tsx`:

```tsx
import { DashboardSubhead } from "./DashboardSubhead";

export function LiveTerminalsPage() {
	return (
		<div className="flex h-full min-h-0 flex-col bg-background text-foreground">
			<DashboardSubhead title="Live Terminals" subtitle="Watch and message multiple sessions at once" />
			<div className="flex flex-1 items-center justify-center text-passive">Coming in Task 3.</div>
		</div>
	);
}
```

- [ ] **Step 7: Run test to verify it passes**

Run: `cd frontend && npx vitest run --config vite.renderer.config.ts src/renderer/components/Sidebar.test.tsx -t "Live Terminals"`
Expected: PASS

- [ ] **Step 8: Typecheck**

Run: `cd frontend && npx tsc --noEmit -p .`
Expected: no new errors (the two pre-existing unrelated errors in `useBrowserView.test.ts` / `env.test.ts` are fine).

- [ ] **Step 9: Commit**

```bash
git add frontend/src/renderer/components/Sidebar.tsx frontend/src/renderer/components/Sidebar.test.tsx frontend/src/renderer/routes/_shell.terminals.tsx frontend/src/renderer/components/LiveTerminalsPage.tsx
git commit -m "feat: add Live Terminals route and Settings-menu entry point"
```

---

### Task 2: `TerminalTile` component

**Files:**
- Create: `frontend/src/renderer/components/TerminalTile.tsx`
- Test: `frontend/src/renderer/components/TerminalTile.test.tsx`

**Interfaces:**
- Consumes: `TerminalPane` (mocked in tests) with its existing props `{ session?, theme, daemonReady, fontSize }` (`TerminalPane.tsx:12-18`); `apiClient.POST`, `apiErrorMessage` from `../lib/api-client`; `WorkspaceSession` and `Theme` types.
- Produces: `TerminalTile` component with props:

```ts
export type TerminalTileProps = {
	sessionId: string;
	session?: WorkspaceSession;
	projectName?: string;
	theme: Theme;
	daemonReady: boolean;
	fontSize: number;
	onRemove: () => void;
};
```

- [ ] **Step 1: Write the failing test**

Create `frontend/src/renderer/components/TerminalTile.test.tsx`:

```tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { TerminalTile } from "./TerminalTile";
import type { WorkspaceSession } from "../types/workspace";

const { postMock } = vi.hoisted(() => ({ postMock: vi.fn() }));

vi.mock("../lib/api-client", () => ({
	apiClient: { POST: postMock },
	apiErrorMessage: (error: unknown, fallback = "Request failed") => {
		if (error instanceof Error) return error.message;
		if (typeof error === "object" && error !== null && typeof (error as { message?: unknown }).message === "string") {
			return (error as { message: string }).message;
		}
		return fallback;
	},
}));

vi.mock("./TerminalPane", () => ({ TerminalPane: () => <div>terminal body</div> }));

const session: WorkspaceSession = {
	id: "holding-hq-1",
	workspaceId: "holding-hq",
	workspaceName: "holding-hq",
	title: "holding-hq-1",
	provider: "claude-code",
	kind: "orchestrator",
	branch: "main",
	status: "working",
	updatedAt: "2026-07-09T00:00:00Z",
	prs: [],
};

function renderTile(overrides: Partial<Parameters<typeof TerminalTile>[0]> = {}) {
	const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
	const onRemove = vi.fn();
	render(
		<QueryClientProvider client={queryClient}>
			<TerminalTile
				sessionId="holding-hq-1"
				session={session}
				projectName="holding-hq"
				theme="dark"
				daemonReady
				fontSize={12}
				onRemove={onRemove}
				{...overrides}
			/>
		</QueryClientProvider>,
	);
	return { onRemove };
}

describe("TerminalTile", () => {
	it("renders the session header and terminal body", () => {
		renderTile();
		expect(screen.getByText("holding-hq")).toBeInTheDocument();
		expect(screen.getByText("holding-hq-1")).toBeInTheDocument();
		expect(screen.getByText("terminal body")).toBeInTheDocument();
	});

	it("sends a message via POST /sessions/{id}/send and clears the input", async () => {
		postMock.mockResolvedValue({ data: { ok: true, sessionId: "holding-hq-1" }, error: undefined });
		const user = userEvent.setup();
		renderTile();

		const input = screen.getByLabelText("Message holding-hq-1");
		await user.type(input, "check status");
		await user.click(screen.getByRole("button", { name: "Send" }));

		await waitFor(() =>
			expect(postMock).toHaveBeenCalledWith("/api/v1/sessions/{sessionId}/send", {
				params: { path: { sessionId: "holding-hq-1" } },
				body: { message: "check status" },
			}),
		);
		await waitFor(() => expect(input).toHaveValue(""));
	});

	it("shows a fallback and remove control when the session no longer exists", async () => {
		const user = userEvent.setup();
		const { onRemove } = renderTile({ session: undefined });

		expect(screen.getByText("Session no longer available")).toBeInTheDocument();
		expect(screen.queryByText("terminal body")).not.toBeInTheDocument();

		await user.click(screen.getByLabelText("Remove holding-hq-1"));
		expect(onRemove).toHaveBeenCalledTimes(1);
	});

	it("shows an inline error when the send fails, without affecting the rest of the tile", async () => {
		postMock.mockResolvedValue({ data: undefined, error: { code: "SESSION_NOT_FOUND", message: "Session not found" } });
		const user = userEvent.setup();
		renderTile();

		await user.type(screen.getByLabelText("Message holding-hq-1"), "check status");
		await user.click(screen.getByRole("button", { name: "Send" }));

		expect(await screen.findByText("Session not found")).toBeInTheDocument();
		// The terminal body and header stay intact — a send failure is scoped to
		// this tile's compose bar, not the whole tile.
		expect(screen.getByText("terminal body")).toBeInTheDocument();
		expect(screen.getByText("holding-hq-1")).toBeInTheDocument();
	});
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run --config vite.renderer.config.ts src/renderer/components/TerminalTile.test.tsx`
Expected: FAIL — `Cannot find module './TerminalTile'`.

- [ ] **Step 3: Write the implementation**

Create `frontend/src/renderer/components/TerminalTile.tsx`:

```tsx
import { useMutation } from "@tanstack/react-query";
import { X } from "lucide-react";
import { useState } from "react";
import { apiClient, apiErrorMessage } from "../lib/api-client";
import type { Theme } from "../stores/ui-store";
import type { WorkspaceSession } from "../types/workspace";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { TerminalPane } from "./TerminalPane";

export type TerminalTileProps = {
	sessionId: string;
	session?: WorkspaceSession;
	projectName?: string;
	theme: Theme;
	daemonReady: boolean;
	fontSize: number;
	onRemove: () => void;
};

// One pane in the Live Terminals grid: a real, interactive TerminalPane (typing
// goes straight into the PTY, same as the single-session view) plus a compose
// bar that queues a message via the daemon's existing send endpoint — the same
// primitive the org heartbeat uses to nudge orchestrators, exposed in the UI
// for the first time.
export function TerminalTile({ sessionId, session, projectName, theme, daemonReady, fontSize, onRemove }: TerminalTileProps) {
	const [draft, setDraft] = useState("");
	const sendMutation = useMutation({
		mutationFn: async (message: string) => {
			const { data, error } = await apiClient.POST("/api/v1/sessions/{sessionId}/send", {
				params: { path: { sessionId } },
				body: { message },
			});
			if (error) throw new Error(apiErrorMessage(error, "Failed to send message"));
			return data;
		},
		onSuccess: () => setDraft(""),
	});

	return (
		<div className="flex min-h-0 flex-col overflow-hidden rounded-lg border border-border bg-surface">
			<div className="flex shrink-0 items-center gap-2 border-b border-border px-3 py-2">
				<span className="min-w-0 truncate text-[12px] font-semibold text-foreground">
					{projectName ?? sessionId}
				</span>
				<span className="min-w-0 truncate font-mono text-[11px] text-passive">{sessionId}</span>
				<button
					aria-label={`Remove ${sessionId}`}
					className="ml-auto grid size-5 shrink-0 place-items-center rounded-md text-passive transition-colors hover:bg-interactive-hover hover:text-foreground"
					onClick={onRemove}
					type="button"
				>
					<X className="size-3.5" aria-hidden="true" />
				</button>
			</div>
			{session ? (
				<>
					<div className="h-[280px] min-h-0">
						<TerminalPane session={session} theme={theme} daemonReady={daemonReady} fontSize={fontSize} />
					</div>
					<form
						className="flex shrink-0 items-center gap-2 border-t border-border p-2"
						onSubmit={(event) => {
							event.preventDefault();
							const message = draft.trim();
							if (!message || sendMutation.isPending) return;
							sendMutation.mutate(message);
						}}
					>
						<Input
							aria-label={`Message ${sessionId}`}
							className="h-8 flex-1"
							disabled={sendMutation.isPending}
							onChange={(e) => setDraft(e.target.value)}
							placeholder="Send a message..."
							value={draft}
						/>
						<Button disabled={sendMutation.isPending || !draft.trim()} size="sm" type="submit">
							Send
						</Button>
					</form>
					{sendMutation.isError && (
						<p className="border-t border-border px-2 py-1.5 text-[11px] text-destructive">
							{sendMutation.error instanceof Error ? sendMutation.error.message : "Failed to send message"}
						</p>
					)}
				</>
			) : (
				<div className="flex flex-1 items-center justify-center p-6 text-center text-[12px] text-passive">
					Session no longer available
				</div>
			)}
		</div>
	);
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && npx vitest run --config vite.renderer.config.ts src/renderer/components/TerminalTile.test.tsx`
Expected: PASS (3 tests)

- [ ] **Step 5: Typecheck**

Run: `cd frontend && npx tsc --noEmit -p .`
Expected: no new errors.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/renderer/components/TerminalTile.tsx frontend/src/renderer/components/TerminalTile.test.tsx
git commit -m "feat: add TerminalTile component with send-message compose bar"
```

---

### Task 3: `LiveTerminalsPage` — grid driven by the `sessions` search param, with add/remove picker

**Files:**
- Modify: `frontend/src/renderer/components/LiveTerminalsPage.tsx` (replace the Task 1 placeholder)
- Test: `frontend/src/renderer/components/LiveTerminalsPage.test.tsx`

**Interfaces:**
- Consumes: `TerminalTile` (`sessionId, session?, projectName?, theme, daemonReady, fontSize, onRemove`) from Task 2; the generic `useSearch({ from: "/_shell/terminals" })` hook from `@tanstack/react-router` (**not** an import of the route file's `Route` object — importing `Route` from `../routes/_shell.terminals` here would create a circular import, since that file imports `LiveTerminalsPage`); `useWorkspaceQuery` (`WorkspaceSummary[]` with `.sessions: WorkspaceSession[]`); `useUiStore` for `theme`; `useShell()` for `daemonStatus`.
- Produces: `LiveTerminalsPage` (same export name/signature as Task 1 — no other file's imports need to change).

- [ ] **Step 1: Write the failing test**

Create `frontend/src/renderer/components/LiveTerminalsPage.test.tsx`:

```tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { LiveTerminalsPage } from "./LiveTerminalsPage";
import type { WorkspaceSummary } from "../types/workspace";

const { navigateMock, searchMock, getMock } = vi.hoisted(() => ({
	navigateMock: vi.fn(),
	searchMock: vi.fn(),
	getMock: vi.fn(),
}));

vi.mock("@tanstack/react-router", async (importOriginal) => {
	const actual = await importOriginal<typeof import("@tanstack/react-router")>();
	return { ...actual, useNavigate: () => navigateMock, useSearch: () => searchMock() };
});

vi.mock("../lib/api-client", () => ({
	apiClient: { GET: getMock },
	apiErrorMessage: (error: unknown, fallback = "Request failed") => (error instanceof Error ? error.message : fallback),
}));

vi.mock("./TerminalPane", () => ({ TerminalPane: () => <div>terminal body</div> }));

const workspaces: WorkspaceSummary[] = [
	{
		id: "holding-hq",
		name: "holding-hq",
		path: "/hq/holding",
		sessions: [
			{
				id: "holding-hq-1",
				workspaceId: "holding-hq",
				workspaceName: "holding-hq",
				title: "holding-hq-1",
				provider: "claude-code",
				kind: "orchestrator",
				branch: "main",
				status: "working",
				updatedAt: "2026-07-09T00:00:00Z",
				prs: [],
			},
		],
	},
	{
		id: "qb-hq",
		name: "qb-hq",
		path: "/hq/qb",
		sessions: [
			{
				id: "qb-hq-1",
				workspaceId: "qb-hq",
				workspaceName: "qb-hq",
				title: "qb-hq-1",
				provider: "claude-code",
				kind: "orchestrator",
				branch: "main",
				status: "working",
				updatedAt: "2026-07-09T00:00:00Z",
				prs: [],
			},
		],
	},
];

function renderPage() {
	getMock.mockImplementation(async (path: string) => {
		if (path === "/api/v1/projects") return { data: { projects: workspaces.map((w) => ({ id: w.id, name: w.name, path: w.path })) }, error: undefined };
		if (path === "/api/v1/sessions")
			return { data: { sessions: workspaces.flatMap((w) => w.sessions.map((s) => ({ ...s, projectId: w.id, harness: s.provider, isTerminated: false }))) }, error: undefined };
		return { data: {}, error: undefined };
	});
	const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	render(
		<QueryClientProvider client={queryClient}>
			<LiveTerminalsPage />
		</QueryClientProvider>,
	);
}

describe("LiveTerminalsPage", () => {
	it("renders one tile per id in the sessions search param", async () => {
		searchMock.mockReturnValue({ sessions: "holding-hq-1,qb-hq-1" });
		renderPage();

		expect(await screen.findByText("holding-hq-1")).toBeInTheDocument();
		expect(screen.getByText("qb-hq-1")).toBeInTheDocument();
	});

	it("removing a tile updates the sessions search param", async () => {
		searchMock.mockReturnValue({ sessions: "holding-hq-1,qb-hq-1" });
		const user = userEvent.setup();
		renderPage();

		await screen.findByText("holding-hq-1");
		await user.click(screen.getByLabelText("Remove holding-hq-1"));

		expect(navigateMock).toHaveBeenCalledWith({
			to: "/terminals",
			search: { sessions: "qb-hq-1" },
		});
	});

	it("adding a session via the picker updates the sessions search param", async () => {
		searchMock.mockReturnValue({ sessions: "holding-hq-1" });
		const user = userEvent.setup();
		renderPage();

		await screen.findByText("holding-hq-1");
		await user.click(screen.getByRole("combobox", { name: "Add a session" }));
		await user.click(await screen.findByRole("option", { name: /qb-hq-1/ }));

		expect(navigateMock).toHaveBeenCalledWith({
			to: "/terminals",
			search: { sessions: "holding-hq-1,qb-hq-1" },
		});
	});
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run --config vite.renderer.config.ts src/renderer/components/LiveTerminalsPage.test.tsx`
Expected: FAIL — placeholder page doesn't render any tiles or picker.

- [ ] **Step 3: Write the implementation**

Replace `frontend/src/renderer/components/LiveTerminalsPage.tsx` entirely:

```tsx
import { useNavigate, useSearch } from "@tanstack/react-router";
import { useShell } from "../lib/shell-context";
import { useUiStore } from "../stores/ui-store";
import { useWorkspaceQuery } from "../hooks/useWorkspaceQuery";
import { DashboardSubhead } from "./DashboardSubhead";
import { TerminalTile } from "./TerminalTile";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "./ui/select";

const TERMINAL_FONT_SIZE = 12;

function parseIds(sessions: string): string[] {
	return sessions ? sessions.split(",").filter(Boolean) : [];
}

export function LiveTerminalsPage() {
	const navigate = useNavigate();
	const { sessions } = useSearch({ from: "/_shell/terminals" });
	const { daemonStatus } = useShell();
	const theme = useUiStore((s) => s.theme);
	const workspaceQuery = useWorkspaceQuery();
	const workspaces = workspaceQuery.data ?? [];

	const selectedIds = parseIds(sessions);
	const setIds = (ids: string[]) => void navigate({ to: "/terminals", search: { sessions: ids.join(",") } });

	const allSessions = workspaces.flatMap((workspace) =>
		workspace.sessions.map((session) => ({ session, projectName: workspace.name })),
	);
	const findSession = (id: string) => allSessions.find((entry) => entry.session.id === id);
	const availableToAdd = allSessions.filter((entry) => !selectedIds.includes(entry.session.id));

	return (
		<div className="flex h-full min-h-0 flex-col bg-background text-foreground">
			<DashboardSubhead
				title="Live Terminals"
				subtitle="Watch and message multiple sessions at once"
				actions={
					availableToAdd.length > 0 ? (
						<Select
							value=""
							onValueChange={(id) => setIds([...selectedIds, id])}
						>
							<SelectTrigger aria-label="Add a session" className="h-8 w-56 text-[13px]">
								<SelectValue placeholder="Add a session..." />
							</SelectTrigger>
							<SelectContent position="popper" align="end">
								{availableToAdd.map(({ session, projectName }) => (
									<SelectItem key={session.id} value={session.id}>
										{projectName} — {session.title}
									</SelectItem>
								))}
							</SelectContent>
						</Select>
					) : undefined
				}
			/>
			<div className="min-h-0 flex-1 overflow-y-auto p-[18px]">
				{selectedIds.length === 0 ? (
					<div className="flex h-full items-center justify-center text-passive">
						No sessions selected — use "Add a session" above.
					</div>
				) : (
					<div className="grid grid-cols-1 gap-4 lg:grid-cols-2 xl:grid-cols-3">
						{selectedIds.map((id) => {
							const entry = findSession(id);
							return (
								<TerminalTile
									key={id}
									sessionId={id}
									session={entry?.session}
									projectName={entry?.projectName}
									theme={theme}
									daemonReady={daemonStatus.state === "ready"}
									fontSize={TERMINAL_FONT_SIZE}
									onRemove={() => setIds(selectedIds.filter((existing) => existing !== id))}
								/>
							);
						})}
					</div>
				)}
			</div>
		</div>
	);
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && npx vitest run --config vite.renderer.config.ts src/renderer/components/LiveTerminalsPage.test.tsx`
Expected: PASS (3 tests)

- [ ] **Step 5: Typecheck**

Run: `cd frontend && npx tsc --noEmit -p .`
Expected: no new errors. `useSearch({ from: "/_shell/terminals" })` resolves its return type from the route tree generated for Task 1's route (TanStack Router's file-based route generation picks this up automatically from `_shell.terminals.tsx`'s `validateSearch`) — its shape matches `TerminalsSearch` from Task 1 by construction.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/renderer/components/LiveTerminalsPage.tsx frontend/src/renderer/components/LiveTerminalsPage.test.tsx
git commit -m "feat: build the Live Terminals grid with add/remove session picker"
```

---

### Task 4: "Watch Live" button on CEO Dashboard company cards

**Files:**
- Modify: `frontend/src/renderer/routes/_shell.index.tsx`
- Test: create `frontend/src/renderer/routes/_shell.index.test.tsx` (no existing test file for this route — confirmed via glob during planning)

**Interfaces:**
- Consumes: `GET /api/v1/org/overview` → `{ overview: { holdingHq？: { orchestratorSessionId？ }, companies: [{ id, hq？: { orchestratorSessionId？ }, projects: [{ orchestratorSessionId？ }] }] } }` (`frontend/src/api/schema.ts:934-950`); `useSelection`-style navigation is not available here (this route isn't inside `Sidebar.tsx`), so this task navigates directly via `useNavigate()` with the same `{ to: "/terminals", search: { sessions } }` shape Task 1 established.

- [ ] **Step 1: Write the failing test**

Create `frontend/src/renderer/routes/_shell.index.test.tsx`:

```tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

const { navigateMock, getMock } = vi.hoisted(() => ({ navigateMock: vi.fn(), getMock: vi.fn() }));

vi.mock("@tanstack/react-router", async (importOriginal) => {
	const actual = await importOriginal<typeof import("@tanstack/react-router")>();
	return { ...actual, useNavigate: () => navigateMock, createFileRoute: () => (opts: unknown) => opts };
});

vi.mock("../lib/api-client", () => ({
	apiClient: { GET: getMock, POST: vi.fn() },
	apiErrorMessage: (error: unknown, fallback = "Request failed") => (error instanceof Error ? error.message : fallback),
}));

vi.mock("../components/MigrationPopup", () => ({ MigrationPopup: () => null }));
vi.mock("../components/HQSection", () => ({ HQSection: () => null }));
vi.mock("../components/HeartbeatPauseSwitch", () => ({ HeartbeatPauseSwitch: () => null }));

import { CEODashboard } from "./_shell.index";

function renderDashboard() {
	getMock.mockImplementation(async (path: string) => {
		if (path === "/api/v1/companies")
			return { data: { companies: [{ id: "qb", name: "qb", createdAt: "2026-07-09T00:00:00Z" }] }, error: undefined };
		if (path === "/api/v1/projects")
			return { data: { projects: [{ id: "limbic-agentstation", name: "limbic-agentstation", path: "/x", companyId: "qb" }] }, error: undefined };
		if (path === "/api/v1/sessions") return { data: { sessions: [] }, error: undefined };
		if (path === "/api/v1/org/overview")
			return {
				data: {
					overview: {
						holdingHq: { projectId: "holding-hq", orchestratorSessionId: "holding-hq-1" },
						companies: [
							{
								id: "qb",
								name: "qb",
								hq: { projectId: "qb-hq", orchestratorSessionId: "qb-hq-1" },
								projects: [{ id: "limbic-agentstation", name: "limbic-agentstation", kind: "workspace", orchestratorSessionId: "limbic-agentstation-1", activeSessions: 0, totalSessions: 1 }],
							},
						],
						paused: false,
					},
				},
				error: undefined,
			};
		return { data: {}, error: undefined };
	});
	const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	render(
		<QueryClientProvider client={queryClient}>
			<CEODashboard />
		</QueryClientProvider>,
	);
}

describe("CEODashboard Watch Live", () => {
	it("navigates to /terminals pre-filled with CEO HQ, that company's PM HQ, and its worker orchestrator", async () => {
		const user = userEvent.setup();
		renderDashboard();

		await user.click(await screen.findByLabelText("Watch qb live"));

		await waitFor(() =>
			expect(navigateMock).toHaveBeenCalledWith({
				to: "/terminals",
				search: { sessions: "holding-hq-1,qb-hq-1,limbic-agentstation-1" },
			}),
		);
	});
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run --config vite.renderer.config.ts src/renderer/routes/_shell.index.test.tsx`
Expected: FAIL — `CEODashboard` is not an exported member of `./_shell.index` (it's currently only used internally as the route's `component`), and no "Watch qb live" label exists yet.

- [ ] **Step 3: Export `CEODashboard` and add the "Watch Live" button**

In `frontend/src/renderer/routes/_shell.index.tsx`:

1. Change `function CEODashboard() {` to `export function CEODashboard() {` (keep the existing `const Route = createFileRoute(...)({ component: CEODashboard })` unchanged — it still refers to the same function).

2. Add a `watchLive` handler and wire it into each non-unassigned company card. Insert this inside `CEODashboard`, near the top (after the existing `useState` declarations):

```ts
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
		const ids = [
			overview?.holdingHq?.orchestratorSessionId,
			company?.hq?.orchestratorSessionId,
			...(company?.projects.map((p) => p.orchestratorSessionId) ?? []),
		].filter((id): id is string => Boolean(id));
		navigate({ to: "/terminals", search: { sessions: ids.join(",") } });
	};
```

3. Inside the company-cards `.map((group) => { ... })`, add a "Watch Live" button to the card. It must stop click propagation so it doesn't also trigger the card's own navigate-to-company-dashboard `onClick`. Add it in `CardContent`, right after the existing stats `<div className="flex gap-4 mt-4">...</div>` block, but only for non-unassigned groups:

```tsx
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
```

4. Add `Terminal` to the existing `lucide-react` import at the top of the file:

```ts
import { Building2, Plus, ArrowRight, Briefcase, Terminal } from "lucide-react";
```

5. Show `watchLiveError` alongside the existing `error` banner (reuse the same styling, right after the existing `{error && (...)}` block):

```tsx
					{watchLiveError && (
						<div className="rounded-md border border-destructive/30 bg-destructive/10 p-4 text-destructive">
							{watchLiveError}
						</div>
					)}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && npx vitest run --config vite.renderer.config.ts src/renderer/routes/_shell.index.test.tsx`
Expected: PASS

- [ ] **Step 5: Run the full frontend suite and typecheck**

Run: `cd frontend && npx tsc --noEmit -p . && npx vitest run --config vite.renderer.config.ts`
Expected: typecheck clean; full suite green aside from the pre-existing, pre-verified-unrelated failures (`supervisor-link.test.ts` Windows socket-permission issue, and the 4 stale `Sidebar.test.tsx` "New project"/"Unassigned" tests already confirmed broken at `HEAD` before this feature — see the session's earlier investigation).

- [ ] **Step 6: Commit**

```bash
git add frontend/src/renderer/routes/_shell.index.tsx frontend/src/renderer/routes/_shell.index.test.tsx
git commit -m "feat: add Watch Live button to CEO Dashboard company cards"
```

---

## Manual end-to-end verification (after all 4 tasks)

1. `cd frontend && npm run dev` with the daemon running (HQ projects `holding-hq` and `qb-hq` already provisioned from this session's earlier work).
2. Settings → **Live Terminals** → confirm the empty state, then use "Add a session" to add `holding-hq-1` and `qb-hq-1` → confirm two tiles render with real terminal output and each accepts typed input.
3. Type a message in one tile's compose bar and click **Send** → confirm `GET /api/v1/projects/holding-hq/messages` (or `qb-hq`) shows the new message with no `senderSessionId` prefix, matching a human send.
4. From the CEO Dashboard, click **Watch Live** on the `qb` company card → confirm it navigates to `/terminals` with all three sessions (CEO HQ, PM HQ, the `limbic-agentstation` orchestrator) pre-filled.
5. Remove a tile via its × button → confirm the URL's `sessions` param updates and the grid re-renders with one fewer tile.
