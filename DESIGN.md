---
version: alpha
name: Modern Agent
description: Local-first supervisor UI for parallel AI coding agents. Dark theme, information density tuned for maintainers monitoring multi-layer orchestration.
colors:
  # Surfaces
  bg: "#0F172A"
  bg-card: "#1E293B"
  bg-sidebar: "#0B1220"
  bg-hover: "#334155"
  border: "#334155"
  # Text
  fg: "#F1F5F9"
  fg-muted: "#94A3B8"
  fg-dim: "#64748B"
  on-accent: "#042F36"
  on-success: "#042F1E"
  # Accent (CEO/Cyan — strategic)
  accent: "#06B6D4"
  # Status (shared with worktrees/PTY dots)
  status-success: "#10B981"
  status-warning: "#F59E0B"
  status-failed: "#EF4444"
  status-idle: "#6B7280"
  # Gate colors (per-gate status in 4-gate hybrid approval flow)
  gate-ci: "#3B82F6"
  gate-review: "#A855F7"
  gate-human: "#F59E0B"
  gate-final: "#10B981"
  # Tier colors (PM dispatch policy: trusted / experimental / banned)
  tier-trusted: "#10B981"
  tier-experimental: "#F59E0B"
  tier-banned: "#EF4444"
  # Heatmap (capacity utilization)
  heat-low: "#10B981"
  heat-mid: "#10B981"
  heat-high: "#10B981"
  heat-peak: "#F59E0B"
typography:
  h1:
    fontFamily: Inter
    fontSize: 1.375rem
    fontWeight: 700
    lineHeight: 1.2
    letterSpacing: "-0.01em"
  h2:
    fontFamily: Inter
    fontSize: 1.125rem
    fontWeight: 600
    lineHeight: 1.3
  h3:
    fontFamily: Inter
    fontSize: 0.875rem
    fontWeight: 600
    textTransform: "uppercase"
    letterSpacing: "0.05em"
  body-md:
    fontFamily: Inter
    fontSize: 0.875rem
    lineHeight: 1.5
  body-sm:
    fontFamily: Inter
    fontSize: 0.75rem
    lineHeight: 1.4
    color: "{colors.fg-muted}"
  mono:
    fontFamily: "JetBrains Mono"
    fontSize: 0.75rem
    fontWeight: 400
  label-caps:
    fontFamily: Inter
    fontSize: 0.6875rem
    fontWeight: 600
    textTransform: "uppercase"
    letterSpacing: "0.05em"
    color: "{colors.fg-muted}"
rounded:
  sm: 4px
  md: 8px
  lg: 12px
  full: 9999px
spacing:
  xs: 4px
  sm: 8px
  md: 16px
  lg: 24px
  xl: 32px
  xxl: 48px
elevation:
  card: "0 1px 3px rgba(0,0,0,0.3)"
  modal: "0 8px 24px rgba(0,0,0,0.5)"
shapes:
  card-radius: "{rounded.lg}"
  button-radius: "{rounded.md}"
  badge-radius: "{rounded.full}"
