-- Hybrid approval gates (design: docs/superpowers/specs/2026-07-14-hybrid-approval-gates-design.md).
-- Three durable tables back the policy engine + tracker observer:
--   policy_runs   : one row per tracker-spawned PR lifecycle.
--   gate_results  : one row per gate attempt (pass/fail/exhausted/overridden).
--   tracker_links : issue <-> session <-> (optional) PR link written by the
--                   tracker observer when a configured label is seen on an
--                   issue.
--
-- All three tables feed CDC events into the existing change_log via triggers,
-- reusing the established (project_id, session_id, event_type, payload,
-- created_at) shape. SQLite cannot widen the change_log CHECK in place, so
-- this migration rebuilds change_log exactly as 0023 did: drop every
-- dependent trigger, copy rows across with the new event_types admitted into
-- the CHECK, recreate the pre-existing triggers verbatim from 0023's current
-- body, then add the three new triggers. The Down reverses the same dance.
--
-- Three coarse-grained event_types are added -- policy_run_changed,
-- gate_result_recorded, tracker_link_changed. They are emitted on every
-- write so the CDC poller sees the lifecycle; finer-grained discriminator
-- fields (current_gate, gate_id, attempt, outcome) live in the JSON payload
-- and are interpreted by the consumer. This keeps the change_log CHECK small
-- and avoids a cascade of single-use event_types that would need rebuilding
-- again every time the policy engine gains a new transition.

-- +goose Up
-- +goose StatementBegin
CREATE TABLE policy_runs (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL,
    session_id      TEXT NOT NULL,
    pr_id           TEXT NOT NULL,
    config_snapshot TEXT NOT NULL,
    current_gate    TEXT NOT NULL,
    final_state     TEXT,
    started_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    UNIQUE(project_id, pr_id)
);

CREATE INDEX idx_policy_runs_session ON policy_runs(session_id);
CREATE INDEX idx_policy_runs_state   ON policy_runs(current_gate, updated_at);

CREATE TABLE gate_results (
    id            TEXT PRIMARY KEY,
    run_id        TEXT NOT NULL REFERENCES policy_runs(id) ON DELETE CASCADE,
    gate_id       TEXT NOT NULL,
    attempt       INTEGER NOT NULL,
    outcome       TEXT NOT NULL,
    reason        TEXT,
    second_vote   TEXT,
    justification TEXT,
    duration_ms   INTEGER,
    created_at    INTEGER NOT NULL
);

CREATE INDEX idx_gate_results_run ON gate_results(run_id, gate_id, attempt);

CREATE TABLE tracker_links (
    issue_id   TEXT NOT NULL,
    project_id TEXT NOT NULL,
    session_id TEXT NOT NULL,
    pr_id      TEXT,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (project_id, issue_id)
);

CREATE INDEX idx_tracker_links_session ON tracker_links(session_id);
-- +goose StatementEnd

-- +goose StatementBegin
-- Drop every trigger that references change_log before rebuilding it; SQLite
-- will refuse to drop a table while triggers on other tables still point at
-- it. The list mirrors the trigger names 0023 created and the policy/tracker
-- triggers this migration is about to add.
DROP TRIGGER IF EXISTS policy_runs_change_trg;
DROP TRIGGER IF EXISTS gate_results_change_trg;
DROP TRIGGER IF EXISTS tracker_links_change_trg;
DROP TRIGGER IF EXISTS pr_review_threads_cdc_update;
DROP TRIGGER IF EXISTS pr_review_threads_cdc_insert;
DROP TRIGGER IF EXISTS sessions_cdc_insert;
DROP TRIGGER IF EXISTS sessions_cdc_update;
DROP TRIGGER IF EXISTS pr_cdc_insert;
DROP TRIGGER IF EXISTS pr_cdc_update;
DROP TRIGGER IF EXISTS pr_session_cdc_update;
DROP TRIGGER IF EXISTS pr_checks_cdc_insert;
DROP TRIGGER IF EXISTS pr_checks_cdc_update;
DROP TRIGGER IF EXISTS session_messages_cdc_insert;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE change_log_new (
    seq        INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id TEXT NOT NULL REFERENCES projects (id),
    session_id TEXT REFERENCES sessions (id),
    event_type TEXT NOT NULL
        CHECK (event_type IN (
            'session_created',
            'session_updated',
            'pr_created',
            'pr_updated',
            'pr_check_recorded',
            'pr_session_changed',
            'pr_review_thread_added',
            'pr_review_thread_resolved',
            'session_message_created',
            'policy_run_changed',
            'gate_result_recorded',
            'tracker_link_changed'
        )),
    payload    TEXT NOT NULL CHECK (json_valid(payload)),
    created_at TIMESTAMP NOT NULL DEFAULT (datetime('now'))
);

