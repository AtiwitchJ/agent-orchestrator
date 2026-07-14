// Package policy implements the hybrid approval-gate engine that drives a
// tracker-spawned pull request from "PR opened" to "merge ready" via four
// sequential gates: CI, agent self-review, human approve, agent final-pass.
//
// The engine is opt-in per project (see Config.Enabled) and is
// transport-agnostic: it exposes the Engine interface and gate types so the
// loopback daemon can drive it in-process, and CLI commands / HTTP controllers
// can submit human decisions without owning the orchestration.
//
// Design references:
//   - docs/superpowers/specs/2026-07-14-hybrid-approval-gates-design.md §2–§5
//   - docs/superpowers/plans/2026-07-14-hybrid-approval-gates.md Tasks 3–4
//
// This package is the Phase 1 skeleton: types, interfaces, and a single
// compileable stub engine. Real gate logic, persistence, and CDC emission land
// in subsequent tasks; nothing here should be treated as production behavior
// until then.
package policy
