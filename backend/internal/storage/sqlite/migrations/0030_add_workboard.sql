-- +goose Up
-- +goose StatementBegin
CREATE TABLE work_cards (
    id                    TEXT PRIMARY KEY,
    project_id            TEXT NOT NULL,
    board_id              TEXT NOT NULL DEFAULT 'default',
    title                 TEXT NOT NULL,
    notes                 TEXT NOT NULL DEFAULT '',
    priority              TEXT NOT NULL,
    labels_json           TEXT NOT NULL DEFAULT '[]',
    status                TEXT NOT NULL,
    scheduled_at          INTEGER,
    ready_at              INTEGER,
    position              INTEGER NOT NULL DEFAULT 0,
    target_path           TEXT NOT NULL,
    repo_name             TEXT NOT NULL DEFAULT '',
    agent                 TEXT NOT NULL,
    session_id            TEXT NOT NULL DEFAULT '',
    waiting_for_input     INTEGER NOT NULL DEFAULT 0,
    paused_retarget       INTEGER NOT NULL DEFAULT 0,
    goal_version          INTEGER NOT NULL DEFAULT 1,
    superseded_by_card_id TEXT NOT NULL DEFAULT '',
    created_at            INTEGER NOT NULL,
    updated_at            INTEGER NOT NULL
);

CREATE INDEX idx_work_cards_project_status ON work_cards(project_id, status, priority, ready_at);
CREATE INDEX idx_work_cards_session ON work_cards(session_id);

CREATE TABLE work_card_events (
    id         TEXT PRIMARY KEY,
    card_id    TEXT NOT NULL REFERENCES work_cards(id) ON DELETE CASCADE,
    project_id TEXT NOT NULL,
    kind       TEXT NOT NULL,
    payload    TEXT NOT NULL DEFAULT '{}',
    created_at INTEGER NOT NULL
);

CREATE INDEX idx_work_card_events_card ON work_card_events(card_id, created_at);
-- +goose StatementEnd

-- +goose StatementBegin
DROP TRIGGER IF EXISTS work_cards_delete_trg;
DROP TRIGGER IF EXISTS work_cards_update_trg;
DROP TRIGGER IF EXISTS work_cards_insert_trg;
DROP TRIGGER IF EXISTS tracker_links_delete_trg;
DROP TRIGGER IF EXISTS tracker_links_update_trg;
DROP TRIGGER IF EXISTS tracker_links_insert_trg;
DROP TRIGGER IF EXISTS gate_results_delete_trg;
DROP TRIGGER IF EXISTS gate_results_update_trg;
DROP TRIGGER IF EXISTS gate_results_insert_trg;
DROP TRIGGER IF EXISTS policy_runs_delete_trg;
DROP TRIGGER IF EXISTS policy_runs_update_trg;
DROP TRIGGER IF EXISTS policy_runs_insert_trg;
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
            'tracker_link_changed',
            'work_card_changed'
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

CREATE TRIGGER work_cards_insert_trg
AFTER INSERT ON work_cards
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (
        NEW.project_id,
        NULLIF(NEW.session_id, ''),
        'work_card_changed',
        json_object('card_id', NEW.id, 'status', NEW.status, 'board_id', NEW.board_id,
            'waiting_for_input', NEW.waiting_for_input, 'paused_retarget', NEW.paused_retarget,
            'op', 'insert'),
        NEW.updated_at);
END;

CREATE TRIGGER work_cards_update_trg
AFTER UPDATE ON work_cards
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (
        NEW.project_id,
        NULLIF(NEW.session_id, ''),
        'work_card_changed',
        json_object('card_id', NEW.id, 'status', NEW.status, 'board_id', NEW.board_id,
            'waiting_for_input', NEW.waiting_for_input, 'paused_retarget', NEW.paused_retarget,
            'op', 'update'),
        NEW.updated_at);
END;

CREATE TRIGGER work_cards_delete_trg
AFTER DELETE ON work_cards
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (
        OLD.project_id,
        NULLIF(OLD.session_id, ''),
        'work_card_changed',
        json_object('card_id', OLD.id, 'status', OLD.status, 'board_id', OLD.board_id,
            'waiting_for_input', OLD.waiting_for_input, 'paused_retarget', OLD.paused_retarget,
            'op', 'delete'),
        COALESCE(OLD.updated_at, unixepoch()));
END;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS work_cards_delete_trg;
DROP TRIGGER IF EXISTS work_cards_update_trg;
DROP TRIGGER IF EXISTS work_cards_insert_trg;
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
            'session_message_created',
            'policy_run_changed',
            'gate_result_recorded',
            'tracker_link_changed'
        )),
    payload    TEXT NOT NULL CHECK (json_valid(payload)),
    created_at TIMESTAMP NOT NULL DEFAULT (datetime('now'))
);

INSERT INTO change_log_old (seq, project_id, session_id, event_type, payload, created_at)
SELECT seq, project_id, session_id, event_type, payload, created_at
FROM change_log
WHERE event_type <> 'work_card_changed';

DROP INDEX IF EXISTS idx_change_log_project;
DROP TABLE change_log;
ALTER TABLE change_log_old RENAME TO change_log;
CREATE INDEX idx_change_log_project ON change_log (project_id, seq);

DROP TABLE IF EXISTS work_card_events;
DROP TABLE IF EXISTS work_cards;
-- +goose StatementEnd

-- +goose StatementBegin
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

CREATE TRIGGER policy_runs_insert_trg
AFTER INSERT ON policy_runs
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (
        NEW.project_id,
        NEW.session_id,
        'policy_run_changed',
        json_object('id', NEW.id, 'pr', NEW.pr_id, 'currentGate', NEW.current_gate,
            'finalState', NEW.final_state, 'op', TG_OP),
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
        json_object('id', NEW.id, 'pr', NEW.pr_id, 'currentGate', NEW.current_gate,
            'finalState', NEW.final_state, 'op', TG_OP),
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
        json_object('id', OLD.id, 'pr', OLD.pr_id, 'currentGate', OLD.current_gate,
            'finalState', OLD.final_state, 'op', TG_OP),
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
        json_object('id', NEW.id, 'runId', NEW.run_id, 'gateId', NEW.gate_id,
            'attempt', NEW.attempt, 'outcome', NEW.outcome, 'op', TG_OP),
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
        json_object('id', NEW.id, 'runId', NEW.run_id, 'gateId', NEW.gate_id,
            'attempt', NEW.attempt, 'outcome', NEW.outcome, 'op', TG_OP),
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
        json_object('id', OLD.id, 'runId', OLD.run_id, 'gateId', OLD.gate_id,
            'attempt', OLD.attempt, 'outcome', OLD.outcome, 'op', TG_OP),
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
        json_object('row', NEW.project_id || ':' || NEW.issue_id, 'issue', NEW.issue_id,
            'pr', NEW.pr_id, 'op', TG_OP),
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
        json_object('row', NEW.project_id || ':' || NEW.issue_id, 'issue', NEW.issue_id,
            'pr', NEW.pr_id, 'op', TG_OP),
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
        json_object('row', OLD.project_id || ':' || OLD.issue_id, 'issue', OLD.issue_id,
            'pr', OLD.pr_id, 'op', TG_OP),
        COALESCE(OLD.created_at, unixepoch()));
END;
-- +goose StatementEnd
