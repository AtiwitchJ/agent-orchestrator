# Live Terminals: Multi-Session Terminal Tiling — Design

> **Status:** ready for plan. Grounded against the working tree on 2026-07-09.
> Every "current state" claim carries a `file:line` reference.

---

## 0. Goal

Right now the only way to watch an orchestrator's terminal is to navigate to
its single session view one at a time — there is no way to watch the CEO HQ,
a Company PM HQ, and a Worker session simultaneously to see whether they are
actually coordinating (this is exactly what motivated the feature: manually
verifying CEO/PM heartbeat coordination required curling the API instead of
watching it happen). This design adds a **generic multi-pane "Live Terminals"
view**: any number of existing sessions tiled side by side, each fully
interactive (type directly into the PTY, same as today), plus a compose bar
per tile that sends a message via the daemon's existing (but never
UI-exposed) message-send endpoint — reusing the exact primitive the org
heartbeat itself uses to nudge orchestrators.

No new backend work is required. This is a frontend-only feature built out of
already-existing, already-tested pieces.

---

## 1. Ground truth (what the code is today)

| Fact | Value | Source |
|---|---|---|
| Per-session terminal component | `TerminalPane` — self-contained, takes a `session` prop, owns its own PTY attach/reattach lifecycle via `useTerminalSession` | `frontend/src/renderer/components/TerminalPane.tsx:12-53` |
| `TerminalPane` props | `{ session?, theme, daemonReady, terminalTarget?, fontSize }` | `TerminalPane.tsx:12-18` |
| Fuller pane with zoom toolbar | `CenterPane` wraps `TerminalPane` + owns `fontSize` state (persisted to `localStorage["ao.terminal.fontSize"]`) + zoom buttons | `frontend/src/renderer/components/CenterPane.tsx:16,39,110-165` |
| Existing single-session usage | `SessionView.tsx` renders `<CenterPane daemonReady=... session=... terminalTarget=... theme=... />` inside a resizable panel | `frontend/src/renderer/components/SessionView.tsx:194-200` |
| `theme` source | `useUiStore()` at the shell level | `frontend/src/renderer/routes/_shell.tsx:57` |
| `daemonReady` source | `daemonStatus.state === "ready"`, `daemonStatus` from `useShell()` / `ShellContextValue` | `SessionView.tsx:195`; `frontend/src/renderer/lib/shell-context.ts:8` |
| Send-message endpoint | `POST /api/v1/sessions/{sessionId}/send` — **already implemented**, used today only by the `ao send` CLI and the heartbeat's internal sender | `backend/internal/httpd/apispec/openapi.yaml:1822-1845`; `backend/internal/cli/send.go:41-59` |
| Send request/response shape | `SendSessionMessageRequest { message (1-4096 chars, required), senderSessionId? }` → `SendSessionMessageResponse { ok, sessionId, message? }` | `openapi.yaml:2868-2889` |
| Existing read-only messages panel | `SessionMessagesPanel` — project-scoped, read-only; its own comment notes the only way to send today is `ao send` from the CLI | `frontend/src/renderer/components/SessionMessagesPanel.tsx:6,56` |
| **No send-message UI exists anywhere in the app today** | confirmed — grep for a compose/send input across `frontend/src` found none | verified this session |
| All sessions across all projects | `useWorkspaceQuery()` → `WorkspaceSummary[]`, each with a `.sessions: WorkspaceSession[]` | `frontend/src/renderer/hooks/useWorkspaceQuery.ts` (existing hook, used throughout `Sidebar.tsx`) |
| Top-level route pattern to mirror | `_shell.prs.tsx` — a one-line route file wrapping a single page component | `frontend/src/renderer/routes/_shell.prs.tsx:1-7` |
| Nav-entry pattern to mirror | Settings dropdown menu item `<DropdownMenuItem onSelect={selection.goPrs}>` | `frontend/src/renderer/components/Sidebar.tsx` (Settings footer dropdown, both expanded and icon-rail variants) |
| `useSelection()` navigation hook | Already has `goPrs: () => navigate({ to: "/prs" })` alongside `goHome`, `goProject`, `goCompany`, `goSession` | `Sidebar.tsx` `useSelection()` |
| CEO/company HQ session lookup | `GET /api/v1/org/overview` already returns `holdingHq.orchestratorSessionId`, `companies[].hq.orchestratorSessionId`, and each company's `projects[].orchestratorSessionId` | verified live this session (`/api/v1/org/overview` response) |

---

## 2. Decisions locked

1. **One generic view, two entry points** (user-confirmed "both"):
   - A "Live Terminals" item in the Settings dropdown menu (same pattern as
     "Pull requests") → opens `/terminals` with whatever was last selected
     (or empty), and a picker to add any session.
   - A button on the CEO Dashboard (per-company card, or a company-scoped
     action) that navigates to `/terminals` **pre-populated** with that
     company's PM HQ session + the CEO HQ session + its most recent active
     worker session, sourced from `GET /api/v1/org/overview`.
2. **Selected sessions live in the URL** (`?sessions=id1,id2,id3` search
   param) — reload-safe and linkable/shareable, not component state.