components:
  # ── Surfaces ──
  card:
    backgroundColor: "{colors.bg-card}"
    textColor: "{colors.fg}"
    rounded: "{rounded.lg}"
    padding: "{spacing.lg}"
    border: "1px solid {colors.border}"
  sidebar:
    backgroundColor: "{colors.bg-sidebar}"
    textColor: "{colors.fg-muted}"
    width: 220px
    padding: "{spacing.md}"
    borderRight: "1px solid {colors.border}"
  # ── Buttons ──
  button-primary:
    backgroundColor: "{colors.accent}"
    textColor: "{colors.on-accent}"
    rounded: "{rounded.md}"
    padding: 8px 16px
    typography: "{typography.body-md}"
    fontWeight: 600
  button-primary-hover:
    backgroundColor: "{colors.accent}"
    textColor: "{colors.on-accent}"
    rounded: "{rounded.md}"
    padding: 8px 16px
    opacity: 0.9
  button-success:
    backgroundColor: "{colors.status-success}"
    textColor: "{colors.on-success}"
    rounded: "{rounded.md}"
    padding: 8px 16px
  button-danger:
    backgroundColor: "{colors.status-failed}"
    textColor: "{colors.bg}"
    rounded: "{rounded.md}"
    padding: 8px 16px
    fontWeight: 600
  button-secondary:
    backgroundColor: "{colors.bg-card}"
    textColor: "{colors.fg}"
    rounded: "{rounded.md}"
    padding: 8px 16px
    border: "1px solid {colors.border}"
  # ── Status indicators ──
  status-dot-success:
    backgroundColor: "{colors.status-success}"
    size: 8px
    rounded: "{rounded.full}"
  status-dot-warning:
    backgroundColor: "{colors.status-warning}"
    size: 8px
    rounded: "{rounded.full}"
  status-dot-failed:
    backgroundColor: "{colors.status-failed}"
    size: 8px
    rounded: "{rounded.full}"
  status-dot-idle:
    backgroundColor: "{colors.status-idle}"
    size: 8px
    rounded: "{rounded.full}"
  # ── Tier badges (worker trust) ──
  # Note: tinted backgrounds + colored text fail WCAG (1:1). Use solid
  # colored text on bg-card instead. The emoji prefix carries the
  # semantic weight; color is reinforcement, not the only signal.
  tier-badge-trusted:
    backgroundColor: "{colors.bg-card}"
    textColor: "{colors.tier-trusted}"
    rounded: "{rounded.full}"
    padding: "3px 10px"
    border: "1px solid {colors.tier-trusted}"
    typography: "{typography.label-caps}"
  tier-badge-experimental:
    backgroundColor: "{colors.bg-card}"
    textColor: "{colors.tier-experimental}"
    rounded: "{rounded.full}"
    padding: "3px 10px"
    border: "1px solid {colors.tier-experimental}"
    typography: "{typography.label-caps}"
  tier-badge-banned:
    backgroundColor: "{colors.bg-card}"
    textColor: "{colors.tier-banned}"
    rounded: "{rounded.full}"
    padding: "3px 10px"
    border: "1px solid {colors.tier-banned}"
    typography: "{typography.label-caps}"
    fontWeight: 700
  # ── Gate status bar (4-gate hybrid approval) ──
  gate-status-bar:
    display: flex
    gap: 2px
    height: 6px
    rounded: 3px
    overflow: hidden
  gate-segment-ci:
    backgroundColor: "{colors.gate-ci}"
  gate-segment-review:
    backgroundColor: "{colors.gate-review}"
  gate-segment-human:
    backgroundColor: "{colors.gate-human}"
  gate-segment-final:
    backgroundColor: "{colors.gate-final}"
  # ── Sidebar nav ──
  nav-item:
    textColor: "{colors.fg-muted}"
    padding: 10px 16px
    rounded: "{rounded.md}"
  nav-item-active:
    textColor: "{colors.fg}"
    backgroundColor: "{colors.bg-hover}"
    borderLeft: "3px solid {colors.accent}"
    padding: 10px 16px
  nav-badge:
    backgroundColor: "{colors.bg}"
    textColor: "{colors.fg-muted}"
    padding: "2px 8px"
    rounded: "{rounded.full}"
    typography: "{typography.body-sm}"
  nav-badge-urgent:
    backgroundColor: "{colors.status-failed}"
    textColor: "{colors.bg}"
    padding: "2px 8px"
    rounded: "{rounded.full}"
    fontWeight: 700
  # ── Insight callout ──
  # Solid border-left on bg-card passes WCAG. The accent stripe is
  # decorative; the text must be readable on its own.
  insight:
    backgroundColor: "{colors.bg-card}"
    textColor: "{colors.fg}"
    borderLeft: "3px solid {colors.accent}"
    rounded: 4px
    padding: 10px 14px
    typography: "{typography.body-md}"
  # ── Worker card (specialized subagent) ──
  worker-card:
    backgroundColor: "{colors.bg-card}"
    textColor: "{colors.fg}"
    rounded: "{rounded.lg}"
    padding: "{spacing.lg}"
    border: "1px solid {colors.border}"
  worker-card-busy:
    backgroundColor: "{colors.bg-card}"
    textColor: "{colors.fg}"
    rounded: "{rounded.lg}"
    padding: "{spacing.lg}"
    border: "1px solid {colors.status-warning}"
  # ── Progress bar (success rate) ──
  progress-bar-track:
    backgroundColor: "{colors.bg}"
    height: 6px
    rounded: 3px
    overflow: hidden
    width: 100px
  progress-bar-fill:
    backgroundColor: "{colors.status-success}"
    height: 100%
  # ── Heatmap (capacity) ──
  heat-cell-low:
    backgroundColor: "{colors.heat-low}"
    opacity: 0.4
    rounded: 2px
  heat-cell-mid:
    backgroundColor: "{colors.heat-mid}"
    opacity: 0.7
    rounded: 2px
  heat-cell-high:
    backgroundColor: "{colors.heat-high}"
    opacity: 1
    rounded: 2px
  heat-cell-peak:
    backgroundColor: "{colors.heat-peak}"
    opacity: 1
    rounded: 2px
