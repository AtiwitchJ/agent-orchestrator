package daemon

import (
	"context"
	"log/slog"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/observe"
	sessionsvc "github.com/modernagent/modern-agent/backend/internal/service/session"
	workboardsvc "github.com/modernagent/modern-agent/backend/internal/service/workboard"
	"github.com/modernagent/modern-agent/backend/internal/storage/sqlite"
)

const workboardDispatchInterval = time.Minute

// startWorkboardDispatcher periodically gives every active project a chance
// to promote due cards and claim ready work. DispatchOnce owns per-project
// serialization, so the loop stays a thin daemon lifecycle wrapper.
func startWorkboardDispatcher(ctx context.Context, store *sqlite.Store, sessions *sessionsvc.Service, logger *slog.Logger) <-chan struct{} {
	if logger == nil {
		logger = slog.Default()
	}
	dispatcher := workboardsvc.NewDispatcher(workboardsvc.DispatchDeps{Store: store, Spawner: sessions})
	return observe.StartPollLoop(ctx, workboardDispatchInterval, func(ctx context.Context) error {
		projects, err := store.ListProjects(ctx)
		if err != nil {
			return err
		}
		for _, project := range projects {
			if _, err := dispatcher.DispatchOnce(ctx, project.ID); err != nil {
				logger.Warn("workboard dispatcher: project dispatch failed", "project", project.ID, "err", err)
			}
		}
		return nil
	}, logger, "workboard dispatcher")
}
