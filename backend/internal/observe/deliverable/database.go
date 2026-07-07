package deliverable

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type databaseWatcher struct {
	logger *slog.Logger
	db     *sql.DB
}

func newDatabaseWatcher(logger *slog.Logger, db *sql.DB) *databaseWatcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &databaseWatcher{logger: logger, db: db}
}

func (w *databaseWatcher) check(ctx context.Context, spec *domain.DatabaseSpec) (bool, error) {
	if spec == nil {
		return false, nil
	}
	if w.db == nil {
		w.logger.Warn("deliverable: database watcher has no DB connection")
		return false, nil
	}

	rows, err := w.db.QueryContext(ctx, spec.Query)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	if !rows.Next() {
		switch spec.Condition {
		case "exists":
			return false, nil
		case "count_gt_0":
			return false, nil
		case "value_equals":
			return false, nil
		default:
			return false, nil
		}
	}

	switch spec.Condition {
	case "exists":
		w.logger.Debug("deliverable: database exists condition met")
		return true, nil
	case "count_gt_0":
		var count int
		if err := rows.Scan(&count); err != nil {
			return false, err
		}
		return count > 0, nil
	case "value_equals":
		var actual interface{}
		if err := rows.Scan(&actual); err != nil {
			return false, err
		}
		return actual == spec.Expected, nil
	default:
		return false, nil
	}
}
