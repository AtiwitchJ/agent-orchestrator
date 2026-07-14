# Hybrid Approval Gates — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **Design companion:** `../specs/2026-07-14-hybrid-approval-gates-design.md`

**Goal:** Drive a closed loop from "issue labeled `agent-ready`" through four sequential gates (CI → Agent review → Human approve → Agent final-pass) with a hybrid veto path that requires **both** a second-opinion agent **and** an explicit human confirmation to override a Gate-4 failure. Every transition is a CDC event; every override is auditable; everything is opt-in per project.

**Architecture:** A new backend package `internal/policy/` holds the gate engine and state machine. It subscribes to events from the existing `internal/observe/scm` observer and the new tracker observer (Task 1) and writes results to two new SQLite tables (`policy_runs`, `gate_results`) that feed triggers into the existing `change_log`. Reviewer and second-opinion sessions are spawned through the existing `internal/adapters/agent/` and `internal/adapters/workspace/` registries — no new adapter ports. The CLI gains read-only `ao policy` and act-on-veto subcommands. The frontend gains a policy badge on the session view, a hybrid-veto dialog, and an experimental banner.

**Tech Stack:** Go 1.25+, sqlc (regen), goose (new migration), Cobra (`ao`), TanStack Router + Query + shadcn primitives, existing `openapi-fetch` typed client (regen via `npm run api`).

---

## Global Constraints

- **Loopback-only, no new ports.** The policy engine runs in-process; only new HTTP surface is read-only state endpoints (see Task 9).
- **Append-only SQLite migrations.** Add a new migration; never modify merged ones. (`AGENTS.md` "Hard rules and boundaries".)
- **No hand-edits to `backend/internal/storage/sqlite/gen/*`.** Edit `queries/` + `migrations/` + run `npm run sqlc`. (`AGENTS.md`.)
- **Status is derived, not stored.** Policy run state lives in `policy_runs`; display status comes from joining with `sessions` and PR facts at service read time.
- **No parallel manual CDC emission.** New tables add `change_log` triggers; store methods do not emit CDC events directly. (`AGENTS.md`.)
- **Use `context.Context` as the first argument** for any function doing I/O or blocking work. (`AGENTS.md`.)
- **Table-test Cobra commands** in the style of `backend/internal/cli/*_test.go`.
- **Return `usageError` for CLI misuse** (exit 2); runtime/daemon failures exit 1.
- **Preserve API error envelopes and request IDs** when surfacing daemon errors.
- **All app state under `~/.ao`.** No writes to `~/Library/Application Support` or any OS default app-data path.
- **Do not add network calls to tests** unless the package already has an integration/e2e pattern; prefer `httptest`, fakes, and injected dependencies.
- **Conventional commits** (`feat:`, `fix:`, `docs:`, `test:`, `chore:`). One logical change per PR.
- **Run narrowest relevant tests first**, then the broader `go test -race ./...` and `npm run lint` gates.
- **Frontend formatting:** tabs for indentation, `cn(...)` from `../lib/utils`, semantic design tokens (`text-foreground`, `text-passive`, `bg-surface`, `border-border`, `text-accent`, `text-destructive`) — never raw Tailwind colors like `slate-*`/`blue-*`.

---

### Task 1: Tracker observer loop (closes #112)

**Files:**
- Modify: `backend/internal/observe/tracker/tracker.go` (new file under `internal/observe/`)
- Modify: `backend/internal/observe/daemon.go` (wire tracker observer into the lifecycle)
- Modify: `backend/internal/cli/project.go` (add `ao tracker label <project>` convenience)
- Test: `backend/internal/observe/tracker/tracker_test.go`

**Interfaces:**
- Produces: `TrackerObserver` struct with `Run(ctx) error` (long-lived loop, like the SCM observer).
- Produces: `TrackerObserverConfig { PollInterval, ProjectLabel }`.
- Produces: CDC event `tracker_issue_labeled` when a configured label is seen.
- Produces: spawn call to existing `internal/adapters/agent/` + `internal/adapters/workspace/` to create a new session in a worktree, link issue ↔ session ↔ (future) PR via a new `tracker_links` table.

- [ ] **Step 1: Write the failing test for tracker label detection**

In `backend/internal/observe/tracker/tracker_test.go`:

```go
func TestTrackerObserver_DetectsConfiguredLabel(t *testing.T) {
    // fake SCM client returns 1 issue labeled "agent-ready"
    // tracker observer with ProjectLabel = "agent-ready"
    // assert: tracker_issue_labeled CDC event fired once
    // assert: session spawn call invoked once with correct worktree
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/observe/tracker/... -run TestTrackerObserver_DetectsConfiguredLabel`
Expected: FAIL — package does not exist yet.

- [ ] **Step 3: Create the package skeleton**

Create `backend/internal/observe/tracker/tracker.go` with `TrackerObserver`, `TrackerObserverConfig`, and `Run(ctx)` that no-ops with a TODO. Wire it into `backend/internal/observe/daemon.go` start/stop.

- [ ] **Step 4: Implement the label scan + spawn**

Use the existing fake SCM client pattern from `internal/observe/scm/`. On label match, call the existing session-spawn path (which already creates worktrees) and write a `tracker_links` row.

- [ ] **Step 5: Run the test, verify pass**

Run: `cd backend && go test -race ./internal/observe/tracker/...`

- [ ] **Step 6: Run broader checks**

Run: `cd backend && go test -race ./internal/observe/... && go vet ./...`

---

### Task 2: New SQLite migration for policy tables

**Files:**
- Create: `backend/internal/storage/sqlite/migrations/00XX_add_policy_tables.sql`
- Modify: `backend/internal/storage/sqlite/queries/policy.sql` (new file)
- Run: `npm run sqlc` from repo root to regenerate `gen/`

**Schema:**