INSERT INTO change_log_new (seq, project_id, session_id, event_type, payload, created_at)
SELECT seq, project_id, session_id, event_type, payload, created_at FROM change_log;

DROP INDEX IF EXISTS idx_change_log_project;
DROP TABLE change_log;
ALTER TABLE change_log_new RENAME TO change_log;
CREATE INDEX idx_change_log_project ON change_log (project_id, seq);
-- +goose StatementEnd

-- +goose StatementBegin
-- Recreate the 9 pre-existing triggers verbatim from 0023's current bodies.
CREATE TRIGGER pr_review_threads_cdc_insert
AFTER INSERT ON pr_review_threads
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (
        (SELECT s.project_id FROM pr p JOIN sessions s ON s.id = p.session_id WHERE p.url = NEW.pr_url),
        (SELECT session_id FROM pr WHERE url = NEW.pr_url),
        'pr_review_thread_added',
        json_object('pr', NEW.pr_url, 'thread', NEW.thread_id, 'path', NEW.path,
            'line', NEW.line,
            'resolved', json(CASE WHEN NEW.resolved THEN 'true' ELSE 'false' END),
            'isBot', json(CASE WHEN NEW.is_bot THEN 'true' ELSE 'false' END)),
        NEW.updated_at);
END;

CREATE TRIGGER pr_review_threads_cdc_update
AFTER UPDATE ON pr_review_threads
WHEN OLD.resolved <> NEW.resolved
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (
        (SELECT s.project_id FROM pr p JOIN sessions s ON s.id = p.session_id WHERE p.url = NEW.pr_url),
        (SELECT session_id FROM pr WHERE url = NEW.pr_url),
        'pr_review_thread_resolved',
        json_object('pr', NEW.pr_url, 'thread', NEW.thread_id, 'path', NEW.path,
            'line', NEW.line,
            'resolved', json(CASE WHEN NEW.resolved THEN 'true' ELSE 'false' END)),
        NEW.updated_at);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER sessions_cdc_insert
AFTER INSERT ON sessions
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (NEW.project_id, NEW.id, 'session_created',
        json_object('id', NEW.id, 'activity', NEW.activity_state,
            'isTerminated', json(CASE WHEN NEW.is_terminated THEN 'true' ELSE 'false' END)),
        NEW.updated_at);
END;

CREATE TRIGGER sessions_cdc_update
AFTER UPDATE ON sessions
WHEN OLD.activity_state <> NEW.activity_state
    OR OLD.is_terminated <> NEW.is_terminated
    OR (OLD.first_signal_at IS NULL AND NEW.first_signal_at IS NOT NULL)
    OR OLD.preview_url <> NEW.preview_url
    OR OLD.preview_revision <> NEW.preview_revision
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (NEW.project_id, NEW.id, 'session_updated',
        json_object('id', NEW.id, 'activity', NEW.activity_state,
            'isTerminated', json(CASE WHEN NEW.is_terminated THEN 'true' ELSE 'false' END),
            'previewUrl', NEW.preview_url, 'previewRevision', NEW.preview_revision),
        NEW.updated_at);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER pr_cdc_insert
