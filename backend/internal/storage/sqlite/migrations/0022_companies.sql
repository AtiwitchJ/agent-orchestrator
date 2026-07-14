-- Company grouping: multiple git-repo projects can be grouped under a single
-- company (e.g. an org with several product repos). companies is a plain
-- registry table (id is the slug, mirroring projects.id) with no CDC — projects
-- itself has none either, so company assignment is not fanned out over CDC.

-- +goose Up
-- +goose StatementBegin
CREATE TABLE companies (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL
);

ALTER TABLE projects ADD COLUMN company_id TEXT REFERENCES companies(id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- SQLite cannot drop projects.company_id (it carries a foreign key
-- constraint) without rebuilding the table. As with 0009's `kind` column,
-- down migrations here are best-effort for dev databases: leave the
-- backward-compatible column in place and just drop the new table.
DROP TABLE companies;
-- +goose StatementEnd
