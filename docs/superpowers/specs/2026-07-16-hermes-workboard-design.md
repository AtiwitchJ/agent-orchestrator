# Hermes Workboard Design

> Locked 2026-07-16 via grilling. Status: **closed spec**.

## Goal

Replace the session-centric kanban as the primary project surface with an
OpenClaw-compatible **Workboard**: durable work **cards** that Hermes (project
orchestrator) claims and dispatches to coding CLIs. Users supervise goals; Hermes
pulls work, answers agents when the user is away, and switches coding agents on
rate limits.

## Non-goals (v1)

- OpenClaw parent/child card graph, proof artifacts, multi-board namespaces
- Per-card fallback agent chains (fallback is project-level only)
- Arbitrary filesystem paths outside registered project repos
- Dual orchestrators (legacy free-form orchestrator + Hermes) running in parallel
- Live xterm preview on every running card simultaneously (focus pane is live)

## Core model

| Unit | Meaning |
|------|---------|
| Project | 1..n git repos (single repo or workspace). One default workboard. |
| Card | Work item. Exactly one **current** coding agent. Target folder on disk. |
| Hermes | Project orchestrator: pull/dispatch, handoff, answer-on-behalf, limit switch. |
| Coding session | Existing AO session/worktree/runtime attached to a card while `running`. |

### Columns (OpenClaw statuses)

`triage` → `backlog` → `todo` → `scheduled` → `ready` → `running` → `review` → `blocked` → `done`

Priorities: `low` | `normal` | `high` | `urgent`  
Labels: free-form strings (required at create — may be empty list only if product later relaxes; **v1 create requires labels field present**, at least one label recommended).

### Pull / WIP (Hybrid)

- User (and Hermes triage) move cards into `ready` (and `scheduled`).
- Hermes claims `ready` → `running` when `count(running) < wipLimit`.
- Default `wipLimit` = **3** (per-project setting).
- Priority order when claiming: `urgent` > `high` > `normal` > `low`, then FIFO by `ready_at`.
- `scheduled`: when `scheduled_at <= now`, Hermes promotes toward claim (into `ready` if WIP full, else may claim into `running`).

### Card create (required fields)

- `title`, `notes`, `priority`, `labels`
- `targetPath` — absolute path under a registered project repo (subdir allowed)
- `agent` — coding harness id; **no project default**; user must pick every time

### Folder binding

- Must resolve under a registered repo for that project (workspace child or single-repo root).
- Subfolders inside that repo are allowed; worktree is still created for the repo root; cwd/start context prefers the subfolder when launching the agent.

### Hermes = orchestrator seat

- `config.orchestrator.agent = "hermes"` for workboard-driven projects (migration path / setting).
- No second “head” process. Ad-hoc `POST /sessions` remains available but is secondary; primary UX is cards.
- Existing live sessions are **not** auto-migrated; user may “Add to Workboard”.

### Dispatch (`ready` → `running`)

1. Create/attach coding session (worker) with card’s agent + prompt from card notes/title.
2. Worktree for target repo; agent launch cwd respects subfolder when supported.
3. Link `card.session_id` ↔ session.
4. UI: compact terminal preview on card (throttled snapshot / last lines); **live xterm in focus panel**.

### Rate-limit switch

- Detect via TUI parse + Hermes judgment; adapter signals when available; user can force switch.
- Project-level ordered fallback list; skip agents in **1h cooldown** after limit.
- Switch = **new session in same worktree** + Hermes handoff prompt; card row updates `agent` / `session_id`.

### Answer-on-behalf

- Normal: wait **10 minutes** on agent question → Hermes reads project/card/code → answers.
- During wait: card stays `running` with **waiting** badge (not moved to `blocked`).
- `blocked` = real external/hard block.
- **Autonomous**: one button + settings panel — skip timeout / shortened timeout / sticky project-wide or one-shot.
- Default Autonomous = **off**.
- Dangerous actions: project **denylist** (with shipped defaults); always wait for user even in Autonomous.
- Persist Q&A on the card for audit.

### Retarget while `running`

| Level | UI | Behavior |
|-------|-----|----------|
| Nudge | Send to agent | Same session; card event `nudged` |
| Retarget | Change goal | Pause (WIP still held) → edit goal → Hermes handoff → resume same session or new session in same worktree |
| Split | New card | New worktree from same base; old card fate asked (`todo` recommended default); reference link only |

Autonomous must **not** Retarget for the user.

### Review / Done

- Move to `review` automatically when PR exists or agent signals completion; user can override.
- `done` is **user-only** (Hermes may suggest, not auto-complete).

### Tracker intake

- Matching issues create cards in **`triage`**.
- Hermes may auto-sort triage; user may also sort (A+B).

### Home surface

- With a recent project: open that project’s Workboard.
- Else: project list, then board.
- SessionsBoard remains reachable (secondary / legacy).

## Config additions (project JSON)

```json
{
  "workboard": {
    "wipLimit": 3,
    "fallbackAgents": ["codex", "claude-code", "kilo"],
    "limitCooldownMinutes": 60,
    "answerTimeoutMinutes": 10,
    "autonomous": {
      "enabled": false,
      "mode": "skip_timeout",
      "shortTimeoutMinutes": 2,
      "sticky": true
    },
    "answerDenylist": ["force_push", "delete_repo", "exfil_secret"]
  }
}
```

Exact enum strings are fixed in the implementation plan.

## Data (sketch)

Table `work_cards` (durable facts only; display badges derived at read time where possible):

- identity: `id`, `project_id`, `board_id` (v1 always `default`)
- content: `title`, `notes`, `priority`, `labels_json`
- placement: `status`, `scheduled_at`, `ready_at`, `position`
- binding: `target_path`, `repo_name` (optional denorm), `agent`, `session_id` (nullable)
- control: `waiting_for_input`, `paused_retarget`, `goal_version`, `superseded_by_card_id`
- audit: `created_at`, `updated_at`

CDC: `work_card_changed` (coarse) into `change_log` via triggers (same rebuild pattern as policy tables).

Related: `work_card_events` for nudge/retarget/answer/switch audit trail (or JSON append — prefer separate table for queryability).

## API sketch

- `GET/POST /api/v1/projects/{id}/workboard/cards`
- `GET/PATCH /api/v1/workboard/cards/{id}`
- `POST .../move`, `.../nudge`, `.../retarget`, `.../split`, `.../switch-agent`
- `POST .../projects/{id}/workboard/autonomous` (set mode)
- Dispatcher is daemon-internal (Hermes tools + periodic reconcile), not a raw “claim” from random clients in v1 — CLI may expose `ao workboard dispatch` later.

## Relationship to existing code

- Reuse session spawn, worktree, terminal mux, project config blob, SSE invalidation.
- Hermes adapter already registered; hooks/activity are weak — dispatcher must not rely on Hermes hooks for card state.
- Do not overload `sessions` rows as cards.

## Phased delivery

1. **Foundation** — schema, CRUD API, CDC, Workboard UI (columns, create, drag), config stub  
2. **Dispatch** — WIP claim, session link, focus TUI + card preview, Hermes orchestrator default  
3. **Autonomy** — answer timeout, Autonomous UI, denylist, limit detect + fallback switch  
4. **Retarget + intake** — Nudge/Retarget/Split, scheduled promote, tracker→triage, home route  

Each phase ships testable software on its own.