AFTER INSERT ON pr
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES ((SELECT project_id FROM sessions WHERE id = NEW.session_id), NEW.session_id, 'pr_created',
        json_object('url', NEW.url, 'session', NEW.session_id, 'state', NEW.pr_state,
                    'ci', NEW.ci_state, 'review', NEW.review_decision, 'mergeability', NEW.mergeability),
        NEW.updated_at);
END;

CREATE TRIGGER pr_cdc_update
AFTER UPDATE ON pr
WHEN OLD.pr_state <> NEW.pr_state
    OR OLD.ci_state <> NEW.ci_state
    OR OLD.review_decision <> NEW.review_decision
    OR OLD.mergeability <> NEW.mergeability
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES ((SELECT project_id FROM sessions WHERE id = NEW.session_id), NEW.session_id, 'pr_updated',
        json_object('url', NEW.url, 'session', NEW.session_id, 'state', NEW.pr_state,
                    'ci', NEW.ci_state, 'review', NEW.review_decision, 'mergeability', NEW.mergeability),
        NEW.updated_at);
END;

CREATE TRIGGER pr_session_cdc_update
AFTER UPDATE ON pr
WHEN OLD.session_id <> NEW.session_id
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (
        (SELECT project_id FROM sessions WHERE id = NEW.session_id),
        NEW.session_id,
        'pr_session_changed',
        json_object('url', NEW.url,
            'fromSession', OLD.session_id,
            'toSession', NEW.session_id),
        NEW.updated_at);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER pr_checks_cdc_insert
AFTER INSERT ON pr_checks
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (
        (SELECT s.project_id FROM pr p JOIN sessions s ON s.id = p.session_id WHERE p.url = NEW.pr_url),
        (SELECT session_id FROM pr WHERE url = NEW.pr_url),
        'pr_check_recorded',
        json_object('pr', NEW.pr_url, 'name', NEW.name, 'commit', NEW.commit_hash, 'status', NEW.status),
        NEW.created_at);
END;

CREATE TRIGGER pr_checks_cdc_update
AFTER UPDATE ON pr_checks
WHEN OLD.status <> NEW.status
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (
        (SELECT s.project_id FROM pr p JOIN sessions s ON s.id = p.session_id WHERE p.url = NEW.pr_url),
        (SELECT session_id FROM pr WHERE url = NEW.pr_url),
        'pr_check_recorded',
        json_object('pr', NEW.pr_url, 'name', NEW.name, 'commit', NEW.commit_hash, 'status', NEW.status),
        datetime('now'));
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER session_messages_cdc_insert
AFTER INSERT ON session_messages
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (
        (SELECT project_id FROM sessions WHERE id = NEW.target_session_id),
        NEW.target_session_id,
        'session_message_created',
        json_object('id', NEW.id,
            'senderSessionId', NEW.sender_session_id,
            'targetSessionId', NEW.target_session_id),
        NEW.created_at);
END;
-- +goose StatementEnd

-- +goose StatementBegin
-- New triggers for the policy + tracker tables.
--
-- project_id and session_id are taken from the table's own columns where
-- present, since both policy_runs and tracker_links carry them directly; for
-- gate_results we resolve project_id from the parent run. The composite
-- row identifier for tracker_links (project_id, issue_id) is encoded as
-- `<projectId>:<issueId>` so consumers can correlate change_log rows with
-- tracker_links lookups without an extra round-trip.
CREATE TRIGGER policy_runs_insert_trg
AFTER INSERT ON policy_runs
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (
        NEW.project_id,
        NEW.session_id,
        'policy_run_changed',
        json_object(
            'id', NEW.id,
            'pr', NEW.pr_id,
            'currentGate', NEW.current_gate,
            'finalState', NEW.final_state,
            'op', TG_OP),
        NEW.updated_at);
END;

CREATE TRIGGER policy_runs_update_trg
AFTER UPDATE ON policy_runs
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (
        NEW.project_id,
        NEW.session_id,
        'policy_run_changed',
        json_object(
            'id', NEW.id,
            'pr', NEW.pr_id,
            'currentGate', NEW.current_gate,
            'finalState', NEW.final_state,
            'op', TG_OP),
        NEW.updated_at);
