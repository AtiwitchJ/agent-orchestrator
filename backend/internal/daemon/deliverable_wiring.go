package daemon

// This file wires the deliverable watcher into daemon startup. The watcher
// confirms a docs-repo session's deliverable artifact exists and records the
// confirmation through the Lifecycle Manager so the session surfaces as
// StatusReportReady.

import (
	"context"
	"log/slog"

	"github.com/aoagents/agent-orchestrator/backend/internal/lifecycle"
	deliverableobserve "github.com/aoagents/agent-orchestrator/backend/internal/observe/deliverable"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

// startDeliverableObserver wires the deliverable watcher over the store and LCM.
// Disabled projects (no deliverable config) are filtered out inside the
// observer's Tick, so an empty fleet costs only one periodic list scan.
func startDeliverableObserver(ctx context.Context, store *sqlite.Store, lcm *lifecycle.Manager, logger *slog.Logger) <-chan struct{} {
	observer := deliverableobserve.New(lcm, store, store, store.DB(), deliverableobserve.Config{Logger: logger})
	return observer.Start(ctx)
}