3. **Fully interactive per tile** (user-confirmed: "ดูได้พิมพ์ได้"): each tile
   is a real `TerminalPane`, typing goes straight into the PTY exactly like
   the existing single-session view — no new terminal plumbing.
4. **A separate compose bar per tile** (user-confirmed: "มีปุ่มไว้ส่งคำสั่ง"):
   a text input + "Send" button beneath each tile, calling the *existing*
   `POST /sessions/{id}/send` endpoint. This is deliberately distinct from
   typing into the PTY — it queues a message for the agent's next turn
   (exactly like a heartbeat nudge), which is more reliable than raw
   keystrokes when a tile's agent is mid-turn.
5. **No new backend endpoints, no new IPC handlers.** Every primitive this
   feature needs already exists and is already tested.
6. **Non-goals for v1:** no drag-to-reorder/resize of tiles, no persisting
   tile layout beyond the URL, no historical transcript scrollback beyond
   what `TerminalPane` already buffers, no notification/alerting on tile
   activity.

---

## 3. Scope

**In scope:**
- New route `frontend/src/renderer/routes/_shell.terminals.tsx` (mirrors
  `_shell.prs.tsx`'s one-line wrapper pattern) rendering a new
  `LiveTerminalsPage` component.
- `LiveTerminalsPage`: reads `?sessions=` search param, resolves each id to
  its `WorkspaceSession` + parent `WorkspaceSummary` via `useWorkspaceQuery`,
  renders a responsive grid of `TerminalTile`, plus a session picker
  (combobox, reusing the existing `Select`/`Command`-style pattern already in
  the codebase) to add sessions and a per-tile remove (×) control.
- `TerminalTile` component: header (project name + session title/role),
  `TerminalPane` body, compose bar footer. Owns its own `useMutation` calling
  `apiClient.POST("/api/v1/sessions/{sessionId}/send", ...)`.
- `goTerminals(sessionIds?: string[])` added to `useSelection()` in
  `Sidebar.tsx`, navigating to `/terminals` with the search param set.
- New "Live Terminals" `DropdownMenuItem` in the Settings footer dropdown
  (both expanded and icon-rail variants), calling `goTerminals()` with no
  pre-fill.
- A "Watch Live" (or similar) action on the CEO Dashboard company card
  (`_shell.index.tsx`) / company dashboard page, fetching
  `/api/v1/org/overview`, extracting that company's HQ + CEO HQ + most recent
  active worker session ids, and calling `goTerminals([...ids])`.

**Out of scope (confirmed above):** any backend/schema changes; tile
resizing/persistence; anything beyond what's listed in Decisions Locked §6.

---

## 4. Component & data flow

```
Settings dropdown ──goTerminals()──┐
                                    ├──> /terminals?sessions=a,b,c
CEO Dashboard card ──goTerminals([hqId, pmId, workerId])──┘
                                          │
                                    LiveTerminalsPage
                                    (reads search param,
                                     useWorkspaceQuery for session data)
                                          │
                              ┌───────────┼───────────┐
                         TerminalTile TerminalTile TerminalTile ...
                         │  header            │            │
                         │  <TerminalPane/>   │            │
                         │  compose bar ──POST /sessions/{id}/send
                         └────────────────────┴────────────┘
                                          │
                                 session picker (add/remove)
                                 mutates the ?sessions= search param
```

Each `TerminalTile` is independent — its own `TerminalPane` instance manages
its own WebSocket/PTY attach exactly as the single-session view does today;
tiling N of them requires no changes to that lifecycle.

---

## 5. Error handling

- **Unknown/stale session id in the URL** (session ended, project removed):
  the tile shows a small "Session no longer available" placeholder with a
  remove button, instead of crashing — mirrors how `TerminalPane` already
  handles an undefined `session` prop (falls through to its "empty" terminal
  key path, `TerminalPane.tsx:21-22`).
- **Send failure:** reuse `apiErrorMessage` (already fixed this session to
  surface `details.suggestedFix`) to show an inline error under that tile's
  compose bar; failure in one tile must not affect others.
- **CEO Dashboard pre-fill when a company has no HQ/worker yet:** only
  pre-fill the ids that actually exist; never block navigation waiting on a
  provision step the user hasn't triggered.

---

## 6. Testing

- `TerminalTile` unit test: renders a session, mounts `TerminalPane` (mock/
  stub, matching existing `SessionView`/`CenterPane` test conventions),
  submits the compose bar, asserts the mocked `apiClient.POST` was called
  with `/api/v1/sessions/{id}/send` and the typed message.
- `LiveTerminalsPage` test: given a `?sessions=` param, asserts one tile
  renders per id; adding via the picker updates the search param; removing a
  tile does too.
- `useSelection().goTerminals` covered indirectly via the existing
  `Sidebar.test.tsx` navigation-mock pattern (`navigateMock`).
- No backend tests needed (no backend changes).

---

## 7. Open items for the plan phase

- Exact combobox/picker component to reuse (candidates: the existing
  `Select` primitives, or a `Command`-style searchable list if the session
  count grows large) — decide during planning after checking what's already
  used for similar "pick one of many workspaces" pickers in this codebase.
- Exact grid breakpoints (2-up / 3-up / auto-fit) — a implementation detail,
  not a design fork.
