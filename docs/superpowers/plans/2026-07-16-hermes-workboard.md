# Hermes Workboard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship an OpenClaw-compatible project Workboard backed by durable `work_cards`, with Hermes as the claiming orchestrator, coding-CLI dispatch, limit failover, answer-on-behalf, and running-card retarget — delivered in four independently testable phases.

**Architecture:** New `work_cards` (+ events) SQLite domain with CDC into `change_log`; service + HTTP under `/api/v1/.../workboard`; daemon dispatcher that claims `ready`→`running` under WIP and spawns existing worker sessions; Electron Workboard UI replaces SessionsBoard as the primary project surface. Hermes occupies `config.orchestrator` — no parallel head.

**Tech Stack:** Go daemon (chi, sqlc, goose), SQLite WAL, existing session_manager/terminal mux, React 19 + TanStack Router/Query, xterm mux WebSocket, OpenAPI → `frontend/src/api/schema.ts`.

**Spec:** `docs/superpowers/specs/2026-07-16-hermes-workboard-design.md`

## Global Constraints

- All app state under `~/.ao` only (`AO_DATA_DIR` / `AO_RUN_FILE` overrides).
- Daemon loopback-only (`127.0.0.1`); CLI is thin HTTP client.
- Never edit merged goose migrations; next file is `0030_*.sql`.
- Never hand-edit `backend/internal/storage/sqlite/gen/*`; run `npm run sqlc`.
- After DTO/API changes run `npm run api` and commit `openapi.yaml` + `schema.ts`.
- Do not store derived display status; derive waiting badges at read time from durable flags.
- Failed runtime probes are not proof of death (existing LCM rule).
- Phase 1 must not depend on Hermes hooks (they are no-ops today).
- Conventional commits; one concern per commit at end of each task.
- Match existing controller/service/test patterns; keep changes surgical.

## File map (locked decomposition)

| Path | Responsibility |
|------|----------------|
| `backend/internal/domain/workboard.go` | Card status/priority enums, card aggregate, validation |
| `backend/internal/domain/projectconfig.go` | `WorkboardConfig` nested in `ProjectConfig` |
| `backend/internal/storage/sqlite/migrations/0030_add_workboard.sql` | Tables + CDC triggers |
| `backend/internal/storage/sqlite/queries/workboard.sql` | sqlc queries |
| `backend/internal/storage/sqlite/store/workboard_store.go` | Store API |
| `backend/internal/service/workboard/` | CRUD, move, list, path validation |
| `backend/internal/service/workboard/dispatch.go` | WIP claim + session spawn (phase 2+) |
| `backend/internal/httpd/controllers/workboard.go` | HTTP handlers |
| `backend/internal/httpd/controllers/dto.go` | Request/response DTOs |
| `backend/internal/observe/trackerintake/` | Emit cards to triage (phase 4) |
| `frontend/src/renderer/components/Workboard.tsx` | Kanban columns + cards |
| `frontend/src/renderer/components/WorkCard.tsx` | Card chrome + preview slot |
| `frontend/src/renderer/components/CreateWorkCardDialog.tsx` | Required create fields |
| `frontend/src/renderer/components/WorkCardFocusPanel.tsx` | Live TUI + retarget menu (phase 2+) |
| `frontend/src/renderer/hooks/useWorkboardQuery.ts` | Query + CDC invalidation |
| `frontend/src/renderer/routes/_shell.projects.$projectId.tsx` | Mount Workboard as primary |

---

# Phase 1 — Foundation (CRUD + board UI)

Ship: durable cards, REST, CDC, Workboard UI with create + drag across OpenClaw columns. No auto-dispatch yet.

### Task 1: Domain types for workboard cards

**Files:**
- Create: `backend/internal/domain/workboard.go`
- Create: `backend/internal/domain/workboard_test.go`
- Modify: `backend/internal/domain/projectconfig.go` (add `Workboard WorkboardConfig`)

**Interfaces:**
- Produces: `domain.CardStatus`, `domain.CardPriority`, `domain.WorkCard`, `domain.WorkboardConfig`, `ValidateCardStatus`, `ValidateTargetPathUnderRepos`