```sql
-- policy_runs: one row per tracker-spawned PR lifecycle
CREATE TABLE policy_runs (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL,
    session_id      TEXT NOT NULL,
    pr_id           TEXT NOT NULL,
    config_snapshot TEXT NOT NULL,         -- JSON of PolicyConfig at run start
    current_gate    TEXT NOT NULL,         -- "ci" | "review" | "human" | "final" | "merge" | "done" | "stopped"
    final_state     TEXT,                  -- "merged" | "stopped" | "abandoned"
    started_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    UNIQUE(project_id, pr_id)
);

CREATE INDEX idx_policy_runs_session ON policy_runs(session_id);
CREATE INDEX idx_policy_runs_state ON policy_runs(current_gate, updated_at);

-- gate_results: one row per gate attempt
CREATE TABLE gate_results (
    id           TEXT PRIMARY KEY,
    run_id       TEXT NOT NULL REFERENCES policy_runs(id) ON DELETE CASCADE,
    gate_id      TEXT NOT NULL,            -- "ci" | "review" | "human" | "final"
    attempt      INTEGER NOT NULL,
    outcome      TEXT NOT NULL,            -- "pass" | "fail" | "exhausted" | "overridden"
    reason       TEXT,
    second_vote  TEXT,                     -- "approve" | "reject" | null
    justification TEXT,                    -- required when outcome = "overridden"
    duration_ms  INTEGER,
    created_at   INTEGER NOT NULL
);

CREATE INDEX idx_gate_results_run ON gate_results(run_id, gate_id, attempt);

-- tracker_links: issue ↔ session link (used by Task 1 + policy run)
CREATE TABLE tracker_links (
    issue_id    TEXT NOT NULL,
    project_id  TEXT NOT NULL,
    session_id  TEXT NOT NULL,
    pr_id       TEXT,
    created_at  INTEGER NOT NULL,
    PRIMARY KEY (project_id, issue_id)
);

-- change_log triggers (reuses existing trigger template)
CREATE TRIGGER policy_runs_change_trg AFTER INSERT OR UPDATE OR DELETE ON policy_runs
BEGIN
    INSERT INTO change_log (table_name, row_id, op, ts) VALUES ('policy_runs', NEW.id, TG_OP, unixepoch());
END;

CREATE TRIGGER gate_results_change_trg AFTER INSERT OR UPDATE OR DELETE ON gate_results
BEGIN
    INSERT INTO change_log (table_name, row_id, op, ts) VALUES ('gate_results', NEW.id, TG_OP, unixepoch());
END;
```

- [ ] **Step 1: Verify schema with a scratch test**

Run: `cd backend && go test ./internal/storage/sqlite/... -run TestPolicySchemaMigrates`
Expected: FAIL until migration lands.

- [ ] **Step 2: Write the migration file**

Number it `00XX_add_policy_tables.sql` following the existing sequence in `migrations/`.

- [ ] **Step 3: Add queries in `queries/policy.sql`**

```sql
-- name: CreatePolicyRun :one
INSERT INTO policy_runs (id, project_id, session_id, pr_id, config_snapshot, current_gate, started_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING *;

-- name: GetPolicyRun :one
SELECT * FROM policy_runs WHERE id = ? LIMIT 1;

-- name: ListPolicyRunsBySession :many
SELECT * FROM policy_runs WHERE session_id = ? ORDER BY started_at DESC;

-- name: UpdatePolicyRunGate :exec
UPDATE policy_runs SET current_gate = ?, updated_at = ? WHERE id = ?;

-- name: FinalizePolicyRun :exec
UPDATE policy_runs SET final_state = ?, current_gate = 'done', updated_at = ? WHERE id = ?;

-- name: RecordGateResult :one
INSERT INTO gate_results (id, run_id, gate_id, attempt, outcome, reason, second_vote, justification, duration_ms, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING *;

-- name: ListGateResults :many
SELECT * FROM gate_results WHERE run_id = ? ORDER BY created_at ASC;

-- name: CreateTrackerLink :one
INSERT INTO tracker_links (issue_id, project_id, session_id, pr_id, created_at)
VALUES (?, ?, ?, ?, ?) RETURNING *;

-- name: GetTrackerLinkByIssue :one
SELECT * FROM tracker_links WHERE project_id = ? AND issue_id = ? LIMIT 1;

-- name: SetTrackerLinkPR :exec
UPDATE tracker_links SET pr_id = ? WHERE project_id = ? AND issue_id = ?;
```

- [ ] **Step 4: Run `npm run sqlc` from the repo root**

Run: `cd /Users/up-mac/wokrspace/mind/agent-orchestrator && npm run sqlc`
Expected: `gen/` regenerates with new typed query methods. No manual edits.

- [ ] **Step 5: Re-run schema migration test, verify pass**

Run: `cd backend && go test -race ./internal/storage/sqlite/...`

---

### Task 3: Policy config accessor

**Files:**
- Modify: `backend/internal/service/project_config.go` (or create if missing)
- Test: `backend/internal/service/project_config_test.go`

**Interfaces:**
- Produces: `PolicyConfig` struct (defined in design doc §4).
- Produces: `func (s *Service) GetPolicyConfig(ctx context.Context, projectID string) (PolicyConfig, error)`.
- Produces: `func (s *Service) SetPolicyConfig(ctx context.Context, projectID string, cfg PolicyConfig) error`.

- [ ] **Step 1: Write failing test for defaults**

```go
func TestGetPolicyConfig_DefaultsWhenAbsent(t *testing.T) {
    cfg, err := svc.GetPolicyConfig(ctx, "proj-without-config")
    require.NoError(t, err)
    require.Equal(t, false, cfg.Enabled)              // opt-in
    require.Equal(t, "same_agent", cfg.ReviewStrategy)
    require.Equal(t, 3, cfg.MaxAutoFixRounds)
    require.Equal(t, 3, cfg.MaxReviseRounds)
    require.Equal(t, "squash", cfg.MergeStrategy)
}
```

- [ ] **Step 2: Implement with conservative defaults**

Define `defaultPolicyConfig()` returning the values from the design doc.

- [ ] **Step 3: Run test, verify pass**

Run: `cd backend && go test -race ./internal/service/... -run TestGetPolicyConfig`

---

### Task 4: Gate engine — state machine core

**Files:**
- Create: `backend/internal/policy/engine.go`
- Create: `backend/internal/policy/gates.go`
- Create: `backend/internal/policy/state.go`
- Test: `backend/internal/policy/engine_test.go`

