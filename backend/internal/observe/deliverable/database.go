package deliverable

import (
	"context"
	"database/sql"
	"log/slog"
	"strconv"

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

	return w.evaluateCondition(rows, spec)
}

func (w *databaseWatcher) evaluateCondition(rows *sql.Rows, spec *domain.DatabaseSpec) (bool, error) {
	switch spec.Condition {
	case "exists":
		return rows.Next(), nil
	case "count_gt_0":
		count := 0
		for rows.Next() {
			count++
		}
		return count > 0, rows.Err()
	case "value_equals", "value_neq", "value_gt", "value_gte", "value_lt", "value_lte":
		if !rows.Next() {
			return false, rows.Err()
		}
		var actual interface{}
		if err := rows.Scan(&actual); err != nil {
			return false, err
		}
		return w.compareValues(actual, spec.Expected, spec.Condition)
	case "row_count_eq", "row_count_neq", "row_count_gt", "row_count_gte", "row_count_lt", "row_count_lte":
		rowCount := 0
		for rows.Next() {
			rowCount++
		}
		if err := rows.Err(); err != nil {
			return false, err
		}
		expected, err := w.parseNumber(spec.Expected)
		if err != nil {
			return false, err
		}
		return w.compareRowCount(rowCount, int(expected), spec.Condition)
	default:
		return false, nil
	}
}

func (w *databaseWatcher) parseNumber(v any) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case float32:
		return float64(n), nil
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case int32:
		return float64(n), nil
	case string:
		return strconv.ParseFloat(n, 64)
	default:
		return 0, nil
	}
}

func (w *databaseWatcher) compareValues(actual, expected any, condition string) (bool, error) {
	switch condition {
	case "value_equals":
		return w.equals(actual, expected), nil
	case "value_neq":
		return !w.equals(actual, expected), nil
	case "value_gt", "value_gte", "value_lt", "value_lte":
		actualNum, err := w.toFloat64(actual)
		if err != nil {
			return false, err
		}
		expectedNum, err := w.toFloat64(expected)
		if err != nil {
			return false, err
		}
		switch condition {
		case "value_gt":
			return actualNum > expectedNum, nil
		case "value_gte":
			return actualNum >= expectedNum, nil
		case "value_lt":
			return actualNum < expectedNum, nil
		case "value_lte":
			return actualNum <= expectedNum, nil
		}
	}
	return false, nil
}

func (w *databaseWatcher) equals(a, b any) bool {
	aNum, aErr := w.toFloat64(a)
	bNum, bErr := w.toFloat64(b)
	if aErr == nil && bErr == nil {
		return aNum == bNum
	}
	return a == b
}

func (w *databaseWatcher) toFloat64(v any) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case float32:
		return float64(n), nil
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case int32:
		return float64(n), nil
	case string:
		return strconv.ParseFloat(n, 64)
	default:
		return 0, nil
	}
}

func (w *databaseWatcher) compareRowCount(actual, expected int, condition string) (bool, error) {
	switch condition {
	case "row_count_eq":
		return actual == expected, nil
	case "row_count_neq":
		return actual != expected, nil
	case "row_count_gt":
		return actual > expected, nil
	case "row_count_gte":
		return actual >= expected, nil
	case "row_count_lt":
		return actual < expected, nil
	case "row_count_lte":
		return actual <= expected, nil
	}
	return false, nil
}
