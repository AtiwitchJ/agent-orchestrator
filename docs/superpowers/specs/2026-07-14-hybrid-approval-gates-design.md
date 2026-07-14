# Hybrid Approval Gates for Tracker-Driven Workflows — Design

> **Status:** ready for plan. Grounded against the working tree on 2026-07-14.
> Every "current state" claim carries a file reference (most facts are pulled
> from `docs/STATUS.md`, the public surface map in
> `backend/internal/httpd/apispec/openapi.yaml`, and the package layout in
> `docs/backend-code-structure.md`).
>
> This is the design companion to the implementation plan at
> `../plans/2026-07-14-hybrid-approval-gates.md`.

---

## 0. Goal

Today Modern Agent can supervise parallel coding-agent sessions, observe PR/CI
state, surface concise PR summaries in the desktop UI, and push agent nudges
back into the owning session when CI fails, review comments land, or merge
conflicts appear. What it **cannot** do yet is drive a closed loop from
"issue labeled `agent-ready`" to "PR merged": the tracker lane is a static
adapter with no observer loop (#112), the SCM observer writes raw PR/tracker
facts but the desktop and `ao session get` only see the concise read model
(#110/#111), and the merge/review actions are HTTP-only with no CLI parity.

This design adds a **policy engine** that sits on top of the existing session
and SCM observers and runs every tracker-spawned PR through four sequential
gates — two of them machine-only, one of them human-only, and one of them a
"hybrid veto" that requires **both** a second-opinion agent **and** an
explicit human confirmation before a final-pass failure can be overridden. The
policy engine is opt-in per project (via the existing `PUT /projects/{id}/config`
route), every transition is a CDC event the desktop can subscribe to (reusing
the `change_log`/`events` plumbing already in place), and every human override
is auditable so we can tune the gates from real telemetry instead of vibes.

The product-facing payoff: a maintainer who labels an issue `agent-ready` can
walk away and come back to either a merged PR or a single, specific request
for their attention — not a pile of unread notifications.

---

## 1. Ground truth (what the code is today)

| Fact | Value | Source |
|---|---|---|
| Tracker lane exists as an adapter but no runtime loop | `internal/adapters/tracker/` is wired into the adapter registry but there is no daemon observer that mirrors issues to agent sessions | `docs/STATUS.md` "In flight" — #112 |
| SCM observer writes raw `pr_*` / `tracker_*` facts but desktop sees concise summaries | `internal/observe/scm` writes facts; `service.Session` derives the display read model; raw events are not surfaced through `ao session get` or as live CDC events to consumers | `docs/STATUS.md` — #110, #111 |
| Per-project config endpoint exists | `PUT /projects/{id}/config` is registered and already accepts arbitrary JSON config blobs | `docs/STATUS.md` "Shipped — Backend" |
| `needs_input` notification already plumbed | Durable dashboard notifications are persisted and streamed for `needs_input`, `ready_to_merge`, `pr_merged`, `pr_closed_unmerged`, with read acknowledgement and Electron toasts | `docs/STATUS.md` "Shipped — Frontend" |
| `ao send` CLI primitive exists | `POST /api/v1/sessions/{sessionId}/send` is implemented; CLI uses it; heartbeat nudges use it | `docs/superpowers/specs/2026-07-09-live-terminals-design.md` §1 |
| CLI parity gap for PR/review actions | `merge`, `resolve-comments`, `review` are HTTP-only; no `ao pr` / `ao review` commands | `docs/STATUS.md` "In flight" — CLI parity |
| CDC infra reusable | DB triggers append to `change_log`; daemon broadcaster serves SSE on `GET /api/v1/events` with `Last-Event-ID` replay | `docs/STATUS.md` "Shipped — Backend" |
| 23 agent adapters exist | Adapter registry under `internal/adapters/agent/` with `ao hooks` activity dispatch | `docs/STATUS.md` "Shipped — Backend" |
| Lifecycle/reaper in place | Reducer + reaper live under `internal/observe/reaper` | `docs/STATUS.md` "Shipped — Backend" |
| SQLite migrations are append-only | AGENTS.md hard rule: do not modify already-merged migrations, add a new one | `AGENTS.md` "Hard rules and boundaries" |
| All app state under `~/.ao` | Daemon data dir, `running.json`, worktrees, Electron `userData` all resolve under `~/.ao` (overridable via `AO_DATA_DIR`) | `AGENTS.md` "Hard rules and boundaries" |
| Loopback-only sidecar | Daemon binds only `127.0.0.1`; no auth, no CORS, no TLS — by design | `AGENTS.md` "Hard rules and boundaries" |
| Status is derived, not stored | Display session status comes from durable facts (`activity_state`, `is_terminated`, PR/check/comment facts) at service read time | `AGENTS.md` "Hard rules and boundaries" |

---

## 2. Decisions locked

1. **Four sequential gates, in order: CI → Agent review → Human approve →
   Agent final-pass.** Any failure short-circuits to the failure path of that
   gate; the run cannot skip ahead. Re-entry after a human `request_changes`
   restarts at Gate 2 (the new push already needs review), not Gate 1.

2. **Gate 4 failure → "hybrid veto"**: a second-opinion agent is spawned and a
   human is asked to confirm. **Both** the second agent and the human must
   approve for the run to proceed; if either rejects, the run stops and
   requires a human to either override (audited) or send the work back for
   revision. Human override of an agent veto is allowed but always audited
   (`human_override_after_veto` event with the human's stated justification).

3. **Review agent is project-configurable, with three strategies:**
   - `same_agent` (default) — reuse the primary agent adapter with
     `role='reviewer'` and a review-oriented prompt
   - `cross_agent` — spawn a different adapter (e.g. primary=claude, review=codex)
     to diversify blind spots
   - `second_only` — skip a Gate-2 review unless Gate 4 actually fires (cheaper,
     faster, but lets more through to the human)

4. **Round limits per gate** (per `PolicyConfig` overrides, defaults below):
   - Gate 1 CI: **3** auto-fix rounds, then escalate to human
     (`agent_exhausted_ci`)
   - Gate 2 Review: **3** revise rounds, then escalate to human
     (`agent_exhausted_review`)
   - Gate 3 Human: unbounded (human-driven)
   - Gate 4 Final-pass: **1** second-opinion round, then hybrid veto resolution
     by human

5. **Opt-in per project, never global default.** New projects default to
   `require_human_approval=true` and `review_strategy=same_agent`, so the
   behavior is "as safe as today" until a maintainer deliberately lowers a
   gate. No silent promotion.

6. **All policy state lives in SQLite, derived status flows through CDC.**
   Two new tables — `policy_runs` (one row per tracker-spawned PR lifecycle)
   and `gate_results` (one row per gate attempt) — both feed triggers into
   `change_log`. No parallel manual CDC emission from store methods.

7. **No new backend ports; reuse existing adapters.** The reviewer spawns
   sessions through the existing `internal/adapters/agent/` registry and the
   existing `internal/adapters/workspace/` (worktree). CI, review-comment, and
   merge-conflict signals come from the existing `internal/observe/scm`
   observer — the policy engine subscribes to its events, it does not
   duplicate the polling.

8. **Hybrid veto second-opinion agent is a separate config field** from
   `review_agent` so a project can use `cross_agent` for Gate 2 (diverse
   review) while still pinning a specific adapter for veto second-opinion
   (trust-stability).

9. **No new daemon flags, no new bind host, no auth, no CORS, no TLS.** The
   engine runs in-process inside the loopback daemon; the only new HTTP
   surface is read-only state endpoints (see §5 Non-goals).

10. **Phase 1 ships closed-loop for the happy path only.** Sad-path branches
    (veto, exhaustion, manual override, conflict rebase failure) are
    implemented but tagged `policy_v1_experimental` until Phase 2 telemetry
    confirms they behave. Frontend shows a small banner when the experimental
    flag is on for a run.

---

## 3. State machine

```
                        ┌──────────────────────────────────────┐
                        │  Issue labeled "agent-ready"         │
                        │  → tracker observer spawns session   │
                        └────────────────┬─────────────────────┘
                                         ↓
                  ┌────────────────────────────────────────────┐
                  │  Agent pushes commit → PR opens            │
                  └────────────────────┬───────────────────────┘
                                       ↓
                ╔══════════════════════════════════════════════╗
                ║  Gate 1: CI                                 ║
                ║  • checks.status == success                 ║
                ║  • fail → agent auto-fix (≤3 rounds)        ║
                ║  • exhausted → notify human, stop run       ║
                ╚═══════════════════╤══════════════════════════╝
                                    ↓ pass
                ╔══════════════════════════════════════════════╗
                ║  Gate 2: Agent self-review                  ║
                ║  • strategy: same_agent | cross_agent |      ║
                ║              second_only                    ║
                ║  • fail → revise (≤3 rounds)                 ║
                ║  • exhausted → notify human, stop run       ║
                ╚═══════════════════╤══════════════════════════╝
                                    ↓ pass
                ╔══════════════════════════════════════════════╗
                ║  Gate 3: Human approve                      ║
                ║  • notification: needs_input                ║
                ║  • approve → continue                       ║
                ║  • request_changes → restart at Gate 2      ║
                ║  • no-response timeout → escalate (assign   ║
                ║    back, label 'stale-agent-work')          ║
                ╚═══════════════════╤══════════════════════════╝
                                    ↓ approve
                ╔══════════════════════════════════════════════╗
                ║  Gate 4: Agent final-pass                   ║
                ║  • rebase against main                      ║
                ║  • lint/policy scan                         ║
                ║  • secret/leftover-debug scan               ║
                ║  • commit-message convention check          ║
                ║  • fail → HYBRID VETO path                  ║
                ╚═══════════════════╤══════════════════════════╝
                                    ↓ pass
                  ┌────────────────────────────────────────────┐
                  │  Merge policy check                        │
                  │  • strategy (squash|rebase|merge)          │
                  │  • min PR age, approvals, draft block      │
                  └────────────────┬───────────────────────────┘
                                   ↓
                            squash-merge → run done
```

### 3.1 Hybrid veto path (Gate 4 failure)

```
Gate 4 fail (e.g. rebase conflict, lint failure, secret scan hit)
   ↓
spawn second-opinion agent (adapter = veto_second_agent)
   ↓
Second agent reviews → vote: approve | reject (with rationale)
   ↓
Notify human: "Gate 4 vetoed. Reason: <x>. Second opinion: <vote>. Override?"
   ↓
   ├── Override + proceed to merge
   │     → CDC: human_override_after_veto { justification }
   ├── Override + send back for revision
   │     → spawn new revision session, restart at Gate 2
   └── Defer to second-opinion agent vote
         → if approve: proceed to merge (no override needed)
         → if reject: stop run, notify human
```

**Conflict-resolution rule:** the second-opinion agent **never** has final say
on its own. A `reject` from the second agent can be overridden by a human,
but the human override is always logged with a required justification field.
A `reject` from both is a hard stop until the human acts.

### 3.2 Re-entry after `request_changes`

When a human issues `request_changes` at Gate 3, the policy run stays alive
but transitions back to Gate 2 (not Gate 1, since CI already passed). This
avoids burning CI cycles on an already-known-broken commit and respects the
reviewer's signal.

---

## 4. Configuration schema

`PolicyConfig` lives inside the existing `ProjectConfig` JSON blob — no new
endpoint, no schema migration, just a typed accessor on the service layer:

```go
type PolicyConfig struct {
    // Master switches — opt-in
    Enabled                  bool   `json:"enabled"`                  // default false
    TrackerLabel             string `json:"tracker_label"`            // default "agent-ready"

    // Gate 1: CI
    AutoFixOnCIFailure       bool   `json:"auto_fix_on_ci_failure"`   // default true
    MaxAutoFixRounds         int    `json:"max_auto_fix_rounds"`      // default 3

    // Gate 2: Agent self-review
    RequireAgentReview       bool   `json:"require_agent_review"`     // default true
    ReviewStrategy           string `json:"review_strategy"`          // default "same_agent"
    // "same_agent" | "cross_agent" | "second_only"
    ReviewAgent              string `json:"review_agent"`             // required when cross_agent
    MaxReviseRounds          int    `json:"max_revise_rounds"`        // default 3

    // Gate 3: Human approve
    RequireHumanApproval     bool   `json:"require_human_approval"`   // default true
    HumanTimeoutHours        int    `json:"human_timeout_hours"`      // 0 = no timeout

    // Gate 4: Agent final-pass
    AgentFinalPass           bool   `json:"agent_final_pass"`         // default true
    VetoSecondAgent          string `json:"veto_second_agent"`        // default = ReviewAgent

    // Merge policy
    MergeStrategy            string `json:"merge_strategy"`           // default "squash"
    // "squash" | "rebase" | "merge"
    MinPRAgeMinutes          int    `json:"min_pr_age_minutes"`       // default 5
    BlockOnDraft             bool   `json:"block_on_draft"`           // default true
}
```

Defaults are conservative: a project with `enabled=false` is unaffected. A
project with `enabled=true` and no other overrides gets the full four-gate
sequence with `same_agent` review and `squash` merge.

---

## 5. CDC event surface

All events flow through the existing `change_log` trigger/broadcaster. No new
SSE route is needed — `GET /api/v1/events` already serves these.

| Event | When | Payload |
|---|---|---|
| `policy_run_started` | PR opened on a tracker-spawned session | `{ run_id, project_id, session_id, pr_id, config_snapshot }` |
| `gate_started` | Per gate entry | `{ run_id, gate_id, attempt }` |
| `gate_passed` | Per gate success | `{ run_id, gate_id, attempt, duration_ms }` |
| `gate_failed` | Per gate failure (with reason) | `{ run_id, gate_id, attempt, reason, will_retry }` |
| `gate_exhausted` | Round limit hit | `{ run_id, gate_id, rounds_attempted }` |
| `human_override_requested` | Hybrid veto asks human | `{ run_id, gate_id, second_opinion_vote }` |
| `human_override_decided` | Human acts on override | `{ run_id, decision, justification }` |
| `hybrid_veto_triggered` | Gate 4 fail | `{ run_id, reason, second_agent }` |
| `second_opinion_received` | Veto agent votes | `{ run_id, vote, rationale }` |
| `merge_blocked` | Merge policy rejects | `{ run_id, reason }` |
| `merge_executed` | Merge committed | `{ run_id, sha, strategy }` |
| `policy_run_completed` | Terminal state | `{ run_id, final_state }` |

Every payload includes `run_id` so the desktop and CLI can correlate a single
PR's full journey without polling.

---

## 6. Non-goals for v1

- **No auto-skip of human approval.** Even with `require_human_approval=false`,
  the engine still records every transition; it just skips the wait. Phase 2
  may revisit this once telemetry confirms trust.
- **No multi-PR orchestration.** One tracker issue → one PR → one run.
  Stacked PRs are out of scope.
- **No cross-run coordination.** Two parallel tracker runs on the same project
  are independent; race-on-merge is handled by GitHub itself.
- **No new auth, no CORS, no TLS, no network surface.** Loopback-only.
- **No frontend redesign.** The existing session view + notifications page gain
  a small "Policy" badge and a hybrid-veto dialog; the existing layout is
  reused.
- **No org-level default templates.** Phase 3.
- **No per-agent-type trust tiers.** Phase 3.

---

## 7. Risks and mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Approval fatigue — humans rubber-stamp everything | Defeats the point of Gate 3 | Telemetry in Phase 2 measures approve-after-veto rate; if high, surface "policy adjustment" suggestion, not auto-change |
| Cost explosion from review + revise loops | $$ | Per-project cost cap (`ao project set cost-cap`); `gate_exhausted` events tagged with cost; kill switch via `ao policy disable <project>` |
| Round limits too low → false negatives | Quality drops | 3 rounds is a starting default; project override + Phase 2 telemetry will tune per gate |
| Round limits too high → waste | $$ | Hard ceiling at 5 rounds regardless of override; reject value with error if attempted |
| Same-model bias in `same_agent` review | Catches less | `cross_agent` is one config toggle away; we encourage it for mature projects in docs |
| Human override of agent veto becomes silent rubber-stamp | Trust erodes | `human_override_after_veto` requires `justification` field; weekly digest in Phase 2 lists top override-justifications |
| Tracker spawn storm (many issues labeled at once) | Resource exhaustion | Tracker observer rate-limits session spawns via the existing `internal/observe/reaper` cadence; `policy_runs` table has unique constraint per `(project_id, pr_id)` |
| Rebase during Gate 4 conflicts with another in-flight merge | Race | Retry once via existing rebase logic; if second conflict, escalate hybrid veto immediately |
| Phase 1 ships with `policy_v1_experimental` flag | UX noise | Frontend banner is small and dismissible per project; auto-removes once Phase 2 promotes the flag off |
| New SQLite tables change CDC contract | Migration drift | Both new tables add `change_log` triggers; existing schema drift tests will catch regressions |

---

## 8. Phase plan

**Phase 1 — Tracker + Hybrid Gates (this plan, ~5–6 months)**
1. Tracker observer loop (#112): issue labeled `tracker_label` → spawn session in
   worktree, link issue ↔ session ↔ PR.
2. Raw `pr_*` / `tracker_*` events exposed (#110, #111): minimum needed for
   debugging gate runs.
3. CLI parity for `ao pr` / `ao review` (read + act).
4. New package `internal/policy/` with gate engine + state machine.
5. Two new SQLite tables (`policy_runs`, `gate_results`) + triggers; sqlc regen.
6. Hybrid veto path including second-opinion agent + human confirmation.
7. All 12 CDC event types wired and surfaced via existing `/api/v1/events`.
8. Frontend: policy badge on session view, hybrid-veto dialog, experimental
   banner.
9. Twelve test scenarios: each gate × pass/fail/exhaustion, plus override
   paths.

**Phase 2 — Trust ladder (post-Phase 1)**
- Per-gate success rate dashboard (read-only, derived from CDC events).
- "Promote to skip" workflow — human-initiated, never automatic.
- Cost telemetry per project / per agent.
- Remove `policy_v1_experimental` flag once telemetry confirms behavior.

**Phase 3 — Org defaults (post-Phase 2)**
- Org-level policy templates.
- Per-agent-type trust tier (e.g. "claude=trusted, qwen=experimental").
- Auto-downgrade to human-only mode when cost ceiling exceeded.

---

## 9. Open questions deferred

These are intentionally **not** decided in this design; they belong in the
plan or Phase 2 design:

- Exact prompt template for `same_agent` review role — owner picks during
  implementation; should land in `internal/policy/prompts/`.
- Cost-currency calculation — Phase 2 telemetry requires pricing data per
  adapter that we don't have today.
- Whether `human_timeout_hours` escalation should also kill the agent session
  or just leave it idle. Likely just idle (don't kill work the user might
  want to come back to).
- Whether the experimental banner is per-project or per-run. Tentative:
  per-run, so a single bad PR doesn't taint the whole project's UI.

---

## 11. Top-down architecture: four layers

Modern Agent already has a multi-layer orchestrator model in the working tree
(see `docs/superpowers/specs/2026-07-09-live-terminals-design.md` §1 for
`holdingHq.orchestratorSessionId`, `companies[].hq.orchestratorSessionId`,
`projects[].orchestratorSessionId`). This design does not invent a new
hierarchy — it sits the policy engine and worker pool on top of the existing
CEO/PM/Worker chain.

### 11.1 The four layers

```
┌─────────────────────────────────────────────────────────────────┐
│  LEVEL 0: USER (Maintainer / Developer)                          │
│  ─────────────────────────────────────                          │
│  • ติดป้าย issue "agent-ready"                                  │
│  • กด approve / request changes                                 │
│  • ตัดสิน merge ขั้นสุดท้าย                                     │
│  • Override hybrid veto (พร้อม justification)                   │
└─────────────────────────────┬───────────────────────────────────┘
                              ↓ (rare, high-stakes decisions)
┌─────────────────────────────────────────────────────────────────┐
│  LEVEL 1: CEO (Holding HQ) — Strategic                          │
│  ─────────────────────────────────────                          │
│  • Cadence: weekly / monthly                                    │
│  • ตัดสิน: "company ไหนควรรับ issue นี้"                         │
│  • ตัดสิน: "priority ของ project ไหนสูงกว่า"                     │
│  • ตัดสิน: "cost ceiling ของ org ถึง limit หรือยัง"              │
│  • ตัดสิน: "tier policy ของ specialty ไหน" (trusted/banned)     │
│  • Visibility: aggregate capacity ข้าม projects                  │
│  • ไม่เห็น: technical detail, individual PR                      │
│                                                                  │
│  Infra: holdingHq.orchestratorSessionId (1 session ต่อ org)     │
└─────────────────────────────┬───────────────────────────────────┘
                              ↓ (assigns to company)
┌─────────────────────────────────────────────────────────────────┐
│  LEVEL 2: PM (Company HQ / Project HQ) — Tactical               │
│  ─────────────────────────────────────                          │
│  • Cadence: daily                                               │
│  • รับ issue จาก CEO หรือ tracker                                │
│  • แตก issue เป็น jobs                                          │
│  • Dispatch jobs ไป worker pool                                 │
│  • Review PR จาก workers                                         │
│  • Aggregate ผล → ส่ง human approve                             │
│  • Iterate เมื่อ fail                                           │
│                                                                  │
│  Infra: companies[].hq.orchestratorSessionId (1 ต่อ project)    │
└─────────────────────────────┬───────────────────────────────────┘
                              ↓ (dispatch job)
┌─────────────────────────────────────────────────────────────────┐
│  LEVEL 3: WORKER POOL — Execution                               │
│  ─────────────────────────────────────                          │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │  Pool (per project, isolated)                             │  │
│  │  ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐         │  │
│  │  │Worker   │ │Worker   │ │Worker   │ │Worker   │         │  │
│  │  │[BE-API] │ │[FE-React│ │[DB]     │ │[Test]   │         │  │
│  │  │success: │ │success: │ │success: │ │success: │         │  │
│  │  │ 92%     │ │ 85%     │ │ 78%     │ │ 88%     │         │  │
│  │  └─────────┘ └─────────┘ └─────────┘ └─────────┘         │  │
│  │  ┌─────────┐ ┌─────────┐                                  │  │
│  │  │Worker   │ │Worker   │                                  │  │
│  │  │[Sec]    │ │[Docs]   │                                  │  │
│  │  │tier:    │ │tier:    │                                  │  │
│  │  │experim. │ │trusted  │                                  │  │
│  │  └─────────┘ └─────────┘                                  │  │
│  └──────────────────────────────────────────────────────────┘  │
│                                                                  │
│  • Workers spawn on-demand, return to pool when done             │
│  • Mesh sync: workers คุยกันเองผ่าน CDC events                  │
│  • ไม่ผูกกับ job เฉพาะ — reusable                               │
│  • Pool collect success rate / cost / time telemetry             │
│                                                                  │
│  Infra: workers[] ใน worktree แยก, 23 agent adapters           │
└─────────────────────────────┬───────────────────────────────────┘
                              ↓ (execute job)
┌─────────────────────────────────────────────────────────────────┐
│  LEVEL 4: RUNTIME — Plumbing                                    │
│  ─────────────────────────────────────                          │
│  • tmux / conpty (terminal per worker)                          │
│  • Git worktree (isolation per worker)                          │
│  • SQLite + CDC events (state + broadcast)                      │
│  • SCM observer (PR/CI/review facts)                            │
│  • Agent adapter registry (claude/codex/cursor/...)             │
└─────────────────────────────────────────────────────────────────┘
```

### 11.2 Cadence and scope per layer

| Level | ชื่อ | Cadence | Visibility | Scope |
|---|---|---|---|---|
| 0 | User | on-demand | full | 1 issue / 1 PR |
| 1 | CEO | weekly | aggregate | org-wide |
| 2 | PM | daily | per-project | 1 project |
| 3 | Worker Pool | per-job | per-job | 1 job |
| 4 | Runtime | continuous | per-worker | 1 worker |

Each layer has a different cadence — they don't block each other and scale
independently. Adding workers doesn't impact CEO/PM; adding projects adds a
new pool (isolated); adding PMs doesn't change CEO's view beyond aggregate
capacity.

### 11.3 Communication patterns

**Hierarchical (slow → fast)**
- CEO → PM: heartbeat (rare, strategic) — already exists
- PM → Worker: dispatch job (per task) — Phase 1
- Worker → PM: report result (when done) — Phase 1

**Mesh (peer-to-peer, within project)**
- Worker[BE] → Worker[FE]: "API contract ออกแล้วนะ"
- Worker[FE] → Worker[BE]: "ต้องการ field เพิ่ม"
- Worker[Test] → Worker[BE]: "test fail ที่ endpoint นี้"
- Transport: existing CDC events; workers subscribe to each other through
  `GET /api/v1/events` filtered by `project_id`

**Hybrid veto (3 layers, Phase 2)**
- Agent (technical check) — Phase 1
- PM (context check) — Phase 2
- Human (final say) — Phase 1

### 11.4 Where the policy engine sits

The policy engine is a **cross-cutting concern** that observes transitions
between Level 2 (PM) and Level 3 (Worker Pool) and gates the merge. It does
not own any layer; it reads CDC events from the existing SCM observer and
the new tracker observer (Task 1) and writes results to `policy_runs` /
`gate_results`. The four gates in §3 are evaluated per-PR, not per-layer —
a single PR from a single worker goes through all four gates, but a multi-PR
issue from multiple workers in a mesh zone gets aggregated by the PM before
Gate 3 (human approval is per-issue, not per-PR).

---

## 12. Worker pool architecture (Phase 1 scope)

The worker pool is the level-3 execution layer. This design decides only the
Phase 1 slice; later phases extend it.

### 12.1 What a Worker is

```
Worker = subagent + specialty + reusable lifecycle
```

Not:
- ❌ "Session ถาวร" that persists forever
- ❌ "Generic agent" that does everything

But:
- ✅ **subagent** — spawned by PM on demand, returns to pool when done
- ✅ **specialty** — has a tag saying what it's good at (BE/FE/DB/Test/Sec/Docs)
- ✅ **reusable** — keeps success rate, cost, and time telemetry so PM can
  pick the best fit

### 12.2 Pool scope (Phase 1 decision)

**Per project + CEO visibility.**

```
โครงการ A: Pool[BE x 2, FE x 2, DB x 1, Test x 1, Sec x 1] = 7 workers
โครงการ B: Pool[BE x 1, FE x 3, DB x 1, Test x 1] = 6 workers

CEO dashboard (read-only, derived from CDC events):
  Org capacity: 13 workers total
  Active jobs: 8 (A: 5, B: 3)
  Idle: 5 (A: 2, B: 3)
  Cost today: $12.40
```

Rationale:
- **Isolation** — project A has sensitive work (auth/payment) that shouldn't
  share resources with project B
- **Cache locality** — workers in project A learn codebase A faster
- **CEO visibility** — strategic optimization without tactical interference
- **Practical** — start per-project, evolve to shared in Phase 3 if telemetry
  shows underutilization

Not chosen:
- ❌ Per-project only (CEO blind) — loses strategic visibility
- ❌ Shared across org (CEO-owned) — breaks isolation, harder to reason about
  cache and secrets

### 12.3 Specialty tags (Phase 1 decision)

**Hybrid: built-in tags + PM override per job.**

Built-in (default, predictable):
- `BE` (Backend) — API, business logic
- `FE` (Frontend) — UI, React/Vue/etc
- `DB` (Database) — schema, migration, query
- `Test` — test cases, coverage
- `Sec` (Security) — security review, secret scan
- `Docs` — documentation
- `Perf` — performance optimization
- `Refactor` — code cleanup

PM override (per job, flexible):
```
PM: "job นี้ต้องการ BE-API specialist ที่เก่ง GraphQL"
  → Pool match: BE + custom tag "GraphQL"
  → ถ้าไม่มี → spawn new worker + tag
```

Trade-off accepted: if PM override is overused, tag proliferation becomes a
governance problem. Phase 2 adds tag governance (deduplication, retirement
rules). Phase 1 keeps the escape hatch open.

### 12.4 Trust model (Phase 1 decision)

**Pool-owned rating + CEO policy tier.**

Pool auto-collects (no human input):
```
worker[BE-API] success rate: 92% (rolling 30 days)
worker[BE-API] avg cost: $0.15
worker[BE-API] avg time: 8 min
worker[BE-API] specialties: [GraphQL, REST, gRPC]
```

CEO sets tier (manual, per specialty, in org config):
- `trusted` — auto-dispatch, no PM approval needed
- `experimental` — require PM approval before dispatch
- `banned` — never dispatch (security incident, license issue, etc.)

Phase 2 evolves this: `experimental` → `trusted` once success rate hits a
threshold, **human-initiated** (CEO clicks promote), never automatic. This
keeps the trust ladder honest — promotions are visible and reviewable.

### 12.5 Dispatch flow

```
PM ได้รับ issue: "เพิ่ม user authentication"
   ↓
PM แตกเป็น jobs:
   1. "design API contract"     → specialty: BE-API, tier: trusted → auto
   2. "DB migration"            → specialty: DB,      tier: trusted → auto
   3. "login UI"                → specialty: FE-React, tier: trusted → auto
   4. "test cases"              → specialty: Test,    tier: trusted → auto
   5. "security review"         → specialty: Sec,     tier: experimental → PM approve
   ↓
Pool dispatch (per job):
   - Look up specialty + tier
   - Pick best-fit worker (highest success rate, lowest cost, available)
   - If no worker available: spawn new (within pool size cap)
   - If tier=experimental: emit `pm_approval_required`, wait for PM `decide`
   - If tier=banned: emit `dispatch_blocked`, fail the job, PM picks another
   ↓
Worker executes:
   - Spawns in own worktree
   - Reports back via `ao send` or CDC event when done
   - Returns to pool (warm, keeps learned context)
   ↓
PM aggregates:
   - Collects all job results
   - Runs Gate 1-4 on each PR
   - Sends single approval request to human (per issue, not per PR)
```

### 12.6 Mesh sync within pool

Workers in the same pool can subscribe to each other's CDC events without
going through PM:

```
PM dispatches: [API contract job] → Worker[BE-API]
   ↓ publishes CDC event: "api_contract_draft_ready"
Worker[FE-React] subscribed to api_contract topics → starts UI work
Worker[Test] subscribed to api_contract topics → starts test stubs
   ↓ all 3 work in parallel
```

This is the same `GET /api/v1/events` SSE stream the desktop already uses.
Phase 1 adds a `mesh_topics` field to `WorkerConfig` (defaults: empty list,
opt-in per worker). PM doesn't need to know about mesh topics — workers
self-organize based on what they subscribe to.

### 12.7 What Phase 1 does NOT ship

- ❌ Auto-spawn of workers beyond pool size cap (pool size is fixed at config
  load; overflow jobs queue)
- ❌ Cross-pool worker migration (a worker in pool A cannot serve pool B)
- ❌ Worker learning (workers don't accumulate codebase memory across jobs;
  Phase 2 may add `.workerstate/` per worker)
- ❌ Tag governance (no deduplication, no retirement; accepted cost in Phase 1)
- ❌ PM arbitration layer in hybrid veto (Phase 1 keeps agent + human only;
  PM arbitration deferred to Phase 2)
- ❌ Cost-based auto-scaling (pool size is config, not auto-tuned)
- ❌ Worker migration on host failure (worker dies → job fails → PM retries;
  no high-availability story in Phase 1)

These are intentional Phase 1 boundaries. The design above describes the
target shape; the implementation plan (§17 in the plan doc) defines what
gets built first.

---

## 14. Worker invocation: 3 modes (subprocess + SDK + tmux)

Workers in the pool can be invoked three different ways. Each mode has
clear trade-offs; Phase 1 ships **subprocess + tmux** (universal, no vendor
lock-in). Phase 2 adds **SDK** as an optimization for Claude Code.

### 14.1 Subprocess (universal, default)

```python
# Hermes spawns CLI as separate process
import asyncio

async def invoke_subprocess(worker: WorkerConfig, prompt: str, workdir: str) -> WorkerResult:
    proc = await asyncio.create_subprocess_exec(
        *worker.cmd,                    # e.g. ["claude", "-p"]
        cwd=workdir,
        stdin=asyncio.subprocess.PIPE,
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.PIPE
    )
    stdout, stderr = await proc.communicate(input=prompt.encode())
    return worker.output_parser.parse(stdout)
```

**Data flow:** `Hermes → spawn process → wait → parse output → return`

| ✅ | ❌ |
|---|---|
| Universal (any CLI) | Blocks until process exits |
| Zero integration | No streaming (output ตอนจบอย่างเดียว) |
| Auth handled by CLI | Parse text/JSON manually |
| CLI features (`--resume`, `--workdir`) | No tool use (agent can't call back) |
| Process crash isolated | Process overhead per call |

**Compatible tools (Phase 1):** Codex, Agy, OpenCode, Kilo, ComfyUI,
himalaya, imsg, custom shell commands.

### 14.2 SDK (Phase 2 — Claude Code only)

```python
# Hermes calls vendor SDK directly
import claude_code

client = claude_code.Client(api_key=os.environ["ANTHROPIC_API_KEY"])

async def invoke_sdk(worker: WorkerConfig, prompt: str, conversation_id: str | None) -> WorkerResult:
    async with client.messages.stream(
        model=worker.model,              # e.g. "claude-sonnet-4-6"
        messages=[{"role": "user", "content": prompt}],
        tools=worker.tools,              # Hermes-defined tools
        conversation_id=conversation_id  # for resume
    ) as stream:
        result = None
        async for chunk in stream:
            if chunk.is_tool_use:
                await handle_tool_call(chunk)
            result = chunk
        return result
```

**Data flow:** `Hermes ↔ stream ↔ Claude API` (bidirectional, real-time)

| ✅ | ❌ |
|---|---|
| Streaming (real-time chunks) | Vendor lock-in (Claude only) |
| Structured output (JSON) | Auth separate from CLI |
| Tool use (agent calls Hermes) | Lose CLI features |
| Interrupt cleanly (break loop) | Need SDK per vendor |
| Cost tracking (tokens in response) | |

**Phase 2 trigger:** Add SDK path only when subprocess shows measurable
bottleneck (e.g. > 30s cold start, or unparseable outputs).

### 14.3a PTY (built-in Python, default for streaming)

**Discovered via prototype (2026-07-14):** tmux is not always installed
(especially on macOS without Homebrew). Python's built-in `pty` module
provides real-time streaming without external dependencies.

```python
# Hermes spawns worker with pseudo-terminal
import os, pty, select, asyncio

async def invoke_pty(worker: WorkerConfig, prompt: str) -> AsyncIterator[str]:
    master, slave = pty.openpty()
    pid = os.fork()
    if pid == 0:
        # Child: replace stdio with slave pty, exec worker
        os.dup2(slave, 0); os.dup2(slave, 1); os.dup2(slave, 2)
        os.execvp(worker.cmd[0], worker.cmd + [prompt])
    # Parent: read from master pty
    os.close(slave)
    loop = asyncio.get_event_loop()
    while True:
        data = await loop.run_in_executor(None, _read_pty, master)
        if not data: break
        yield data.decode(errors="replace")
    os.waitpid(pid, 0)
```

**Data flow:** `Hermes → fork+exec → pty → stream bytes → async iterator`

| ✅ | ❌ |
|---|---|
| Built-in Python (zero install) | No persistent session across calls |
| Real-time streaming | Cleanup on crash is harder |
| Works on macOS, Linux | Not Windows native |
| Lower overhead than tmux | No user attach (can't `tmux attach`) |

**Phase 1 default for streaming tasks** — proven via prototype at
`docs/superpowers/prototypes/2026-07-14-worker-invocation.py`.

### 14.3 TMUX (Modern Agent style — interactive/long-running, opt-in)

```python
# Hermes spawns agent in tmux session, streams via WebSocket
import libtmux

async def invoke_tmux(worker: WorkerConfig, prompt: str, session_name: str) -> AsyncIterator[str]:
    server = libtmux.Server()
    session = server.sessions.new(
        session_name=session_name or f"worker-{uuid4()}",
        window_command=f"{' '.join(worker.cmd)} '{prompt}'"
    )

    # Stream output
    pane = session.active_window.active_pane
    last_lines = []
    while not is_done(pane):
        current = pane.capture_pane()
        new = current[len(last_lines):]
        for line in new:
            yield line
        last_lines = current
        await asyncio.sleep(0.5)

    # Cleanup
    session.kill()
```

**Data flow:** `Hermes → tmux spawn → pane buffer → WebSocket → UI`

| ✅ | ❌ |
|---|---|
| Real-time streaming (terminal-like) | No structured output |
| User can interrupt (Ctrl+C via pane) | No tool use (without custom signaling) |
| Persistent session (resume tmux) | Resource overhead (tmux + agent + WS) |
| User sees live progress | Cleanup responsibility (kill session) |
| Compatible with any CLI | Hard to test (needs tmux) |

**Use when:** Long-running tasks (1+ hours), user wants visibility,
agent may need mid-task course correction.

### 14.4 Mode selection per worker

```yaml
# workers.yaml — registry
workers:
  - name: claude-pm
    adapter: claude_code
    modes: [subprocess, sdk, tmux]
    default_mode: sdk                    # CEO↔PM uses SDK
    tier: trusted
    cost_per_call: 0.15

  - name: codex-worker
    adapter: codex_cli
    modes: [subprocess, tmux]
    default_mode: subprocess
    cmd: ["codex", "exec", "--json"]
    tier: trusted
    cost_per_call: 0.12

  - name: agy-worker
    adapter: agy_cli
    modes: [subprocess]
    default_mode: subprocess
    cmd: ["agy", "-p", "--print"]
    tier: trusted
    cost_per_call: 0.10

  - name: image-gen-batch
    adapter: comfyui_cli
    modes: [subprocess]
    default_mode: subprocess
    cmd: ["comfyui", "generate", "--workflow", "sdxl.json"]
    tier: experimental
    cost_per_call: 0.50

  - name: long-refactor
    adapter: claude_code
    modes: [tmux]
    default_mode: tmux
    tier: trusted
```

### 14.5 Worker abstraction interface

```go
// internal/worker/worker.go (Phase 1)
type Worker interface {
    Name() string
    Tier() Tier
    Available() bool

    // Phase 1 modes
    Invoke(ctx context.Context, req InvokeRequest) (InvokeResult, error)
    InvokeTmux(ctx context.Context, req InvokeRequest, sessionName string) (<-chan string, error)

    // Phase 2
    InvokeSDK(ctx context.Context, req InvokeRequest) (<-chan StreamChunk, error)
}

type InvokeRequest struct {
    Prompt    string
    Workdir   string
    Context   map[string]string
    Timeout   time.Duration
}

type InvokeResult struct {
    Output   string
    Cost     float64
    Duration time.Duration
    Metadata map[string]string
}
```

### 14.6 When to use which mode (decision tree)

```
Is the task interactive (user watches + may interrupt)?
├── YES → PTY (default) or TMUX (opt-in, if user wants to attach)
│   Examples: long refactor, debugging session, live demo
│
└── NO → Is the tool Claude Code AND cost/speed critical?
    ├── YES → SDK (Phase 2)
    │   Examples: PM↔Worker conversation, cost-sensitive batch
    │
    └── NO → SUBPROCESS
        Examples: batch jobs, one-shot tasks, non-Claude tools
```

**Phase 1 default stack:**
- `subprocess` for fire-and-forget (batch, custom workers)
- `pty` for real-time streaming (long tasks, real-time progress)
- `tmux` only when user explicitly wants to attach to a session (rare)

**Phase 2 add:** `sdk` for Claude Code when bottleneck is measured.

---

## 15. Custom workers: beyond coding

The pool is **not limited to coding agents**. Phase 1 supports custom
workers for any tool that can be invoked as a CLI or subprocess. Each
worker has a `tier` (trusted/experimental/banned) and a `specialty`.

### 15.1 Built-in specialty categories

```go
type Specialty string

const (
    SpecCoding    Specialty = "code"      // Claude Code, Codex, etc.
    SpecImage     Specialty = "image"     // ComfyUI, DALL-E, Midjourney
    SpecVideo     Specialty = "video"     // Runway, Pika, ComfyUI Wan
    SpecAudio     Specialty = "audio"     // heartmula, audiocraft
    SpecEmail     Specialty = "email"     // himalaya, gmail
    SpecIM        Specialty = "im"        // imsg, slack
    SpecProductivity Specialty = "productivity"  // notion, airtable
    SpecResearch  Specialty = "research"  // arxiv, blogwatcher
    SpecCustom    Specialty = "custom"    // anything user-defined
)
```

### 15.2 Example custom workers

```yaml
# Image generation
- name: comfyui-sdxl
  specialty: image
  cmd: ["comfyui", "generate", "--workflow", "sdxl_txt2img"]
  input_schema:
    prompt: string
    negative_prompt: string
    seed: int
  output: png_file
  tier: experimental
  cost_per_call: 0.05

# Video generation
- name: comfyui-wan
  specialty: video
  cmd: ["comfyui", "generate", "--workflow", "wan_t2v"]
  input_schema:
    prompt: string
    duration_seconds: int
  output: mp4_file
  tier: experimental
  cost_per_call: 0.50

# Email send
- name: himalaya-send
  specialty: email
  cmd: ["himalaya", "send", "--template", "default"]
  input_schema:
    to: string
    subject: string
    body: string
  output: message_id
  tier: trusted
  cost_per_call: 0.00

# Research
- name: arxiv-search
  specialty: research
  cmd: ["arxiv", "search", "--format", "json"]
  input_schema:
    query: string
    max_results: int
  output: paper_list
  tier: trusted
  cost_per_call: 0.00

# Music generation (HeartMuLa)
- name: heartmula
  specialty: audio
  cmd: ["heartmula", "generate", "--lyrics-file", "-"]
  input_schema:
    lyrics: string
    style: string
  output: mp3_file
  tier: experimental
  cost_per_call: 0.20

# Productivity
- name: notion-page-create
  specialty: productivity
  cmd: ["ntn", "page", "create"]
  input_schema:
    title: string
    content: string
    parent_id: string
  output: page_url
  tier: trusted
  cost_per_call: 0.00

# Social media
- name: xurl-post
  specialty: im
  cmd: ["xurl", "post"]
  input_schema:
    text: string
    media_paths: list[string]
  output: post_url
  tier: experimental
  cost_per_call: 0.00
```

### 15.3 Custom worker creation

PM can declare new custom workers at runtime:

```yaml
# PM registers new worker during issue planning
- name: my-custom-tool
  specialty: custom
  cmd: ["./scripts/my-tool.sh"]
  input_schema:
    param1: string
    param2: int
  output: text
  tier: experimental             # PM must approve first dispatch
  pm_justification: "needed for migration script"
```

Pool spawns the worker on first use, tracks success rate, and surfaces
to CEO for tier promotion (per §12.4 trust model).

### 15.4 Worker types by use case

| Use case | Workers | Mode |
|---|---|---|
| **Code review** | Claude Code, Codex, OpenCode | Subprocess (Phase 1) |
| **Refactor** | Claude Code (long task) | TMUX (user watches) |
| **Image gen** | ComfyUI local | Subprocess (batch) |
| **Video gen** | ComfyUI Wan | Subprocess (long) or TMUX |
| **Email** | himalaya, imsg | Subprocess (fire-forget) |
| **Research** | arxiv, blogwatcher | Subprocess (parse JSON) |
| **Music** | heartmula, audiocraft | Subprocess (long) or TMUX |
| **PM conversation** | Claude Code, Codex | SDK (Phase 2) |
| **Live demo** | Any | TMUX (user watches) |

---

## 16. Hermes CEO behavior: when user is away

When the user is not present, **Hermes (CEO) = Modern Agent's internal
orchestrator sessions** (Holding/Company/Project HQ) takes over decision-
making. The behavior depends on **stakes** and **policy**.

### 16.1 Decision matrix

```
Stakes     │ User present      │ User away (default)    │ User away (urgent)
───────────┼───────────────────┼────────────────────────┼────────────────────
Low        │ Ask user          │ Hermes decides         │ Hermes decides
Medium     │ Ask user          │ Hermes + notify user   │ Hermes + notify
High       │ Require approve   │ Require approve        │ Escalate (wait)
Critical   │ Require approve   │ Block until return     │ Block until return
```

**Stakes definitions:**
- **Low:** cosmetic, reversible, low cost (e.g. reword commit message)
- **Medium:** functional change, moderate cost, reversible (e.g. add file)
- **High:** API change, schema change, dependency update
- **Critical:** auth, security, payment, data migration, production deploy

### 16.2 Notification routing

When Hermes acts while user is away:

| Stakes | Notification channel | Why |
|---|---|---|
| Low | Silent (logged only) | Don't spam user |
| Medium | In-app notification + email digest | User sees on return |
| High | Push notification (OS) + email | User should know soon |
| Critical | Push + email + SMS + wait | Don't act until user responds |

**Implementation:** Uses Modern Agent's existing notification system
(`needs_input`, `ready_to_merge` from STATUS.md) + extends with
`hermes_auto_decided` event for audit.

### 16.3 Policy configuration

```yaml
# .agent-orchestrator/policy.yaml
hermes_ceo:
  enabled: true

  # Per-stakes default action
  when_user_away:
    low: auto_decide
    medium: auto_decide_and_notify
    high: require_approval
    critical: block

  # Per-specialty overrides
  specialty_overrides:
    security:                      # Sec workers always escalate
    - require_approval
    payment:
    - block
    docs:                          # Docs workers always auto
    - auto_decide

  # Cost ceiling
  max_daily_cost_usd: 50.00
  cost_ceiling_action: downgrade_to_human_only

  # Notification channels
  notifications:
    push: true
    email: true
    email_digest: hourly
    sms: false                     # opt-in
```

### 16.4 Audit trail

Every Hermes auto-decision is logged:

```sql
CREATE TABLE hermes_decisions (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL,
    stakes          TEXT NOT NULL,        -- low|medium|high|critical
    action          TEXT NOT NULL,        -- what Hermes did
    reasoning       TEXT NOT NULL,        -- why
    cost_usd        REAL,
    user_away       INTEGER NOT NULL,     -- 1 if user was away
    created_at      INTEGER NOT NULL
);
```

User can review on return:
- "While you were away, I made 12 decisions. Click to review."

### 16.5 Escalation paths

When Hermes cannot decide (low confidence, conflicting signals):

```
Hermes cannot decide
   ↓
1. Auto-decide with audit log (if low stakes)
   ↓
2. Wait for user (if high stakes, with timeout)
   ↓
3. Defer to CEO policy (if user-defined)
   ↓
4. Spawn "review agent" to get second opinion
   ↓
5. Block until user returns
```

**Default:** Option 1 for low/medium, Option 2 for high, Option 5 for critical.

### 16.6 Hermes ↔ Modern Agent integration

Hermes is the **outer CEO** — user-facing decision maker.
Modern Agent's internal orchestrators (Holding/Company/Project HQ) are
**inner PMs** — tactical executors.

```
User → Hermes (outer CEO)
         │  "Should I refactor billing?"
         │
         ├── User present → ask user
         │
         └── User away → policy check
                ↓
         Modern Agent daemon (loopback)
                ↓
         Holding HQ orchestrator (inner CEO)
                ↓
         Company HQ / Project HQ (inner PM)
                ↓
         Worker pool (23 adapters + custom)
                ↓
         4-gate hybrid approval → merge
```

**Conflict resolution:** Hermes decides strategic ("should we?").
Inner PMs decide tactical ("how?"). Gate vetoes (agent + human) protect
execution quality. Hermes never bypasses gates.

---

## 17. Cross-references

- Implementation plan: `../plans/2026-07-14-hybrid-approval-gates.md`
- Backend mental model: `../../architecture.md`
- Backend package layout: `../../backend-code-structure.md`
- Live Terminals design (CEO/PM/Worker precedent): `../../superpowers/specs/2026-07-09-live-terminals-design.md`
- Design system: `../../../DESIGN.md`
- Tailwind theme: `../../../tailwind.theme.json`
- Tracker lane issue: modernagent/modern-agent#112
- Raw PR events: modernagent/modern-agent#110, #111
- CLI parity: docs/STATUS.md "In flight — CLI parity for PR/review actions"
- Conventions and hard rules: `../../AGENTS.md`