**Interfaces:**
- Produces: `Engine` struct with `Run(ctx, runID) error`, `Decide(ctx, runID, decision) error`.
- Produces: `Gate` interface per gate: `ID() GateID`, `Run(ctx, runCtx) (GateOutcome, error)`.
- Produces: `GateID` enum: `GateCI`, `GateReview`, `GateHuman`, `GateFinal`.

- [ ] **Step 1: Write the state machine test**

```go
func TestEngine_HappyPathPassesAllFourGates(t *testing.T) {
    // fake gates all return pass
    // engine.Run(ctx, runID)
    // assert: policy_runs.final_state == "merged" (after merge policy)
    // assert: 4 gate_results rows with outcome=pass
    // assert: CDC events fired in correct order
}
```

- [ ] **Step 2: Implement Engine with sequential gate execution**

Use the state machine from design doc §3. Engine reads `policy_runs.current_gate`, executes that gate, records the result, and transitions.

- [ ] **Step 3: Run test, verify pass**

Run: `cd backend && go test -race ./internal/policy/...`

---

### Task 5: Gate 1 — CI auto-fix

**Files:**
- Create: `backend/internal/policy/gates/ci.go`
- Test: `backend/internal/policy/gates/ci_test.go`

**Interfaces:**
- Produces: `CIGate` implementing `Gate`.
- Subscribes to: existing `internal/observe/scm` PR check events.

- [ ] **Step 1: Write tests for pass / fail / exhausted**

```go
func TestCIGate_Pass(t *testing.T)              { /* checks.status == success */ }
func TestCIGate_AutoFixThenPass(t *testing.T)   { /* 2 fails, 1 pass → outcome=pass, 3 attempts */ }
func TestCIGate_Exhausted(t *testing.T)         { /* 3 fails → outcome=exhausted */ }
```

- [ ] **Step 2: Implement — auto-fix triggers `ao hooks` activity dispatch using existing adapter registry**

- [ ] **Step 3: Run tests, verify pass**

Run: `cd backend && go test -race ./internal/policy/gates/...`

---

### Task 6: Gate 2 — Agent self-review with revision loop

**Files:**
- Create: `backend/internal/policy/gates/review.go`
- Create: `backend/internal/policy/prompts/review.md` (reviewer prompt template)
- Test: `backend/internal/policy/gates/review_test.go`

**Interfaces:**
- Produces: `ReviewGate` with `Strategy` field (`same_agent`, `cross_agent`, `second_only`).
- Produces: spawn call to `internal/adapters/agent/` registry.

- [ ] **Step 1: Write tests covering all three strategies**

```go
func TestReviewGate_SameAgent(t *testing.T)   { /* reuses primary adapter */ }
func TestReviewGate_CrossAgent(t *testing.T)  { /* spawns ReviewAgent adapter */ }
func TestReviewGate_SecondOnly(t *testing.T)  { /* skips unless Gate 4 fires */ }
func TestReviewGate_ReviseExhausted(t *testing.T) { /* 3 fails → exhausted */ }
```

- [ ] **Step 2: Implement strategy dispatch**

`same_agent` reuses the primary adapter with `role='reviewer'` and the prompt from `prompts/review.md`. `cross_agent` looks up `ReviewAgent`. `second_only` returns `pass` immediately.

- [ ] **Step 3: Run tests, verify pass**

Run: `cd backend && go test -race ./internal/policy/gates/...`

---

### Task 7: Gate 3 — Human approve + notification

**Files:**
- Modify: `backend/internal/service/notification.go` (existing — add `policy_veto_requested` and `needs_input` mapping)
- Create: `backend/internal/policy/gates/human.go`
- Test: `backend/internal/policy/gates/human_test.go`

**Interfaces:**
- Produces: `HumanGate` that emits a notification and waits for `human_override_decided` event with `decision` ∈ {`approve`, `request_changes`, `comment`}.

- [ ] **Step 1: Write tests for approve / request_changes / timeout**

```go
func TestHumanGate_Approve(t *testing.T)         { /* decision=approve → outcome=pass */ }
func TestHumanGate_RequestChanges(t *testing.T)  { /* decision=request_changes → outcome=fail, restart at Gate 2 */ }
func TestHumanGate_Timeout(t *testing.T)          { /* HumanTimeoutHours exceeded → escalate */ }
```

- [ ] **Step 2: Implement — uses existing notification plumbing; new event type `human_override_decided` already covered by Task 12**

- [ ] **Step 3: Run tests, verify pass**

Run: `cd backend && go test -race ./internal/policy/gates/...`

---

### Task 8: Gate 4 — Agent final-pass with hybrid veto

**Files:**
- Create: `backend/internal/policy/gates/final.go`
- Create: `backend/internal/policy/gates/hybrid_veto.go`
- Test: `backend/internal/policy/gates/final_test.go`
- Test: `backend/internal/policy/gates/hybrid_veto_test.go`

**Interfaces:**
- Produces: `FinalGate` running: rebase → lint/policy scan → secret scan → commit-message check.
- Produces: `HybridVeto` flow per design doc §3.1.

- [ ] **Step 1: Write tests for final-pass happy path**

```go
func TestFinalGate_Pass(t *testing.T) { /* all 4 checks pass → outcome=pass */ }
func TestFinalGate_RebaseConflict(t *testing.T) { /* rebase fails → triggers veto */ }
```

- [ ] **Step 2: Write tests for hybrid veto — both approve, second rejects, human overrides**

```go
func TestHybridVeto_BothApprove(t *testing.T)         { /* second=approve, human=confirm → proceed */ }
func TestHybridVeto_SecondRejects(t *testing.T)       { /* second=reject → stop unless human override */ }
func TestHybridVeto_HumanOverridesWithJustification(t *testing.T) {
    /* second=reject, human override with reason → audit event, proceed */
}
func TestHybridVeto_MissingJustification(t *testing.T) { /* override without text → rejected */ }
```

- [ ] **Step 3: Implement rebase/lint/secret checks using existing adapters**

Rebase uses `internal/adapters/workspace/`. Lint/secret scan call out to the existing `ao hooks` activity dispatch.

- [ ] **Step 4: Implement hybrid veto path**

