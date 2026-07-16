-- +goose Up
-- Work cards store audit times as Unix milliseconds, while change_log.created_at
-- is a TIMESTAMP scanned by sqlc into time.Time. Convert at the CDC boundary.
-- +goose StatementBegin
DROP TRIGGER IF EXISTS work_cards_delete_trg;
DROP TRIGGER IF EXISTS work_cards_update_trg;
DROP TRIGGER IF EXISTS work_cards_insert_trg;

UPDATE change_log
SET created_at = datetime(created_at / 1000.0, 'unixepoch')
WHERE event_type = 'work_card_changed' AND typeof(created_at) = 'integer';

CREATE TRIGGER work_cards_insert_trg
AFTER INSERT ON work_cards
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (
        NEW.project_id,
        NULLIF(NEW.session_id, ''),
        'work_card_changed',
        json_object('card_id', NEW.id, 'project_id', NEW.project_id, 'status', NEW.status,
            'board_id', NEW.board_id, 'waiting_for_input', NEW.waiting_for_input,
            'paused_retarget', NEW.paused_retarget, 'op', 'insert'),
        datetime(NEW.updated_at / 1000.0, 'unixepoch'));
END;

CREATE TRIGGER work_cards_update_trg
AFTER UPDATE ON work_cards
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (
        NEW.project_id,
        NULLIF(NEW.session_id, ''),
        'work_card_changed',
        json_object('card_id', NEW.id, 'project_id', NEW.project_id, 'status', NEW.status,
            'board_id', NEW.board_id, 'waiting_for_input', NEW.waiting_for_input,
            'paused_retarget', NEW.paused_retarget, 'op', 'update'),
        datetime(NEW.updated_at / 1000.0, 'unixepoch'));
END;

CREATE TRIGGER work_cards_delete_trg
AFTER DELETE ON work_cards
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (
        OLD.project_id,
        NULLIF(OLD.session_id, ''),
        'work_card_changed',
        json_object('card_id', OLD.id, 'project_id', OLD.project_id, 'status', OLD.status,
            'board_id', OLD.board_id, 'waiting_for_input', OLD.waiting_for_input,
            'paused_retarget', OLD.paused_retarget, 'op', 'delete'),
        datetime(OLD.updated_at / 1000.0, 'unixepoch'));
END;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS work_cards_delete_trg;
DROP TRIGGER IF EXISTS work_cards_update_trg;
DROP TRIGGER IF EXISTS work_cards_insert_trg;

CREATE TRIGGER work_cards_insert_trg
AFTER INSERT ON work_cards
BEGIN
    INSERT INTO change_log (project_id, session_id, event_type, payload, created_at)
    VALUES (
        NEW.project_id,
        NULLIF(NEW.session_id, ''),
        'work_card_changed',
        json_object('card_id', NEW.id, 'project_id', NEW.project_id, 'status', NEW.status,
            'board_id', NEW.board_id, 'waiting_for_input', NEW.waiting_for_input,
            'paused_retarget', NEW.paused_retarget, 'op', 'insert'),
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
        json_object('card_id', NEW.id, 'project_id', NEW.project_id, 'status', NEW.status,
            'board_id', NEW.board_id, 'waiting_for_input', NEW.waiting_for_input,
            'paused_retarget', NEW.paused_retarget, 'op', 'update'),
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
        json_object('card_id', OLD.id, 'project_id', OLD.project_id, 'status', OLD.status,
            'board_id', OLD.board_id, 'waiting_for_input', OLD.waiting_for_input,
            'paused_retarget', OLD.paused_retarget, 'op', 'delete'),
        OLD.updated_at);
END;
-- +goose StatementEnd
