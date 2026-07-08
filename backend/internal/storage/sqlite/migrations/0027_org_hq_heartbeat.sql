-- Org hierarchy: a project can be marked as the standing HQ for a company's
-- PM orchestrator ('company', requires company_id set) or for the holding's
-- CEO orchestrator ('holding', requires company_id empty — the holding is
-- implicit, one per daemon). Partial unique indexes enforce at most one HQ
-- per company and at most one holding HQ, among active (non-archived)
-- projects.
--
-- org_settings is a small named key/value table for org-wide state that must
-- survive a daemon restart — today just the heartbeat pause flag. It is
-- deliberately not a generic app-settings table.

-- +goose Up
-- +goose StatementBegin
ALTER TABLE projects ADD COLUMN hq_role TEXT;

CREATE UNIQUE INDEX idx_projects_company_hq ON projects(company_id)
    WHERE hq_role = 'company' AND archived_at IS NULL;

CREATE UNIQUE INDEX idx_projects_holding_hq ON projects(hq_role)
    WHERE hq_role = 'holding' AND archived_at IS NULL;

CREATE TABLE org_settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- SQLite cannot drop a column without rebuilding the table. As with 0022's
-- company_id, down migrations here are best-effort for dev databases: leave
-- hq_role in place and drop only what can be dropped cleanly.
DROP INDEX idx_projects_company_hq;
DROP INDEX idx_projects_holding_hq;
DROP TABLE org_settings;
-- +goose StatementEnd