Spawn second-opinion agent (`VetoSecondAgent`, fallback to `ReviewAgent`). Emit `human_override_requested`. Wait for `human_override_decided` with `justification` non-empty when decision=override.

- [ ] **Step 5: Run tests, verify pass**

Run: `cd backend && go test -race ./internal/policy/gates/...`

---

### Task 9: HTTP surface for policy state (read-only)

**Files:**
- Modify: `backend/internal/httpd/controllers/policy.go` (new)
- Modify: `backend/internal/httpd/controllers/dto.go` (add request/response shapes)
- Modify: `backend/internal/httpd/apispec/specgen/build.go` (register the operation)
- Test: `backend/internal/httpd/controllers/policy_test.go`

**Endpoints:**
- `GET /api/v1/projects/{id}/policy` — return merged `PolicyConfig` (defaults + overrides)
- `PUT /api/v1/projects/{id}/policy` — update config
- `GET /api/v1/policy/runs/{runId}` — run state + last gate outcome
- `GET /api/v1/policy/runs/{runId}/gates` — full gate history
- `POST /api/v1/policy/runs/{runId}/decide` — human decision endpoint (used by CLI + desktop)

- [ ] **Step 1: Write failing tests for each endpoint**

- [ ] **Step 2: Add DTOs in `dto.go`**

- [ ] **Step 3: Implement controllers**

- [ ] **Step 4: Register in `specgen/build.go`**

- [ ] **Step 5: Run `npm run api` to regenerate OpenAPI spec + frontend TS types**

Run: `cd /Users/up-mac/wokrspace/mind/agent-orchestrator && npm run api`

- [ ] **Step 6: Run HTTP tests + spec-drift tests**

Run: `cd backend && go test -race ./internal/httpd/...`

---

### Task 10: CLI parity — `ao policy` subcommands

**Files:**
- Create: `backend/internal/cli/policy.go`
- Test: `backend/internal/cli/policy_test.go`

**Subcommands:**
- `ao policy show <project>` — print merged config
- `ao policy set <project> --enabled --review-agent=codex --max-revise=3 ...`
- `ao policy runs <session>` — list runs for a session
- `ao policy decide <runId> --approve | --request-changes "msg" | --override-with "justification"`

- [ ] **Step 1: Write table tests mirroring existing `*_test.go` style**

- [ ] **Step 2: Implement — reuse existing `usageError` helper and CLI client helpers**

- [ ] **Step 3: Run CLI tests**

Run: `cd backend && go test -race ./internal/cli/...`

---

### Task 11: CLI parity — `ao pr` and `ao review`

**Files:**
- Create: `backend/internal/cli/pr.go`
- Create: `backend/internal/cli/review.go`
- Test: `backend/internal/cli/pr_test.go`, `backend/internal/cli/review_test.go`

**Subcommands:**
- `ao pr list <project>` — list PRs + concise state
- `ao pr merge <prId>` — invoke existing merge endpoint
- `ao pr resolve-comments <prId>` — invoke existing resolve-comments endpoint
- `ao review list <project>` — list reviews
- `ao review execute <project>` — invoke existing review-execute endpoint
- `ao review send <reviewId>` — invoke existing review-send endpoint

- [ ] **Step 1: Write table tests for each subcommand**

- [ ] **Step 2: Implement using shared CLI HTTP client helpers**

- [ ] **Step 3: Run CLI tests**

Run: `cd backend && go test -race ./internal/cli/...`

---

### Task 12: CDC event wiring (12 event types from design §5)

**Files:**
- Modify: `backend/internal/policy/events.go`
- Modify: `backend/internal/observe/cdc/broadcaster.go` (verify subscription path)

**Note:** Events flow through `change_log` triggers added in Task 2; the broadcaster picks them up automatically. This task is mainly to:
- Ensure each of the 12 event types has a corresponding Go type for typed consumption
- Add typed helpers on the policy engine for emitting (`EmitGatePassed`, etc.)
- Verify `GET /api/v1/events` actually streams them (write a smoke test)

- [ ] **Step 1: Define Go types in `events.go`**

- [ ] **Step 2: Write smoke test that subscribes via SSE and verifies all 12 event types flow during a happy-path run**

- [ ] **Step 3: Run CDC broadcaster tests**

Run: `cd backend && go test -race ./internal/observe/cdc/...`

---

### Task 13: Frontend — policy badge on session view

**Files:**
- Create: `frontend/src/renderer/components/PolicyBadge.tsx`
- Modify: `frontend/src/renderer/components/SessionView.tsx`
- Test: `frontend/src/renderer/components/PolicyBadge.test.tsx`

**Behavior:**
- Badge reads from `useWorkspaceQuery()` cache for the session, fetches `GET /api/v1/policy/runs?sessionId=...`
- Shows current gate + last outcome
- `policy_v1_experimental` shows a small dismissible banner on the badge

- [ ] **Step 1: Write failing test — badge renders gate name + outcome**

- [ ] **Step 2: Implement component using shadcn `Badge` primitive + existing semantic tokens**

- [ ] **Step 3: Wire into `SessionView.tsx` next to existing status indicator**

- [ ] **Step 4: Run frontend tests + typecheck**

Run: `cd frontend && npm run typecheck && npx vitest run src/renderer/components/PolicyBadge.test.tsx`

---

### Task 14: Frontend — hybrid-veto dialog

**Files:**
- Create: `frontend/src/renderer/components/HybridVetoDialog.tsx`
- Modify: `frontend/src/renderer/components/PolicyBadge.tsx` (open dialog on click when veto is active)
- Test: `frontend/src/renderer/components/HybridVetoDialog.test.tsx`

**Behavior:**
- Receives `runId`, second-opinion vote, reason
- Three actions: Override (requires justification text), Send back for revision, Defer to second-opinion (visible only when second=approve)
- Calls `POST /api/v1/policy/runs/{runId}/decide`

- [ ] **Step 1: Write failing test — override button disabled until justification text is non-empty**

- [ ] **Step 2: Implement using existing `Dialog` primitive**

- [ ] **Step 3: Wire into `PolicyBadge` click handler**

- [ ] **Step 4: Run frontend tests + typecheck**

Run: `cd frontend && npm run typecheck && npx vitest run src/renderer/components/HybridVetoDialog.test.tsx`

