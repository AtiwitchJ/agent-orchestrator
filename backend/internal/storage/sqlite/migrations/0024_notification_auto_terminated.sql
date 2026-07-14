-- notifications.type CHECK is rebuilt to add 'auto_terminated': the stalled-
-- session auto-kill monitor's audit notification (see observe/stallmon and
-- domain.NotificationAutoTerminated). SQLite cannot widen a CHECK in place;
-- unlike 0023's change_log rebuild, nothing triggers off of notifications, so
-- this is the plain rebuild: create a wider table, copy every row across,
-- drop the old table, rename, and recreate both pre-existing indexes verbatim
-- from 0011 (including the partial unique unread-dedupe index).

-- +goose Up
-- +goose StatementBegin
CREATE TABLE notifications_new (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    pr_url TEXT NOT NULL DEFAULT '',
    type TEXT NOT NULL CHECK (
        type IN (
            'needs_input',
            'ready_to_merge',
            'pr_merged',
            'pr_closed_unmerged',
            'auto_terminated'
        )
    ),
    title TEXT NOT NULL,
    body TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'unread' CHECK (status IN ('read', 'unread')),
    created_at TIMESTAMP NOT NULL
);

INSERT INTO notifications_new (id, session_id, project_id, pr_url, type, title, body, status, created_at)
SELECT id, session_id, project_id, pr_url, type, title, body, status, created_at
FROM notifications;

DROP INDEX IF EXISTS idx_notifications_unread_dedupe;
DROP INDEX IF EXISTS idx_notifications_status;
DROP TABLE notifications;
ALTER TABLE notifications_new RENAME TO notifications;

CREATE INDEX idx_notifications_status
    ON notifications(status, created_at DESC);

CREATE UNIQUE INDEX idx_notifications_unread_dedupe
    ON notifications(session_id, type, pr_url)
    WHERE status = 'unread';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE TABLE notifications_old (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    pr_url TEXT NOT NULL DEFAULT '',
    type TEXT NOT NULL CHECK (
        type IN (
            'needs_input',
            'ready_to_merge',
            'pr_merged',
            'pr_closed_unmerged'
        )
    ),
    title TEXT NOT NULL,
    body TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'unread' CHECK (status IN ('read', 'unread')),
    created_at TIMESTAMP NOT NULL
);

INSERT INTO notifications_old (id, session_id, project_id, pr_url, type, title, body, status, created_at)
SELECT id, session_id, project_id, pr_url, type, title, body, status, created_at
FROM notifications
WHERE type <> 'auto_terminated';

DROP INDEX IF EXISTS idx_notifications_unread_dedupe;
DROP INDEX IF EXISTS idx_notifications_status;
DROP TABLE notifications;
ALTER TABLE notifications_old RENAME TO notifications;

CREATE INDEX idx_notifications_status
    ON notifications(status, created_at DESC);

CREATE UNIQUE INDEX idx_notifications_unread_dedupe
    ON notifications(session_id, type, pr_url)
    WHERE status = 'unread';
-- +goose StatementEnd
