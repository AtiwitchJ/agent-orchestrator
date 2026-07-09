-- Widen the projects.kind CHECK to allow 'docs_repo'. domain.ProjectKindDocsRepo
-- has existed since the deliverable-watcher feature landed, but 0009's CHECK
-- constraint was never widened to admit it — every registration of a
-- docs-repo project (AsDocsRepo: true) has been failing at the DB layer with
-- "CHECK constraint failed: kind IN ('single_repo', 'workspace')" the whole
-- time, uncaught because no test exercised project.Add against the real
-- store with AsDocsRepo set. SQLite cannot ALTER a CHECK, so this surgically
-- rewrites the stored CREATE TABLE text in sqlite_master, mirroring 0007's
-- harness-widening technique. writable_schema edits must run outside a
-- transaction, and RESET forces an immediate schema reparse on the connection.

-- +goose NO TRANSACTION
-- +goose Up
-- +goose StatementBegin
PRAGMA writable_schema = ON;
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE sqlite_master
SET sql = replace(
    sql,
    'CHECK (kind IN (''single_repo'', ''workspace''))',
    'CHECK (kind IN (''single_repo'', ''workspace'', ''docs_repo''))'
)
WHERE type = 'table' AND name = 'projects';
-- +goose StatementEnd
-- +goose StatementBegin
PRAGMA writable_schema = RESET;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
PRAGMA writable_schema = ON;
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE sqlite_master
SET sql = replace(
    sql,
    'CHECK (kind IN (''single_repo'', ''workspace'', ''docs_repo''))',
    'CHECK (kind IN (''single_repo'', ''workspace''))'
)
WHERE type = 'table' AND name = 'projects';
-- +goose StatementEnd
-- +goose StatementBegin
PRAGMA writable_schema = RESET;
-- +goose StatementEnd