---

### Task 15: Frontend — experimental banner

**Files:**
- Create: `frontend/src/renderer/components/PolicyExperimentalBanner.tsx`
- Modify: `frontend/src/renderer/components/SessionView.tsx`
- Test: `frontend/src/renderer/components/PolicyExperimentalBanner.test.tsx`

**Behavior:**
- Shows when `GET /api/v1/policy/runs/{runId}` returns `experimental: true`
- Dismissible per-run (stored in component state, not persisted — re-shows on next visit per design §6)
- Plain prose: "This run uses the policy_v1_experimental path. Gates may behave slightly differently than the documented defaults."

- [ ] **Step 1: Write failing test**

- [ ] **Step 2: Implement using existing `Banner` or shadcn `Alert` primitive**

- [ ] **Step 3: Wire into `SessionView.tsx` above the policy badge**

- [ ] **Step 4: Run frontend tests + typecheck**

---

### Task 16: End-to-end smoke test

**Files:**
- Create: `backend/internal/policy/integration_test.go` (build tag `//go:build integration`)
- Modify: `test/cli/run-policy-smoke.sh`

- [ ] **Step 1: Write integration test that runs the full happy path through all four gates against a fake SCM + fake agent**

Run: `cd backend && go test -race -tags=integration ./internal/policy/...`

- [ ] **Step 2: Write CLI smoke script that boots a project, enables policy, opens a PR with `gh`, watches the events stream, asserts each gate fires**

Run: `bash test/cli/run-policy-smoke.sh`

- [ ] **Step 3: Document in `test/cli/README.md` how to re-run the smoke**

---

### Task 17: Final verification gates

- [ ] **Step 1: Run the local gate**

Run: `cd backend && go build ./... && go test -race ./...`

- [ ] **Step 2: Run lint**

Run: `cd /Users/up-mac/wokrspace/mind/agent-orchestrator && npm run lint`

- [ ] **Step 3: Run frontend typecheck + build**

Run: `cd frontend && npm run typecheck && npm run build`

- [ ] **Step 4: Run sqlc + api regen (no diff = clean)**

Run: `npm run sqlc && npm run api && git diff --stat backend/internal/storage/sqlite/gen/ frontend/src/api/schema.ts`
Expected: empty diff.

- [ ] **Step 5: Run full CI locally if Docker is available**

Run: `npx @redwoodjs/agent-ci run --all`

---

### Task 18: Worker pool registry + specialty tags

**Files:**
- Create: `backend/internal/policy/pool.go`
- Create: `backend/internal/policy/pool_test.go`
- Modify: `backend/internal/service/project_config.go` (extend `PolicyConfig` with pool fields per design §12.2-12.4)
- Test: `backend/internal/policy/pool_test.go`

**Interfaces:**
- Produces: `PoolConfig` struct:
  ```go
  type PoolConfig struct {
      Enabled           bool              `json:"enabled"`
      SizePerSpecialty  map[string]int    `json:"size_per_specialty"`  // e.g. {"BE": 2, "FE": 2, "DB": 1, ...}
      MeshTopics        []string          `json:"mesh_topics"`         // topics workers can subscribe to
  }
  ```
- Produces: `Specialty` type with 8 built-in tags (`BE`, `FE`, `DB`, `Test`, `Sec`, `Docs`, `Perf`, `Refactor`) + escape-hatch string for PM override.
- Produces: `WorkerTier` enum: `TierTrusted`, `TierExperimental`, `TierBanned`.
- Produces: `Pool` struct with `Dispatch(ctx, job JobSpec) (WorkerHandle, error)`.
- Produces: `WorkerHandle` interface: `Done(ctx) (JobResult, error)`, `Release()` (return to pool).

- [ ] **Step 1: Write failing tests for built-in specialty validation + tier enforcement**

```go
func TestPool_RejectsUnknownSpecialty(t *testing.T) { /* "Quantum" alone fails */ }
func TestPool_RejectsBannedTier(t *testing.T)        { /* tier=banned → dispatch_blocked */ }
func TestPool_RequiresPMApprovalForExperimental(t *testing.T) { /* wait for PM decide */ }
func TestPool_DispatchesTrustedAuto(t *testing.T)    { /* tier=trusted → no wait */ }
```

- [ ] **Step 2: Implement `PoolConfig` + `Specialty` + `WorkerTier` types**

- [ ] **Step 3: Implement `Pool` struct with size cap + tier-aware dispatch**

`Dispatch` looks up pool size for specialty, picks best-fit worker (highest success rate, lowest cost, available), spawns new if under cap, queues if at cap.

- [ ] **Step 4: Run tests, verify pass**

Run: `cd backend && go test -race ./internal/policy/...`

---

### Task 19: Worker pool telemetry (success rate, cost, time)

**Files:**
- Create: `backend/internal/storage/sqlite/migrations/00XX_add_worker_pool_tables.sql`
- Create: `backend/internal/storage/sqlite/queries/pool.sql`
- Run: `npm run sqlc` from repo root
- Test: `backend/internal/policy/pool_telemetry_test.go`

**Schema:**

```sql
-- worker_pool: per-project pool config snapshot
CREATE TABLE worker_pool (
    project_id    TEXT PRIMARY KEY,
    config        TEXT NOT NULL,         -- JSON of PoolConfig
    updated_at    INTEGER NOT NULL
);

-- worker_runs: one row per worker spawn/job cycle
CREATE TABLE worker_runs (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL,
    worker_id       TEXT NOT NULL,        -- e.g. "worker[BE-API]#3"
    specialty       TEXT NOT NULL,
    tier            TEXT NOT NULL,        -- trusted|experimental|banned
    pm_override_tag TEXT,                 -- PM's custom tag, if any
    job_id          TEXT NOT NULL,
    started_at      INTEGER NOT NULL,
    finished_at     INTEGER,
    outcome         TEXT,                 -- "pass" | "fail" | "abandoned"
    cost_cents      INTEGER,
    duration_ms     INTEGER
);

CREATE INDEX idx_worker_runs_project ON worker_runs(project_id, specialty);
CREATE INDEX idx_worker_runs_worker ON worker_runs(worker_id, started_at DESC);

-- change_log triggers
CREATE TRIGGER worker_runs_change_trg AFTER INSERT OR UPDATE OR DELETE ON worker_runs
BEGIN
    INSERT INTO change_log (table_name, row_id, op, ts) VALUES ('worker_runs', NEW.id, TG_OP, unixepoch());
END;
```