END;

CREATE TRIGGER policy_runs_delete_trg
AFTER DELETE ON policy_runs
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (
        OLD.project_id,
        OLD.session_id,
        'policy_run_changed',
        json_object(
            'id', OLD.id,
            'pr', OLD.pr_id,
            'currentGate', OLD.current_gate,
            'finalState', OLD.final_state,
            'op', TG_OP),
        COALESCE(OLD.updated_at, unixepoch()));
END;

CREATE TRIGGER gate_results_insert_trg
AFTER INSERT ON gate_results
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (
        (SELECT project_id FROM policy_runs WHERE id = NEW.run_id),
        (SELECT session_id FROM policy_runs WHERE id = NEW.run_id),
        'gate_result_recorded',
        json_object(
            'id', NEW.id,
            'runId', NEW.run_id,
            'gateId', NEW.gate_id,
            'attempt', NEW.attempt,
            'outcome', NEW.outcome,
            'op', TG_OP),
        NEW.created_at);
END;

CREATE TRIGGER gate_results_update_trg
AFTER UPDATE ON gate_results
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (
        (SELECT project_id FROM policy_runs WHERE id = NEW.run_id),
        (SELECT session_id FROM policy_runs WHERE id = NEW.run_id),
        'gate_result_recorded',
        json_object(
            'id', NEW.id,
            'runId', NEW.run_id,
            'gateId', NEW.gate_id,
            'attempt', NEW.attempt,
            'outcome', NEW.outcome,
            'op', TG_OP),
        NEW.created_at);
END;

CREATE TRIGGER gate_results_delete_trg
AFTER DELETE ON gate_results
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (
        (SELECT project_id FROM policy_runs WHERE id = OLD.run_id),
        (SELECT session_id FROM policy_runs WHERE id = OLD.run_id),
        'gate_result_recorded',
        json_object(
            'id', OLD.id,
            'runId', OLD.run_id,
            'gateId', OLD.gate_id,
            'attempt', OLD.attempt,
            'outcome', OLD.outcome,
            'op', TG_OP),
        COALESCE(OLD.created_at, unixepoch()));
END;

CREATE TRIGGER tracker_links_insert_trg
AFTER INSERT ON tracker_links
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (
        NEW.project_id,
        NEW.session_id,
        'tracker_link_changed',
        json_object(
            'row', NEW.project_id || ':' || NEW.issue_id,
            'issue', NEW.issue_id,
            'pr', NEW.pr_id,
            'op', TG_OP),
        NEW.created_at);
END;

CREATE TRIGGER tracker_links_update_trg
AFTER UPDATE ON tracker_links
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (
        NEW.project_id,
        NEW.session_id,
        'tracker_link_changed',
        json_object(
            'row', NEW.project_id || ':' || NEW.issue_id,
            'issue', NEW.issue_id,
            'pr', NEW.pr_id,
            'op', TG_OP),
        NEW.created_at);
END;

CREATE TRIGGER tracker_links_delete_trg
AFTER DELETE ON tracker_links
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (
        OLD.project_id,
        OLD.session_id,
        'tracker_link_changed',
        json_object(
            'row', OLD.project_id || ':' || OLD.issue_id,
            'issue', OLD.issue_id,
            'pr', OLD.pr_id,
            'op', TG_OP),
        COALESCE(OLD.created_at, unixepoch()));
