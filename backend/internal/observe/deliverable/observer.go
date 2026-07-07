// Package deliverable implements the OBSERVE-layer watcher that confirms a
// docs-repo session's deliverable artifact exists. When confirmed, it calls the
// LCM to record the timestamp so the session surfaces as StatusReportReady.
//
// The package owns three watcher implementations: filesystem (fsnotify glob),
// database (polled SELECT), and webhook (polled HTTP GET). The Observer
// coordinates the right watcher per session based on project deliverable config.
package deliverable

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
)

const DefaultTickInterval = 30 * time.Second

type Config struct {
	Tick   time.Duration
	Clock  func() time.Time
	Logger *slog.Logger
}

type projectStore interface {
	GetProject(ctx context.Context, id string) (domain.ProjectRecord, bool, error)
}

type sessionStore interface {
	ListAllSessions(ctx context.Context) ([]domain.SessionRecord, error)
}

type deliverableSink interface {
	ApplyDeliverableConfirmed(ctx context.Context, id domain.SessionID, confirmedAt time.Time) error
}

// Observer is the deliverable watcher. It polls sessions and fires filesystem,
// database, or webhook watchers based on each project's deliverable config.
type Observer struct {
	sink       deliverableSink
	projects   projectStore
	sessions   sessionStore
	tick       time.Duration
	clock      func() time.Time
	logger     *slog.Logger
	fswatch    *filesystemWatcher
	dbWatcher  *databaseWatcher
	webWatcher *webhookWatcher
}

func New(sink deliverableSink, projects projectStore, sessions sessionStore, db *sql.DB, cfg Config) *Observer {
	o := &Observer{
		sink:     sink,
		projects: projects,
		sessions: sessions,
		tick:     cfg.Tick,
		clock:    cfg.Clock,
		logger:   cfg.Logger,
	}
	if o.tick <= 0 {
		o.tick = DefaultTickInterval
	}
	if o.clock == nil {
		o.clock = time.Now
	}
	if o.logger == nil {
		o.logger = slog.Default()
	}
	o.fswatch = newFilesystemWatcher(o.logger)
	o.dbWatcher = newDatabaseWatcher(o.logger, db)
	o.webWatcher = newWebhookWatcher(o.logger)
	return o
}

// Start launches the background goroutine and returns a channel that closes
// when the loop exits.
func (o *Observer) Start(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go o.loop(ctx, done)
	return done
}

func (o *Observer) loop(ctx context.Context, done chan<- struct{}) {
	defer close(done)
	t := time.NewTicker(o.tick)
	defer t.Stop()
	if err := o.Tick(ctx); err != nil {
		o.logger.Error("deliverable: initial tick failed", "err", err)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := o.Tick(ctx); err != nil {
				o.logger.Error("deliverable: tick failed", "err", err)
			}
		}
	}
}

func (o *Observer) Tick(ctx context.Context) error {
	sessions, err := o.sessions.ListAllSessions(ctx)
	if err != nil {
		return err
	}
	for _, sess := range sessions {
		if sess.IsTerminated {
			continue
		}
		if !sess.DeliverableConfirmedAt.IsZero() {
			continue
		}
		o.checkOne(ctx, sess)
	}
	return nil
}

func (o *Observer) checkOne(ctx context.Context, sess domain.SessionRecord) {
	cfg, worktreePath, ok := o.deliverableConfigForSession(ctx, sess)
	if !ok {
		return
	}

	confirmed, err := o.checkDeliverable(ctx, cfg, worktreePath)
	if err != nil {
		o.logger.Debug("deliverable: check failed", "session", sess.ID, "err", err)
		return
	}
	if !confirmed {
		return
	}

	confirmedAt := o.clock()
	if err := o.sink.ApplyDeliverableConfirmed(ctx, sess.ID, confirmedAt); err != nil {
		o.logger.Error("deliverable: ApplyDeliverableConfirmed failed",
			"session", sess.ID, "err", err)
		return
	}
	o.logger.Info("deliverable: confirmed", "session", sess.ID, "type", cfg.Type)
}

func (o *Observer) deliverableConfigForSession(ctx context.Context, sess domain.SessionRecord) (domain.DeliverableConfig, string, bool) {
	proj, ok, err := o.projects.GetProject(ctx, string(sess.ProjectID))
	if err != nil || !ok {
		return domain.DeliverableConfig{}, "", false
	}
	if proj.Kind.WithDefault() != domain.ProjectKindDocsRepo {
		return domain.DeliverableConfig{}, "", false
	}
	if proj.Config.Deliverable == nil {
		return domain.DeliverableConfig{}, "", false
	}
	return *proj.Config.Deliverable, sess.Metadata.WorkspacePath, true
}

func (o *Observer) checkDeliverable(ctx context.Context, cfg domain.DeliverableConfig, worktreePath string) (bool, error) {
	switch cfg.Type {
	case domain.DeliverableTypeFilesystem:
		return o.fswatch.check(ctx, cfg.Filesystem, worktreePath)
	case domain.DeliverableTypeDatabase:
		return o.dbWatcher.check(ctx, cfg.Database)
	case domain.DeliverableTypeWebhook:
		return o.webWatcher.check(ctx, cfg.Webhook)
	default:
		return false, nil
	}
}