- [x] **Step 1: Write the failing test**

```go
// backend/internal/domain/workboard_test.go
package domain_test

import (
	"testing"

	"github.com/modernagent/modern-agent/backend/internal/domain"
)

func TestParseCardStatus(t *testing.T) {
	got, err := domain.ParseCardStatus("ready")
	if err != nil || got != domain.CardStatusReady {
		t.Fatalf("got %v %v", got, err)
	}
	if _, err := domain.ParseCardStatus("nope"); err == nil {
		t.Fatal("expected error")
	}
}

func TestWorkboardConfigDefaults(t *testing.T) {
	d := domain.DefaultWorkboardConfig()
	if d.WIPLimit != 3 || d.AnswerTimeoutMinutes != 10 || d.LimitCooldownMinutes != 60 {
		t.Fatalf("unexpected defaults: %+v", d)
	}
	if d.Autonomous.Enabled {
		t.Fatal("autonomous should default off")
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/domain/ -run 'TestParseCardStatus|TestWorkboardConfigDefaults' -count=1`  
Expected: FAIL (undefined types/funcs)

- [x] **Step 3: Write minimal implementation**

```go
// backend/internal/domain/workboard.go
package domain

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type CardStatus string

const (
	CardStatusTriage    CardStatus = "triage"
	CardStatusBacklog   CardStatus = "backlog"
	CardStatusTodo      CardStatus = "todo"
	CardStatusScheduled CardStatus = "scheduled"
	CardStatusReady     CardStatus = "ready"
	CardStatusRunning   CardStatus = "running"
	CardStatusReview    CardStatus = "review"
	CardStatusBlocked   CardStatus = "blocked"
	CardStatusDone      CardStatus = "done"
)

func ParseCardStatus(s string) (CardStatus, error) {
	switch CardStatus(s) {
	case CardStatusTriage, CardStatusBacklog, CardStatusTodo, CardStatusScheduled,
		CardStatusReady, CardStatusRunning, CardStatusReview, CardStatusBlocked, CardStatusDone:
		return CardStatus(s), nil
	default:
		return "", fmt.Errorf("invalid card status %q", s)
	}
}

type CardPriority string

const (
	CardPriorityLow    CardPriority = "low"
	CardPriorityNormal CardPriority = "normal"
	CardPriorityHigh   CardPriority = "high"
	CardPriorityUrgent CardPriority = "urgent"
)

func ParseCardPriority(s string) (CardPriority, error) {
	switch CardPriority(s) {
	case CardPriorityLow, CardPriorityNormal, CardPriorityHigh, CardPriorityUrgent:
		return CardPriority(s), nil
	default:
		return "", fmt.Errorf("invalid card priority %q", s)
	}
}

// PriorityRank higher = claimed sooner.
func (p CardPriority) Rank() int {
	switch p {
	case CardPriorityUrgent:
		return 4
	case CardPriorityHigh:
		return 3
	case CardPriorityNormal:
		return 2
	case CardPriorityLow:
		return 1
	default:
		return 0
	}
}

type WorkCard struct {
	ID                string
	ProjectID         string
	BoardID           string
	Title             string
	Notes             string
	Priority          CardPriority
	Labels            []string
	Status            CardStatus
	ScheduledAt       *time.Time
	ReadyAt           *time.Time
	Position          int64
	TargetPath        string
	RepoName          string
	Agent             string
	SessionID         string
	WaitingForInput   bool
	PausedRetarget    bool
	GoalVersion       int
	SupersededByCardID string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type WorkboardAutonomousConfig struct {
	Enabled              bool   `json:"enabled,omitempty"`
	Mode                 string `json:"mode,omitempty"` // skip_timeout | short_timeout
	ShortTimeoutMinutes  int    `json:"shortTimeoutMinutes,omitempty"`
	Sticky               bool   `json:"sticky,omitempty"`
}

type WorkboardConfig struct {
	WIPLimit              int                       `json:"wipLimit,omitempty"`
	FallbackAgents        []string                  `json:"fallbackAgents,omitempty"`
	LimitCooldownMinutes   int                       `json:"limitCooldownMinutes,omitempty"`
	AnswerTimeoutMinutes  int                       `json:"answerTimeoutMinutes,omitempty"`
	Autonomous            WorkboardAutonomousConfig `json:"autonomous,omitempty"`
	AnswerDenylist        []string                  `json:"answerDenylist,omitempty"`
}

func DefaultWorkboardConfig() WorkboardConfig {
	return WorkboardConfig{
		WIPLimit:             3,
		LimitCooldownMinutes:  60,
		AnswerTimeoutMinutes: 10,
		Autonomous: WorkboardAutonomousConfig{
			Mode:                "skip_timeout",
			ShortTimeoutMinutes: 2,
			Sticky:              true,
		},
		AnswerDenylist: []string{"force_push", "delete_repo", "exfil_secret"},
	}
}

// TargetPathAllowed reports whether absPath is under one of the registered repo roots.
func TargetPathAllowed(absPath string, repoRoots []string) bool {
	clean := filepath.Clean(absPath)
	for _, root := range repoRoots {
		r := filepath.Clean(root)
		if clean == r || strings.HasPrefix(clean, r+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
```