END;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Reverse the change_log widening: drop the 9 new triggers (3 tables x 3
-- ops), drop every pre-existing trigger that references change_log, rebuild
-- the table without the 3 policy/tracker event_types, then recreate the
-- 9 pre-existing triggers verbatim.
DROP TRIGGER IF EXISTS tracker_links_delete_trg;
DROP TRIGGER IF EXISTS tracker_links_update_trg;
DROP TRIGGER IF EXISTS tracker_links_insert_trg;
DROP TRIGGER IF EXISTS gate_results_delete_trg;
DROP TRIGGER IF EXISTS gate_results_update_trg;
DROP TRIGGER IF EXISTS gate_results_insert_trg;
DROP TRIGGER IF EXISTS policy_runs_delete_trg;
DROP TRIGGER IF EXISTS policy_runs_update_trg;
DROP TRIGGER IF EXISTS policy_runs_insert_trg;
DROP TRIGGER IF EXISTS session_messages_cdc_insert;
DROP TRIGGER IF EXISTS pr_checks_cdc_update;
DROP TRIGGER IF EXISTS pr_checks_cdc_insert;
DROP TRIGGER IF EXISTS pr_session_cdc_update;
DROP TRIGGER IF EXISTS pr_cdc_update;
DROP TRIGGER IF EXISTS pr_cdc_insert;
DROP TRIGGER IF EXISTS sessions_cdc_update;
DROP TRIGGER IF EXISTS sessions_cdc_insert;
DROP TRIGGER IF EXISTS pr_review_threads_cdc_update;
DROP TRIGGER IF EXISTS pr_review_threads_cdc_insert;

CREATE TABLE change_log_old (
    seq        INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id TEXT NOT NULL REFERENCES projects (id),
    session_id TEXT REFERENCES sessions (id),
    event_type TEXT NOT NULL
        CHECK (event_type IN (
            'session_created',
            'session_updated',
            'pr_created',
            'pr_updated',
            'pr_check_recorded',
            'pr_session_changed',
            'pr_review_thread_added',
            'pr_review_thread_resolved',
            'session_message_created'
        )),
    payload    TEXT NOT NULL CHECK (json_valid(payload)),
    created_at TIMESTAMP NOT NULL DEFAULT (datetime('now'))
);

INSERT INTO change_log_old (seq, project_id, session_id, event_type, payload, created_at)
SELECT seq, project_id, session_id, event_type, payload, created_at
FROM change_log
WHERE event_type NOT IN ('policy_run_changed', 'gate_result_recorded', 'tracker_link_changed');

DROP INDEX IF EXISTS idx_change_log_project;
DROP TABLE change_log;
ALTER TABLE change_log_old RENAME TO change_log;
CREATE INDEX idx_change_log_project ON change_log (project_id, seq);

DROP TABLE IF EXISTS tracker_links;
DROP TABLE IF EXISTS gate_results;
DROP TABLE IF EXISTS policy_runs;
-- +goose StatementEnd

-- +goose StatementBegin
-- Recreate the 9 pre-existing triggers verbatim. (Identical to the Up block
-- but inside the Down half so the schema can be fully reverted.)
CREATE TRIGGER pr_review_threads_cdc_insert
AFTER INSERT ON pr_review_threads
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (
        (SELECT s.project_id FROM pr p JOIN sessions s ON s.id = p.session_id WHERE p.url = NEW.pr_url),
        (SELECT session_id FROM pr WHERE url = NEW.pr_url),
        'pr_review_thread_added',
        json_object('pr', NEW.pr_url, 'thread', NEW.thread_id, 'path', NEW.path,
            'line', NEW.line,
            'resolved', json(CASE WHEN NEW.resolved THEN 'true' ELSE 'false' END),
            'isBot', json(CASE WHEN NEW.is_bot THEN 'true' ELSE 'false' END)),
        NEW.updated_at);
END;

CREATE TRIGGER pr_review_threads_cdc_update
AFTER UPDATE ON pr_review_threads
WHEN OLD.resolved <> NEW.resolved
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (
        (SELECT s.project_id FROM pr p JOIN sessions s ON s.id = p.session_id WHERE p.url = NEW.pr_url),
        (SELECT session_id FROM pr WHERE url = NEW.pr_url),
        'pr_review_thread_resolved',
        json_object('pr', NEW.pr_url, 'thread', NEW.thread_id, 'path', NEW.path,
            'line', NEW.line,
            'resolved', json(CASE WHEN NEW.resolved THEN 'true' ELSE 'false' END)),
        NEW.updated_at);
END;