---

## Overview

Modern Agent is a local-first supervisor for parallel AI coding agents.
The UI is designed for **maintainers who run multiple agents in parallel** —
they need to see strategic state (CEO view), tactical execution (PM view),
and worker telemetry (pool view) in one consistent visual language.

**Voice:** Technical, calm, dense. The interface treats the user as an
operator who wants signal, not decoration. Information density over
whitespace. Trust is shown explicitly through tier badges, success rates,
and cost — never hidden.

**Mood:** Dark theme reduces eye strain during long debugging sessions.
Cyan accent (`#06B6D4`) is the only "interaction" color — used sparingly
to preserve its signal. Status colors (green/amber/red) are reserved for
actual state, never decoration.

## Colors

### Surfaces
- **bg ({colors.bg}):** Page background. Deep slate.
- **bg-card ({colors.bg-card}):** Default card surface.
- **bg-sidebar ({colors.bg-sidebar}):** Sidebar background, slightly darker than page.
- **bg-hover ({colors.bg-hover}):** Hover/active state for nav and list rows.
- **border ({colors.border}):** Subtle 1px borders on cards and table rows.

### Text
- **fg ({colors.fg}):** Primary text — headlines, body, high-emphasis.
- **fg-muted ({colors.fg-muted}):** Secondary text — labels, metadata, dimmed rows.
- **fg-dim ({colors.fg-dim}):** Tertiary — disabled states, timestamps.
- **on-accent ({colors.on-accent}):** Text on cyan accent (dark teal for contrast).

### Accent
- **accent ({colors.accent}):** Cyan — the only "interaction driver" color.
  Used for primary buttons, active nav state, insights, and the brand mark.
  Reserved to preserve its signal: if everything is cyan, nothing is.

### Status (4-state, never more)
- **status-success ({colors.status-success}):** Idle workers, passed gates, trusted tiers.
- **status-warning ({colors.status-warning}):** Working/busy state, experimental tier, peak capacity.
- **status-failed ({colors.status-failed}):** Failed gates, banned tier, stuck issues.
- **status-idle ({colors.status-idle}):** Offline or no signal.

### Gate colors (4-gate hybrid approval flow)
- **gate-ci ({colors.gate-ci}):** Blue — Gate 1 (CI auto-fix).
- **gate-review ({colors.gate-review}):** Purple — Gate 2 (agent self-review).
- **gate-human ({colors.gate-human}):** Amber — Gate 3 (human approval).
- **gate-final ({colors.gate-final}):** Green — Gate 4 (agent final-pass).

### Tier colors (PM dispatch policy)
- **tier-trusted ({colors.tier-trusted}):** Auto-dispatch, no PM approval.
- **tier-experimental ({colors.tier-experimental}):** PM approval required.
- **tier-banned ({colors.tier-banned}):** Never dispatch.

