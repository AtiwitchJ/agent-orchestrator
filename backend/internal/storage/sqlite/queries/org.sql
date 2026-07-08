-- name: GetOrgSetting :one
SELECT value FROM org_settings WHERE key = ?;

-- name: SetOrgSetting :exec
INSERT INTO org_settings (key, value) VALUES (?, ?)
ON CONFLICT (key) DO UPDATE SET value = excluded.value;

-- name: SetProjectHQRole :execrows
UPDATE projects SET hq_role = ? WHERE id = ?;
