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

-- name: DeleteWorkCard :exec
DELETE FROM work_cards WHERE id = ?;

-- name: CountRunningCards :one
SELECT COUNT(*) FROM work_cards
WHERE project_id = ? AND status = 'running';

-- name: ClaimReadyWorkCard :execrows
UPDATE work_cards AS card
SET status = 'running', session_id = '', updated_at = sqlc.arg(updated_at)
WHERE card.id = sqlc.arg(card_id)
  AND card.project_id = sqlc.arg(project_id)
  AND card.status = 'ready'
  AND card.paused_retarget = 0
  AND (
    SELECT COUNT(*) FROM work_cards AS running_cards
    WHERE running_cards.project_id = sqlc.arg(project_id) AND running_cards.status = 'running'
  ) < sqlc.arg(wip_limit);

-- name: InsertWorkCardEvent :exec
INSERT INTO work_card_events (id, card_id, project_id, kind, payload, created_at)
VALUES (?, ?, ?, ?, ?, ?);
