-- name: CreatePolicyRun :one
INSERT INTO policy_runs (
    id, project_id, session_id, pr_id, config_snapshot,
    current_gate, final_state, started_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, NULL, ?, ?)
RETURNING *;

-- name: GetPolicyRun :one
SELECT * FROM policy_runs WHERE id = ? LIMIT 1;

-- name: ListPolicyRunsBySession :many
SELECT * FROM policy_runs WHERE session_id = ? ORDER BY started_at DESC;

-- name: ListActivePolicyRuns :many
SELECT * FROM policy_runs
WHERE final_state IS NULL
  AND current_gate NOT IN ('done', 'stopped')
ORDER BY updated_at ASC;

-- name: UpdatePolicyRunGate :exec
UPDATE policy_runs SET current_gate = ?, updated_at = ? WHERE id = ?;

-- name: FinalizePolicyRun :exec
UPDATE policy_runs
SET final_state = ?, current_gate = 'done', updated_at = ?
WHERE id = ?;

-- name: RecordGateResult :one
INSERT INTO gate_results (
    id, run_id, gate_id, attempt, outcome,
    reason, second_vote, justification, duration_ms, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListGateResults :many
SELECT * FROM gate_results WHERE run_id = ? ORDER BY created_at ASC, attempt ASC;

-- name: CreateTrackerLink :one
INSERT INTO tracker_links (issue_id, project_id, session_id, pr_id, created_at)
VALUES (?, ?, ?, NULL, ?)
RETURNING *;

-- name: GetTrackerLinkByIssue :one
SELECT * FROM tracker_links WHERE project_id = ? AND issue_id = ? LIMIT 1;

-- name: GetTrackerLinkBySession :one
SELECT * FROM tracker_links WHERE session_id = ? ORDER BY created_at DESC LIMIT 1;

-- name: SetTrackerLinkPR :exec
UPDATE tracker_links SET pr_id = ? WHERE project_id = ? AND issue_id = ?;