**Queries (in `queries/pool.sql`):**
```sql
-- name: GetPoolConfig :one
SELECT * FROM worker_pool WHERE project_id = ? LIMIT 1;

-- name: UpsertPoolConfig :one
INSERT INTO worker_pool (project_id, config, updated_at) VALUES (?, ?, ?)
ON CONFLICT(project_id) DO UPDATE SET config = excluded.config, updated_at = excluded.updated_at
RETURNING *;

-- name: RecordWorkerRun :one
INSERT INTO worker_runs (id, project_id, worker_id, specialty, tier, pm_override_tag, job_id, started_at, finished_at, outcome, cost_cents, duration_ms)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING *;

-- name: WorkerSuccessRate :one
SELECT
    COUNT(*) FILTER (WHERE outcome = 'pass') AS passes,
    COUNT(*) AS total,
    COALESCE(AVG(cost_cents) FILTER (WHERE outcome = 'pass'), 0) AS avg_cost_cents,
    COALESCE(AVG(duration_ms) FILTER (WHERE outcome = 'pass'), 0) AS avg_duration_ms
FROM worker_runs
WHERE worker_id = ? AND started_at > ?;
```

- [ ] **Step 1: Write failing test for success-rate aggregation**

- [ ] **Step 2: Write the migration + queries**

- [ ] **Step 3: Run `npm run sqlc`**

Run: `cd /Users/up-mac/wokrspace/mind/agent-orchestrator && npm run sqlc`

- [ ] **Step 4: Implement `RecordWorkerRun` + `WorkerSuccessRate` helpers**

- [ ] **Step 5: Run tests, verify pass**

Run: `cd backend && go test -race ./internal/policy/... ./internal/storage/sqlite/...`

---

### Task 20: PM dispatch + mesh sync via existing CDC events

**Files:**
- Create: `backend/internal/policy/dispatcher.go`
- Modify: `backend/internal/observe/cdc/broadcaster.go` (verify mesh filter path)
- Test: `backend/internal/policy/dispatcher_test.go`

**Interfaces:**
- Produces: `Dispatcher` struct: `DispatchJob(ctx, job JobSpec) (JobHandle, error)`.
- Produces: `JobSpec` struct: `{ Specialty, Tier, PmOverrideTag, MeshTopics[] }`.
- Produces: CDC event `pm_job_dispatched` (project_id, job_id, worker_id, specialty).
- Produces: CDC event `mesh_topic_published` (project_id, topic, payload_ref) — workers subscribe via `?project_id=X&topics=...` filter on `GET /api/v1/events`.

- [ ] **Step 1: Write failing test for PM dispatch happy path**

```go
func TestDispatcher_DispatchTrustedJob(t *testing.T) {
    // tier=trusted, pool has free worker
    // assert: worker spawned, pm_job_dispatched event fires
    // assert: worker_runs row inserted
}
func TestDispatcher_MeshTopicPublished(t *testing.T) {
    // job publishes to "api_contract"
    // assert: mesh_topic_published event fires
    // assert: subscribed worker (in same project) sees event
}
```

- [ ] **Step 2: Implement `Dispatcher` calling `Pool.Dispatch` + writing telemetry**

- [ ] **Step 3: Implement mesh topic publication as CDC events filtered by project_id**

Reuses the existing `change_log` + broadcaster path; no new transport.

- [ ] **Step 4: Run tests, verify pass**

Run: `cd backend && go test -race ./internal/policy/...`

---

### Task 21: CLI surface for pool (read-only in Phase 1)

**Files:**
- Create: `backend/internal/cli/pool.go`
- Test: `backend/internal/cli/pool_test.go`

**Subcommands:**
- `ao pool show <project>` — print pool config + current worker counts
- `ao pool runs <project>` — list recent worker_runs (last 50)
- `ao pool stats <project>` — aggregated success rate / cost / time per specialty

- [ ] **Step 1: Write table tests mirroring `*_test.go` style**

- [ ] **Step 2: Implement using existing CLI HTTP client helpers**

- [ ] **Step 3: Run CLI tests**

Run: `cd backend && go test -race ./internal/cli/...`

---

### Task 22: Final verification gates (extended)

- [ ] **Step 1: Run the local gate**

Run: `cd backend && go build ./... && go test -race ./...`

- [ ] **Step 2: Run lint**

Run: `cd /Users/up-mac/wokrspace/mind/agent-orchestrator && npm run lint`

- [ ] **Step 3: Run frontend typecheck + build**

Run: `cd frontend && npm run typecheck && npm run build`

- [ ] **Step 4: Run sqlc + api regen (no diff = clean)**

Run: `npm run sqlc && npm run api && git diff --stat backend/internal/storage/sqlite/gen/ frontend/src/api/schema.ts`
Expected: empty diff.

- [ ] **Step 5: Run full CI locally if Docker is available**

Run: `npx @redwoodjs/agent-ci run --all`

- [ ] **Step 6: Run pool-specific smoke**

Run: `cd backend && go test -race -tags=integration ./internal/policy/...`

---

### Task 23: Design system foundation (tokens, primitives)

**Files:**
- Create: `frontend/src/renderer/design/tokens.ts` (color, typography, spacing)
- Create: `frontend/src/renderer/design/WorkerCard.tsx`
- Create: `frontend/src/renderer/design/StatusDot.tsx` (🟢🟡🔴⚪)
- Create: `frontend/src/renderer/design/TierBadge.tsx` (trusted/experimental/banned)
- Create: `frontend/src/renderer/design/GateStatusBar.tsx` (CI/Review/Human/Final)
- Test: `frontend/src/renderer/design/*.test.tsx`

