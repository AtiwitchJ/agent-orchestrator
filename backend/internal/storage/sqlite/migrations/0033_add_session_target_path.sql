-- +goose Up
-- target_path records the optional source directory a worker launched from.
-- Defaulting to the empty string preserves existing sessions' worktree-root
-- restore behavior.
-- +goose StatementBegin
ALTER TABLE sessions ADD COLUMN target_path TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE sessions DROP COLUMN target_path;
-- +goose StatementEnd
