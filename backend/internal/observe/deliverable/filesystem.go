package deliverable

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type filesystemWatcher struct {
	logger *slog.Logger
}

func newFilesystemWatcher(logger *slog.Logger) *filesystemWatcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &filesystemWatcher{logger: logger}
}

func (w *filesystemWatcher) check(ctx context.Context, spec *domain.FilesystemSpec, worktreePath string) (bool, error) {
	if spec == nil || worktreePath == "" {
		return false, nil
	}
	matches, err := filepath.Glob(filepath.Join(worktreePath, spec.Glob))
	if err != nil {
		return false, err
	}
	if len(matches) > 0 {
		w.logger.Debug("deliverable: filesystem match",
			"glob", spec.Glob,
			"worktree", worktreePath,
			"matches", len(matches))
		return true, nil
	}
	return false, nil
}

func (w *filesystemWatcher) checkFileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