**Tokens (matches design system in design doc):**
```ts
export const status = {
  success: '#10B981',
  warning: '#F59E0B',
  failed:  '#EF4444',
  idle:    '#6B7280',
} as const;

export const tier = {
  trusted:      { color: '#10B981', label: 'trusted' },
  experimental: { color: '#F59E0B', label: '⚠ experimental' },
  banned:       { color: '#EF4444', label: '⛔ banned' },
} as const;

export const gate = {
  ci:     { color: 'blue',   label: 'CI' },
  review: { color: 'purple', label: 'Review' },
  human:  { color: 'amber',  label: 'Human' },
  final:  { color: 'green',  label: 'Final' },
} as const;
```

- [ ] **Step 1: Write tests for each primitive component**

- [ ] **Step 2: Implement tokens + primitives using existing shadcn `ui/*` base + semantic color classes**

- [ ] **Step 3: Run frontend tests + typecheck**

Run: `cd frontend && npm run typecheck && npx vitest run src/renderer/design/`

---

### Task 24: Sidebar navigation + live badges

**Files:**
- Modify: `frontend/src/renderer/components/Sidebar.tsx`
- Create: `frontend/src/renderer/components/SidebarBadges.tsx`
- Test: `frontend/src/renderer/components/Sidebar.test.tsx`

**Behavior:**
- 5 main items: Org Overview, Project HQ, Worker Pool, Approvals, Telemetry
- Live badges via `useWorkspaceQuery()` + CDC subscription:
  - Project HQ: `[3 active]` (active jobs count)
  - Worker Pool: `[8/13 workers]` (active/total)
  - Approvals: `[3 pending]` with 🔴 red dot when > 0
- Telemetry + Settings: no badge
- Mirrors the existing nav-entry pattern (Settings dropdown) — extend the existing `useSelection()` hook.

- [ ] **Step 1: Write failing test — badge updates on CDC event**

```tsx
it("updates active count when worker_runs CDC event arrives", async () => {
  // render sidebar
  // simulate CDC event
  // expect badge text to change
});
```

- [ ] **Step 2: Add new nav items + route files (`_shell.org.tsx`, `_shell.pool.tsx`, `_shell.approvals.tsx`, `_shell.telemetry.tsx`)**

- [ ] **Step 3: Implement `SidebarBadges` subscribing to `GET /api/v1/events` filtered by `?sidebar=true`**

- [ ] **Step 4: Run frontend tests + typecheck**

---

### Task 25: Org Overview page (CEO view)

**Files:**
- Create: `frontend/src/renderer/routes/_shell.org.tsx`
- Create: `frontend/src/renderer/components/OrgOverviewPage.tsx`
- Create: `frontend/src/renderer/components/OrgHealthCard.tsx`
- Create: `frontend/src/renderer/components/TrustTiersTable.tsx`
- Create: `frontend/src/renderer/components/CapacityHeatmap.tsx`
- Test: `frontend/src/renderer/components/OrgOverviewPage.test.tsx`

**Behavior (per design doc §1️⃣):**
- **Org Health Card**: active workers, cost today, per-company breakdown
- **Trust Tiers Table**: per-specialty tier (trusted/experimental/banned), success rate, promote action
- **Capacity Heatmap**: last 24h worker activity
- **Active Issues List**: top 5 issues, drill into Project HQ

**Interfaces:**
- New HTTP endpoint: `GET /api/v1/org/overview` (already exists per `2026-07-09-live-terminals-design.md` §1 — just consume the response shape)
- Trust tier promote: `PUT /api/v1/org/tiers/{specialty}` with `{ tier: "trusted" }`

- [ ] **Step 1: Write failing tests for each component**

- [ ] **Step 2: Implement components using `useWorkspaceQuery()` for initial load + CDC subscription for live updates**

- [ ] **Step 3: Wire route + nav entry**

- [ ] **Step 4: Run frontend tests + typecheck + build**

Run: `cd frontend && npm run typecheck && npm run build`

---

### Task 26: Project HQ page (PM view)

**Files:**
- Create: `frontend/src/renderer/components/ProjectHQPage.tsx`
- Create: `frontend/src/renderer/components/PoolStatusCard.tsx`
- Create: `frontend/src/renderer/components/ActiveJobsList.tsx`
- Create: `frontend/src/renderer/components/MeshActivityFeed.tsx`
- Test: `frontend/src/renderer/components/ProjectHQPage.test.tsx`

**Behavior (per design doc §2️⃣):**
- **Pool Status Card**: workers + current jobs (per specialty)
- **Active Jobs List**: jobs with per-job progress, gate status, action buttons (View Mesh, Force Sync, Cancel Job)
- **Mesh Activity Feed**: live feed of CDC `mesh_topic_published` events, subscribe to topics
- **Recent PRs**: with gate status (CI/Review/Final)

**Interfaces:**
- `GET /api/v1/projects/{id}/pool` — pool config + current state
- `GET /api/v1/projects/{id}/jobs` — active jobs
- `POST /api/v1/projects/{id}/jobs/{jobId}/cancel` — cancel a running job

- [ ] **Step 1: Write failing tests**

- [ ] **Step 2: Implement components**

- [ ] **Step 3: Wire route + nav entry**

- [ ] **Step 4: Run frontend tests + typecheck**

---

### Task 27: Worker Pool page (Execution view)

**Files:**
- Create: `frontend/src/renderer/components/WorkerPoolPage.tsx`
- Create: `frontend/src/renderer/components/WorkerCard.tsx` (per design doc §3️⃣)
- Create: `frontend/src/renderer/components/PromoteWorkerDialog.tsx`
- Test: `frontend/src/renderer/components/WorkerPoolPage.test.tsx`

**Behavior (per design doc §3️⃣):**
- Filter bar: specialty, tier, status
- Worker cards (1 per worker): status, success rate, cost, time, tier, specialty, last run, action buttons
- **Promote Worker Dialog**: shows current stats vs threshold, impact description, audit note
- Group cards by specialty (BE Workers, FE Workers, etc.)

**Interfaces:**
- `GET /api/v1/projects/{id}/pool/workers` — list all workers
- `POST /api/v1/projects/{id}/pool/workers/{workerId}/promote` — human-initiated tier promotion

- [ ] **Step 1: Write failing tests including threshold check**

- [ ] **Step 2: Implement components using `WorkerCard` primitive from Task 23**

- [ ] **Step 3: Wire route + nav entry**

