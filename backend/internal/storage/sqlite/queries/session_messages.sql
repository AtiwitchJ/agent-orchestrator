-- name: InsertSessionMessage :exec
INSERT INTO session_messages (id, sender_session_id, target_session_id, content, created_at)
VALUES (?, ?, ?, ?, ?);

-- name: ListProjectSessionMessages :many
SELECT sm.id, sm.sender_session_id, sm.target_session_id, sm.content, sm.created_at
FROM session_messages sm
JOIN sessions s ON s.id = sm.target_session_id
WHERE s.project_id = ?
ORDER BY sm.created_at DESC
LIMIT ?;