Add to `ProjectConfig`:

```go
Workboard WorkboardConfig `json:"workboard,omitempty"`
```

Update `MarshalJSON` map copy in `projectconfig.go` to include `workboard` when non-zero (follow existing policy omit pattern: include when `WIPLimit != 0 || len(FallbackAgents) > 0 || Autonomous.Enabled || ...` — simplest: always omitempty via pointer OR treat zero WIPLimit as “use default at read”). Prefer **read-time default merge** in service: if `WIPLimit == 0`, apply `DefaultWorkboardConfig().WIPLimit`.

- [x] **Step 4: Run tests**

Run: `cd backend && go test ./internal/domain/ -run 'TestParseCardStatus|TestWorkboardConfigDefaults' -count=1`  
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add backend/internal/domain/workboard.go backend/internal/domain/workboard_test.go backend/internal/domain/projectconfig.go
git commit -m "$(cat <<'EOF'
feat(domain): add workboard card types and config defaults

EOF
)"
```

---

### Task 2: Migration `0030_add_workboard` + sqlc

**Files:**
- Create: `backend/internal/storage/sqlite/migrations/0030_add_workboard.sql`
- Create: `backend/internal/storage/sqlite/queries/workboard.sql`
- Modify: change_log CHECK rebuild (same dance as `0029_add_policy_tables.sql`) admitting `work_card_changed`
- Regenerate: `npm run sqlc`

**Interfaces:**
- Consumes: domain statuses as TEXT
- Produces: sqlc methods `InsertWorkCard`, `GetWorkCard`, `ListWorkCardsByProject`, `UpdateWorkCard`, `DeleteWorkCard`, `CountRunningCards`

- [x] **Step 1: Write migration Up (tables)**

```sql
-- +goose Up
CREATE TABLE work_cards (
    id                   TEXT PRIMARY KEY,
    project_id           TEXT NOT NULL,
    board_id             TEXT NOT NULL DEFAULT 'default',
    title                TEXT NOT NULL,
    notes                TEXT NOT NULL DEFAULT '',
    priority             TEXT NOT NULL,
    labels_json          TEXT NOT NULL DEFAULT '[]',
    status               TEXT NOT NULL,
    scheduled_at         INTEGER,
    ready_at             INTEGER,
    position             INTEGER NOT NULL DEFAULT 0,
    target_path          TEXT NOT NULL,
    repo_name            TEXT NOT NULL DEFAULT '',
    agent                TEXT NOT NULL,
    session_id           TEXT NOT NULL DEFAULT '',
    waiting_for_input    INTEGER NOT NULL DEFAULT 0,
    paused_retarget      INTEGER NOT NULL DEFAULT 0,
    goal_version         INTEGER NOT NULL DEFAULT 1,
    superseded_by_card_id TEXT NOT NULL DEFAULT '',
    created_at           INTEGER NOT NULL,
    updated_at           INTEGER NOT NULL
);