CREATE TRIGGER sessions_cdc_insert
AFTER INSERT ON sessions
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (NEW.project_id, NEW.id, 'session_created',
        json_object('id', NEW.id, 'activity', NEW.activity_state,
            'isTerminated', json(CASE WHEN NEW.is_terminated THEN 'true' ELSE 'false' END)),
        NEW.updated_at);
END;

CREATE TRIGGER sessions_cdc_update
AFTER UPDATE ON sessions
WHEN OLD.activity_state <> NEW.activity_state
    OR OLD.is_terminated <> NEW.is_terminated
    OR (OLD.first_signal_at IS NULL AND NEW.first_signal_at IS NOT NULL)
    OR OLD.preview_url <> NEW.preview_url
    OR OLD.preview_revision <> NEW.preview_revision
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (NEW.project_id, NEW.id, 'session_updated',
        json_object('id', NEW.id, 'activity', NEW.activity_state,
            'isTerminated', json(CASE WHEN NEW.is_terminated THEN 'true' ELSE 'false' END),
            'previewUrl', NEW.preview_url, 'previewRevision', NEW.preview_revision),
        NEW.updated_at);
END;

CREATE TRIGGER pr_cdc_insert
AFTER INSERT ON pr
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES ((SELECT project_id FROM sessions WHERE id = NEW.session_id), NEW.session_id, 'pr_created',
        json_object('url', NEW.url, 'session', NEW.session_id, 'state', NEW.pr_state,
                    'ci', NEW.ci_state, 'review', NEW.review_decision, 'mergeability', NEW.mergeability),
        NEW.updated_at);
END;

CREATE TRIGGER pr_cdc_update
AFTER UPDATE ON pr
WHEN OLD.pr_state <> NEW.pr_state
    OR OLD.ci_state <> NEW.ci_state
    OR OLD.review_decision <> NEW.review_decision
    OR OLD.mergeability <> NEW.mergeability
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES ((SELECT project_id FROM sessions WHERE id = NEW.session_id), NEW.session_id, 'pr_updated',
        json_object('url', NEW.url, 'session', NEW.session_id, 'state', NEW.pr_state,
                    'ci', NEW.ci_state, 'review', NEW.review_decision, 'mergeability', NEW.mergeability),
        NEW.updated_at);
END;

CREATE TRIGGER pr_session_cdc_update
AFTER UPDATE ON pr
WHEN OLD.session_id <> NEW.session_id
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (
        (SELECT project_id FROM sessions WHERE id = NEW.session_id),
        NEW.session_id,
        'pr_session_changed',
        json_object('url', NEW.url,
            'fromSession', OLD.session_id,
            'toSession', NEW.session_id),
        NEW.updated_at);
END;

CREATE TRIGGER pr_checks_cdc_insert
AFTER INSERT ON pr_checks
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (
        (SELECT s.project_id FROM pr p JOIN sessions s ON s.id = p.session_id WHERE p.url = NEW.pr_url),
        (SELECT session_id FROM pr WHERE url = NEW.pr_url),
        'pr_check_recorded',
        json_object('pr', NEW.pr_url, 'name', NEW.name, 'commit', NEW.commit_hash, 'status', NEW.status),
        NEW.created_at);
END;

CREATE TRIGGER pr_checks_cdc_update
AFTER UPDATE ON pr_checks
WHEN OLD.status <> NEW.status
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (
        (SELECT s.project_id FROM pr p JOIN sessions s ON s.id = p.session_id WHERE p.url = NEW.pr_url),
        (SELECT session_id FROM pr WHERE url = NEW.pr_url),
        'pr_check_recorded',
        json_object('pr', NEW.pr_url, 'name', NEW.name, 'commit', NEW.commit_hash, 'status', NEW.status),
        datetime('now'));
END;

CREATE TRIGGER session_messages_cdc_insert
AFTER INSERT ON session_messages
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (
        (SELECT project_id FROM sessions WHERE id = NEW.target_session_id),
        NEW.target_session_id,
        'session_message_created',
        json_object('id', NEW.id,
            'senderSessionId', NEW.sender_session_id,
            'targetSessionId', NEW.target_session_id),
        NEW.created_at);
END;
-- +goose StatementEnd
