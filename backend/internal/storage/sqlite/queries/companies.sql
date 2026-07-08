-- name: InsertCompany :exec
INSERT INTO companies (id, name, created_at) VALUES (?, ?, ?);


-- name: ListCompanies :many
SELECT id, name, created_at FROM companies ORDER BY name;

-- name: GetCompany :one
SELECT id, name, created_at FROM companies WHERE id = ?;

-- name: SetProjectCompany :execrows
UPDATE projects SET company_id = ? WHERE id = ?;

-- name: DeleteCompany :execrows
DELETE FROM companies WHERE id = ?;