CREATE INDEX idx_work_cards_project_status ON work_cards(project_id, status, priority, ready_at);
CREATE INDEX idx_work_cards_session ON work_cards(session_id);

CREATE TABLE work_card_events (
    id         TEXT PRIMARY KEY,
    card_id    TEXT NOT NULL REFERENCES work_cards(id) ON DELETE CASCADE,
    project_id TEXT NOT NULL,
    kind       TEXT NOT NULL,
    payload    TEXT NOT NULL DEFAULT '{}',
    created_at INTEGER NOT NULL
);

CREATE INDEX idx_work_card_events_card ON work_card_events(card_id, created_at);
```

Then rebuild `change_log` CHECK to include `work_card_changed` using the **exact pattern** in `0029_add_policy_tables.sql` (copy trigger bodies from current migration head). Add AFTER INSERT/UPDATE/DELETE triggers on `work_cards` emitting `work_card_changed` with `json_object('card_id', NEW.id, 'status', NEW.status, ...)`.

Down: reverse rebuild dropping new event type and tables.

- [x] **Step 2: Write sqlc queries**

```sql
-- backend/internal/storage/sqlite/queries/workboard.sql
-- name: InsertWorkCard :exec
INSERT INTO work_cards (
  id, project_id, board_id, title, notes, priority, labels_json, status,
  scheduled_at, ready_at, position, target_path, repo_name, agent, session_id,
  waiting_for_input, paused_retarget, goal_version, superseded_by_card_id,
  created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetWorkCard :one
SELECT * FROM work_cards WHERE id = ?;

-- name: ListWorkCardsByProject :many
SELECT * FROM work_cards
WHERE project_id = ? AND board_id = ?
ORDER BY status, position, created_at;

-- name: UpdateWorkCard :exec
UPDATE work_cards SET
  title = ?, notes = ?, priority = ?, labels_json = ?, status = ?,
  scheduled_at = ?, ready_at = ?, position = ?, target_path = ?, repo_name = ?,
  agent = ?, session_id = ?, waiting_for_input = ?, paused_retarget = ?,
  goal_version = ?, superseded_by_card_id = ?, updated_at = ?
WHERE id = ?;

-- name: CountRunningCards :one
SELECT COUNT(*) FROM work_cards
WHERE project_id = ? AND status = 'running';

-- name: InsertWorkCardEvent :exec
INSERT INTO work_card_events (id, card_id, project_id, kind, payload, created_at)
VALUES (?, ?, ?, ?, ?, ?);
```

- [x] **Step 3: Regenerate**

Run: `npm run sqlc`  
Expected: `backend/internal/storage/sqlite/gen/workboard.sql.go` created; build clean

- [x] **Step 4: Smoke migrate in tests**

Run: `cd backend && go test ./internal/storage/sqlite/... -count=1`  
Expected: PASS (existing migrate tests apply 0030)

- [x] **Step 5: Commit**

```bash
git add backend/internal/storage/sqlite/migrations/0030_add_workboard.sql \
  backend/internal/storage/sqlite/queries/workboard.sql \
  backend/internal/storage/sqlite/gen/
git commit -m "$(cat <<'EOF'
feat(storage): add work_cards schema and sqlc queries

EOF
)"
```

---

### Task 3: Workboard store + service CRUD

**Files:**
- Create: `backend/internal/storage/sqlite/store/workboard_store.go`
- Create: `backend/internal/service/workboard/service.go`
- Create: `backend/internal/service/workboard/service_test.go`
- Wire store into daemon/sqlite facade the same way other stores are exposed

**Interfaces:**
- Consumes: sqlc gen, `domain.WorkCard`
- Produces:
  - `workboard.Service.Create(ctx, CreateInput) (WorkCard, error)`
  - `List(ctx, projectID, boardID) ([]WorkCard, error)`
  - `Get(ctx, id) (WorkCard, error)`
  - `Move(ctx, id, status, position) (WorkCard, error)`
  - `Update(ctx, id, UpdateInput) (WorkCard, error)`

```go
type CreateInput struct {
	ProjectID  string
	BoardID    string // default "default"
	Title      string
	Notes      string
	Priority   domain.CardPriority
	Labels     []string
	Status     domain.CardStatus // default triage
	TargetPath string
	Agent      string
	ScheduledAt *time.Time
}
```

Validation in Create:
- title/notes/priority/labels/targetPath/agent required (labels may be empty slice but field must be provided — reject `nil` if you distinguish; for JSON use `[]string{}` minimum length 0 allowed **only if** create DTO always sends array; spec said labels required — enforce `len(labels) >= 1` in service).
- Resolve project repo roots; `domain.TargetPathAllowed`.
- On status `ready`, set `ReadyAt=now`.

- [x] **Step 1: Failing service test** (httptest or in-memory sqlite per package pattern)

```go
func TestCreateRequiresAgentAndPath(t *testing.T) {
	svc := newTestService(t) // helper opens temp sqlite + registers one repo root
	_, err := svc.Create(context.Background(), workboard.CreateInput{
		ProjectID: "p1", Title: "t", Notes: "n", Priority: domain.CardPriorityNormal,
		Labels: []string{"bug"}, TargetPath: "/nope", Agent: "",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}
```

- [x] **Step 2: Run — expect FAIL**

Run: `cd backend && go test ./internal/service/workboard/ -count=1`

- [x] **Step 3: Implement store mapping + service**

Map INTEGER ms timestamps ↔ `time.Time`; labels JSON via `encoding/json`.

- [x] **Step 4: Run — expect PASS**

- [x] **Step 5: Commit**

```bash
git commit -m "$(cat <<'EOF'
feat(workboard): add store and CRUD service with path validation

EOF
)"
```

---

### Task 4: HTTP API + OpenAPI

**Files:**
- Create: `backend/internal/httpd/controllers/workboard.go`
- Create: `backend/internal/httpd/controllers/workboard_test.go`
- Modify: `backend/internal/httpd/controllers/dto.go`
- Modify: `backend/internal/httpd/apispec/specgen/build.go` (`schemaNames` for new types)
- Modify: router registration in daemon/httpd wiring
- Run: `npm run api`

**Interfaces:**
- Produces DTOs:
  - `WorkCardResponse`, `CreateWorkCardRequest`, `MoveWorkCardRequest`, `ListWorkCardsResponse`
- Routes:
  - `GET /api/v1/projects/{projectId}/workboard/cards`
  - `POST /api/v1/projects/{projectId}/workboard/cards`
  - `GET /api/v1/workboard/cards/{cardId}`
  - `PATCH /api/v1/workboard/cards/{cardId}`
  - `POST /api/v1/workboard/cards/{cardId}/move` body `{ "status": "ready", "position": 0 }`

- [x] **Step 1: Failing controller test** (follow `prs_test.go` / sessions pattern with fake service)

```go
func TestCreateWorkCard_Validation(t *testing.T) {
	// POST missing agent → 400 INVALID envelope
}
```

- [x] **Step 2: Implement handlers + register**

- [x] **Step 3: `npm run api` && `cd backend && go test ./internal/httpd/... -count=1`**

Expected: PASS; `openapi.yaml` + `frontend/src/api/schema.ts` updated

- [x] **Step 4: Commit** including generated API artifacts

```bash
git commit -m "$(cat <<'EOF'
feat(api): expose workboard card CRUD and move endpoints

EOF
)"
```

---

### Task 5: SSE invalidation for work cards

**Files:**
- Modify: frontend `CDC_EVENT_TYPES` (or equivalent in `event-transport.ts`) to include `work_card_changed`
- Modify: invalidate `workboardQueryKey(projectId)` on that event
- Backend: ensure trigger payload includes `project_id` so clients can scope

- [x] **Step 1: Unit test** that event type is in the allowlist (frontend vitest) or backend trigger test if pattern exists

- [x] **Step 2: Implement**

- [x] **Step 3: Commit**

```bash
git commit -m "$(cat <<'EOF'
feat(events): invalidate workboard queries on work_card_changed

