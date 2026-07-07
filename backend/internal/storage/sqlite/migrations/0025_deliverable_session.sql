-- +goose Up
-- +goose StatementBegin
-- Adds deliverable_confirmed_at to sessions for docs-repo deliverable tracking.
-- SQLite supports ADD COLUMN with a nullable or default-bearing column.
ALTER TABLE sessions ADD COLUMN deliverable_confirmed_at TEXT;

-- Also widen the CDC update trigger to fire when deliverable_confirmed_at changes.
-- The original trigger fires on activity_state or is_terminated changes; now it
-- also fires when the deliverable watcher confirms the artifact.
DROP TRIGGER IF EXISTS sessions_cdc_update;
CREATE TRIGGER sessions_cdc_update
AFTER UPDATE ON sessions
WHEN OLD.activity_state <> NEW.activity_state
    OR OLD.is_terminated <> NEW.is_terminated
    OR OLD.deliverable_confirmed_at <> NEW.deliverable_confirmed_at
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (NEW.project_id, NEW.id, 'session_updated',
        json_object('id', NEW.id, 'activity', NEW.activity_state, 'isTerminated', json(CASE WHEN NEW.is_terminated THEN 'true' ELSE 'false' END)),
        NEW.updated_at);
END;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS sessions_cdc_update;
CREATE TRIGGER sessions_cdc_update
AFTER UPDATE ON sessions
WHEN OLD.activity_state <> NEW.activity_state
    OR OLD.is_terminated <> NEW.is_terminated
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (NEW.project_id, NEW.id, 'session_updated',
        json_object('id', NEW.id, 'activity', NEW.activity_state, 'isTerminated', json(CASE WHEN NEW.is_terminated THEN 'true' ELSE 'false' END)),
        NEW.updated_at);
END;
ALTER TABLE sessions DROP COLUMN deliverable_confirmed_at;
-- +goose StatementEnd