- [ ] **Step 4: Run frontend tests + typecheck**

---

### Task 28: Approvals Inbox page (Gate 3 — Human decision)

**Files:**
- Create: `frontend/src/renderer/components/ApprovalsInboxPage.tsx`
- Create: `frontend/src/renderer/components/IssueApprovalCard.tsx`
- Create: `frontend/src/renderer/components/RecentDecisionsList.tsx`
- Test: `frontend/src/renderer/components/ApprovalsInboxPage.test.tsx`

**Behavior (per design doc §4️⃣):**
- **Issue-level approval** (per design §11.4 — PM aggregates PRs into issue)
- Per-issue card: PR list with per-PR gate status, cost, time, worker count
- Action buttons: Approve & Merge, Request Changes, View Diff
- Recent decisions list (last 24h, audit trail)
- Filter: All / Awaiting / Recent

**Interfaces:**
- `GET /api/v1/approvals/inbox` — list issues awaiting human approval
- `POST /api/v1/policy/runs/{runId}/decide` (already defined in Task 9) — `approve` | `request_changes` | `comment`

- [ ] **Step 1: Write failing tests for issue aggregation + decision flow**

- [ ] **Step 2: Implement components**

- [ ] **Step 3: Wire route + nav entry (already exists as `/_shell/approvals`)**

- [ ] **Step 4: Run frontend tests + typecheck**

---

### Task 29: Telemetry Dashboard (Analytics)

**Files:**
- Create: `frontend/src/renderer/components/TelemetryDashboardPage.tsx`
- Create: `frontend/src/renderer/components/GateSuccessRateChart.tsx`
- Create: `frontend/src/renderer/components/CostOverTimeChart.tsx`
- Create: `frontend/src/renderer/components/WorkerPerformanceTable.tsx`
- Create: `frontend/src/renderer/components/HumanOverrideDigest.tsx`
- Test: `frontend/src/renderer/components/TelemetryDashboardPage.test.tsx`

**Behavior (per design doc §5️⃣):**
- **Gate Success Rate Chart**: bar chart per gate (CI/Review/Human/Final) with insights (e.g. "Gate 3 at 72% — review for rubber-stamp pattern")
- **Cost Over Time Chart**: line chart, last 30 days, total + avg/day + projected
- **Worker Performance Table**: per-worker success/cost/time
- **Human Override Digest**: top justifications, pattern detection
- Range filter: 7d / 30d / 90d

**Interfaces:**
- `GET /api/v1/telemetry/gates?range=30d` — gate success rates
- `GET /api/v1/telemetry/cost?range=30d` — cost timeseries
- `GET /api/v1/telemetry/workers?range=30d` — worker performance
- `GET /api/v1/telemetry/overrides?range=30d` — override digest

- [ ] **Step 1: Write failing tests for each chart component**

- [ ] **Step 2: Implement components using existing chart library (recharts or similar — check `frontend/package.json` first)**

- [ ] **Step 3: Wire route + nav entry**

- [ ] **Step 4: Run frontend tests + typecheck**

---

### Task 30: Final verification gates (UI complete)

- [ ] **Step 1: Run the local gate**

Run: `cd backend && go build ./... && go test -race ./...`

- [ ] **Step 2: Run lint**

Run: `cd /Users/up-mac/wokrspace/mind/agent-orchestrator && npm run lint`

- [ ] **Step 3: Run frontend typecheck + build**

Run: `cd frontend && npm run typecheck && npm run build`

- [ ] **Step 4: Run sqlc + api regen (no diff = clean)**

Run: `npm run sqlc && npm run api && git diff --stat backend/internal/storage/sqlite/gen/ frontend/src/api/schema.ts`
Expected: empty diff.

- [ ] **Step 5: Run full CI locally if Docker is available**

Run: `npx @redwoodjs/agent-ci run --all`

- [ ] **Step 6: Run e2e smoke for new pages (Playwright or similar)**

Run: `bash test/e2e/run-ui-smoke.sh`

- [ ] **Step 7: Verify demo flow from design doc**

Manually walk through the 1-minute demo flow in the UX design doc — Org → Project → Pool → Approve → Telemetry → Promote.

---

## Cross-references

- Design: `../specs/2026-07-14-hybrid-approval-gates-design.md`
  - §11 Top-down architecture: four layers
  - §12 Worker pool architecture (Phase 1 scope)
- Backend mental model: `../../architecture.md`
- Backend package layout: `../../backend-code-structure.md`
- Conventions: `../../AGENTS.md`
- Tracker lane issue: modernagent/modern-agent#112
- Live Terminals precedent (CEO/PM/Worker): `../specs/2026-07-09-live-terminals-design.md`
- Raw PR events: modernagent/modern-agent#110, #111
- CLI parity: `docs/STATUS.md` "In flight — CLI parity for PR/review actions"

## New HTTP endpoints introduced in this plan

| Endpoint | Task | Purpose |
|---|---|---|
| `GET /api/v1/org/overview` | 25 (existing, consumed) | CEO dashboard data |
| `PUT /api/v1/org/tiers/{specialty}` | 25 | Trust tier promotion |
| `GET /api/v1/projects/{id}/pool` | 26 | Pool status |
| `GET /api/v1/projects/{id}/jobs` | 26 | Active jobs |
| `POST /api/v1/projects/{id}/jobs/{jobId}/cancel` | 26 | Cancel job |
| `GET /api/v1/projects/{id}/pool/workers` | 27 | Worker list |
| `POST /api/v1/projects/{id}/pool/workers/{workerId}/promote` | 27 | Promote tier |
| `GET /api/v1/approvals/inbox` | 28 | Pending approvals |
| `POST /api/v1/policy/runs/{runId}/decide` | 9, 28 | Human decision |
| `GET /api/v1/telemetry/gates` | 29 | Gate success rates |
| `GET /api/v1/telemetry/cost` | 29 | Cost timeseries |
| `GET /api/v1/telemetry/workers` | 29 | Worker performance |
| `GET /api/v1/telemetry/overrides` | 29 | Override digest |

After adding any of these, run `npm run api` to regenerate OpenAPI spec and
`frontend/src/api/schema.ts` (per `AGENTS.md` "API contract changes").