EOF
)"
```

---

### Task 6: Frontend Workboard UI (columns + create + drag)

**Files:**
- Create: `frontend/src/renderer/hooks/useWorkboardQuery.ts`
- Create: `frontend/src/renderer/components/Workboard.tsx`
- Create: `frontend/src/renderer/components/WorkCard.tsx`
- Create: `frontend/src/renderer/components/CreateWorkCardDialog.tsx`
- Create: matching `*.test.tsx`
- Modify: `frontend/src/renderer/routes/_shell.projects.$projectId.tsx` — render `Workboard` as primary; keep SessionsBoard behind a “Sessions” toggle/link

**Interfaces:**
- `useWorkboardCards(projectId)` → `WorkCard[]`
- Columns constant matching OpenClaw order
- Create dialog fields: title, notes, priority, labels (chip input), folder picker (`ao.chooseDirectory` then validate via API error), agent select (from agents catalog — no default selected)

- [x] **Step 1: Vitest — CreateWorkCardDialog refuses submit without agent**

- [x] **Step 2: Implement components** (reuse SessionsBoard layout/CSS variables where possible; new column labels)

Drag: HTML5 drag-and-drop or existing pattern; on drop call `POST .../move`.

- [x] **Step 3: `cd frontend && npm run typecheck`** + targeted vitest

- [x] **Step 4: Commit**

```bash
git commit -m "$(cat <<'EOF'
feat(ui): add OpenClaw-style workboard with create and drag-move