## Typography

Inter for everything. Hierarchy comes from weight and size, not font family.
JetBrains Mono for IDs, worker names, and CDC event names (they need to be
copy-pasteable).

- **h1 (22px / 700):** Page titles ("Org Overview", "Project HQ").
- **h2 (18px / 600):** Card titles, section heads.
- **h3 (14px / 600, uppercase):** Sub-section labels (rare).
- **body-md (14px / 400):** Default body text, table cells, list items.
- **body-sm (12px / 400, muted):** Metadata, timestamps, project names.
- **mono (12px / 400):** IDs, worker names, CDC events.
- **label-caps (11px / 600, uppercase, tracked):** Card titles, tier badges, status labels.

## Layout

4px spacing baseline. Cards use `lg` (24px) padding internally. Section
gaps between cards use `lg` (24px). Page-level breathing room uses `xl`
(32px) main padding.

Sidebar is fixed at 220px. Main content fills the rest. Page width is
fluid — design assumes ≥1280px viewport (desktop supervisor use case).

## Elevation

Two shadow levels only. Cards default to no shadow (border-only). Modals
and floating popovers use `modal` shadow. No "lift on hover" effects.

## Shapes

- **sm (4px):** Interactive elements (buttons, inputs).
- **md (8px):** Cards, secondary surfaces.
- **lg (12px):** Worker cards, primary cards.
- **full:** Avatars, tier badges, status pills.

## Components

### Cards
`card` is the default surface for grouped content. No shadow. Border is
1px subtle. Worker cards use `worker-card` for the standard form,
`worker-card-busy` (warning border) when active.

### Buttons
`button-primary` (cyan) is the only high-emphasis action per screen.
`button-success` for approvals. `button-danger` for destructive actions
(cancel job, override veto). `button-secondary` for everything else.

### Status indicators
4 `status-dot-*` variants. Use sparingly — one dot per entity, never
decorative repetition. Pair with a label or context.

### Tier badges
Three `tier-badge-*` variants. Trust is **always visible** — workers
show their tier prominently. Experimental tiers include a ⚠ prefix to
draw the eye.

### Gate status bar
Linear 4-segment bar (CI → Review → Human → Final) showing pass/fail
per gate. Filled segments = passed, empty = pending, red overlay = failed.

### Sidebar nav
Active state uses left border (3px cyan) + bg-hover. Badges show live
counts: `[3 active]`, `[8/13]`, `[3 pending]` (red when urgent).

### Insight callout
Single `insight` component for all "system is telling you something"
moments (threshold met, rubber-stamp detected, cost spike).

## Do's and Don'ts

- **Do** use token references (`{colors.accent}`) in component definitions
  — never literal hex.
- **Do** show tier + success rate + cost on every worker card. Trust
  is earned through transparency.
- **Do** use `status-*` tokens for state, never decoration. A green dot
  always means "idle/passed/trusted".
- **Don't** introduce new colors. If the palette doesn't cover a case,
  add a token to this file first.
- **Don't** use multiple accent colors. The cyan is the only "click me"
  signal.
- **Don't** nest component variants. `button-primary-hover` is a sibling
  key, not a child of `button-primary`.
- **Don't** use `status-failed` for non-state indicators (no red hearts,
  no red "!" icons, no red error pages without a real failure).
- **Don't** use `gate-*` colors outside the 4-gate status bar. They
  carry semantic weight — reuse dilutes meaning.
- **Don't** auto-promote experimental tiers. Tier promotion is always
  human-initiated, with a justification recorded in the audit trail.

## Cross-references

- Implementation: `docs/superpowers/plans/2026-07-14-hybrid-approval-gates.md` Tasks 23-30
- Design context: `docs/superpowers/specs/2026-07-14-hybrid-approval-gates-design.md` §11-12
- HTML mockup (Org Overview): `/Users/up-mac/.hermes/cache/ui-mockup-org-overview.html`