EOF
)"
```

---

### Task 7: Phase 1 gate

- [x] **Step 1: Run**

```bash
cd backend && go test ./internal/domain/ ./internal/service/workboard/ ./internal/httpd/... -count=1
cd frontend && npm run typecheck
```

Expected: PASS

- [ ] **Step 2: Manual smoke** — add project, open workboard, create card in triage, drag to ready, refresh persists

- [ ] **Step 3: Commit any fixes; tag phase complete in PR body**

---

# Phase 2 — Dispatch + TUI

### Task 8: Claim algorithm + dispatcher loop

**Files:**
- Create: `backend/internal/service/workboard/dispatch.go`
- Create: `backend/internal/service/workboard/dispatch_test.go`
- Wire ticker in `backend/internal/daemon/` (similar to tracker intake / heartbeat)

**Interfaces:**
- `DispatchOnce(ctx, projectID) (claimed []string, err error)`
- Select candidates: `status=ready` OR (`scheduled` AND `scheduled_at<=now`) promoted to ready first
- Order by priority rank DESC, `ready_at` ASC
- While `CountRunning < WIPLimit`, claim next: set `running`, spawn worker via existing `session.Service.Spawn` with `Harness=card.Agent`, `Prompt=title+"\n\n"+notes`, record `session_id`
- Skip cards with `PausedRetarget`

- [x] TDD: table test for ordering + WIP cap with fake spawn
- [x] Commit: `feat(workboard): claim ready cards under WIP and spawn workers`

### Task 9: Force Hermes orchestrator for workboard projects

**Files:**
- Modify project settings UI + `spawn-orchestrator.ts` defaults when workboard enabled
- Optional: on first workboard open, if no orch, spawn with hermes

- [x] Commit: `feat(workboard): prefer hermes as project orchestrator`

### Task 10: Card focus panel + preview

**Files:**
- `WorkCardFocusPanel.tsx` — reuse `TerminalPane`/`useTerminalSession` when `card.sessionId` set
- Card preview: poll last N lines or lightweight snapshot endpoint if needed; v1 may show status + “Open terminal” until snapshot exists — **prefer** attaching a read-only mux consumer throttled to 2fps for the focused card only; non-focused cards show activity badge + last event

- [x] Commit: `feat(ui): live terminal focus panel for running work cards`

---

# Phase 3 — Autonomy + limit switch

### Task 11: Waiting-for-input + answer timeout

**Files:**
- `service/workboard/answer.go`
- Detect needs_input via existing session activity_state / notifications
- Set `waiting_for_input`; after `AnswerTimeoutMinutes` (or autonomous rules), invoke Hermes session `send` with grounded prompt; append `work_card_events` kind `hermes_answer`; clear waiting

- [ ] Tests for timeout math + denylist short-circuit (never auto-answer denylisted tool intents — classify via simple keyword/allow list in v1)
- [ ] Commit: `feat(workboard): hermes answer-on-behalf after timeout`

### Task 12: Autonomous IPC/UI

**Files:**
- PATCH project config `workboard.autonomous`
- UI: single Autonomous button opening settings panel (mode, short minutes, sticky)

- [ ] Commit: `feat(ui): autonomous mode controls for workboard`

### Task 13: Limit detect + fallback switch

**Files:**
- `service/workboard/switch_agent.go`
- Parse known rate-limit substrings per harness (start with codex/claude); Hermes may POST internal hint later
- Pick next from `fallbackAgents` skipping cooldown map in memory or sqlite (`agent_limit_cooldowns` optional — v1 in-memory per daemon life is OK if documented; prefer sqlite row for durability: simple `workboard_agent_cooldowns(project_id, agent, until_ms)`)
- New session same worktree: use existing restore/spawn paths; update card.agent + session_id; event `agent_switched`

- [ ] Commit: `feat(workboard): auto-switch coding agent on rate limit`

---

# Phase 4 — Retarget, scheduled, intake, home

### Task 14: Nudge / Retarget / Split APIs + UI

**Files:**
- `POST /workboard/cards/{id}/nudge` → session send + event
- `POST .../retarget` → set `paused_retarget`, update title/notes, bump `goal_version`, hermes handoff send or respawn, clear pause
- `POST .../split` → create card, set `superseded_by` / fate status on old, optional immediate ready
- UI menu on running card per locked Retarget design

- [ ] Commit: `feat(workboard): nudge, retarget, and split for running cards`

### Task 15: Scheduled promotion

- Dispatcher promotes `scheduled`→`ready` when due (already partially in Task 8); ensure create/edit UI for `scheduled_at`

- [ ] Commit: `feat(workboard): scheduled card auto-promotion`

### Task 16: Tracker intake → triage cards

**Files:**
- Modify `observe/trackerintake` to call `workboard.Create` with status `triage` instead of/in addition to direct session spawn — **replace** direct spawn when `config.workboard` present or feature flag `workboardIntake: true` default true once phase 4 ships

- [ ] Commit: `feat(intake): create triage work cards from matching issues`

### Task 17: Home route + Add to Workboard

- Default navigate to last project workboard
- Action on legacy session: create card linked to session_id in `running`

- [ ] Commit: `feat(ui): workboard home and add-session-to-card`

### Task 18: Final gate

```bash
cd backend && go test ./... -count=1
cd frontend && npm run typecheck
npm run api  # clean drift
```

Manual checklist from design: create→ready→claim→TUI; timeout answer; limit switch; retarget; intake triage; done is manual.

---

## Spec coverage self-review

| Spec item | Tasks |
|-----------|-------|
| OpenClaw columns + priority/labels | 1, 2, 6 |
| Hybrid pull + WIP 3 | 8 |
| 1 card : 1 agent, no default | 3, 6 |
| Folder under registered repo | 3 |
| Hermes orchestrator seat | 9 |
| TUI preview + focus live | 10 |
| Limit switch + cooldown 1h | 13 |
| Answer 10m + Autonomous UI | 11–12 |
| Denylist + audit events | 11, 2 (events table) |
| Retarget 3 levels | 14 |
| scheduled auto | 8, 15 |
| intake → triage | 16 |
| done user-only | 6 (no auto done in dispatcher) |
| single board v1 | 2 (`board_id=default`) |
| legacy sessions opt-in | 17 |

## Placeholder scan

No TBD steps; phase 2–4 compress UI chrome but name files, routes, and commit messages explicitly.

## Type consistency

- Status strings match OpenClaw exactly.
- Config JSON keys: `wipLimit`, `fallbackAgents`, `limitCooldownMinutes`, `answerTimeoutMinutes`, `autonomous`, `answerDenylist`.
- CDC event: `work_card_changed`.
