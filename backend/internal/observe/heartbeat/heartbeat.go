// Package heartbeat implements the background loop that periodically nudges
// a company PM or the holding CEO orchestrator to wake up, check org status,
// and act where needed. It is deliberately nudge-only: it never spawns a
// session. If an HQ project's orchestrator has died, the heartbeat simply has
// nothing to nudge and stays silent until a human restarts it — the worst a
// bug here can do is send a redundant chat message to an idle orchestrator,
// never lose or duplicate real delegated work:
//
//   - The global kill switch (org_settings heartbeat_paused) is re-read every
//     tick and, when set, skips the entire pass before any project is even
//     enumerated.
//   - Only a project with HQRole set, heartbeat enabled in its config, and an
//     ACTIVE (non-terminated) orchestrator session is ever a send candidate.
//   - A nudge is sent only when that orchestrator is currently Idle.
//     WaitingInput (a question pending for the human) and Active (already
//     working) are both skipped, and the per-project interval clock is NOT
//     advanced on a skip — so a nudge fires on the first tick the orchestrator
//     is next idle, rather than waiting out a full interval on top of however
//     long it was busy or blocked.
//   - lastSent bookkeeping is in-memory only (per the durable-facts
//     principle); a daemon restart just restarts each project's interval
//     timer from zero, which is a late bias, never an early double-send.
package heartbeat

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
)

// DefaultTickInterval is the cadence used when Config.Tick is zero. This is
// the scan cadence only — how often eligible projects are re-checked — not
// the per-project nudge cadence, which comes from each HQ project's own
// heartbeat.interval config.
const DefaultTickInterval = 60 * time.Second

// Config holds the externally-tunable knobs for a Monitor. Every field is optional.
type Config struct {
	// Tick is the interval between scans. <=0 means DefaultTickInterval.
	Tick time.Duration
	// Clock supplies the "now" used for interval comparisons. nil means
	// time.Now. Injected in tests so assertions don't race wallclock.
	Clock func() time.Time
	// Logger receives operational diagnostics. nil means slog.Default.
	Logger *slog.Logger
}

// Store is the durable persistence surface the heartbeat loop needs.
type Store interface {
	ListProjects(ctx context.Context) ([]domain.ProjectRecord, error)
	ListSessions(ctx context.Context, project domain.ProjectID) ([]domain.SessionRecord, error)
	GetOrgSetting(ctx context.Context, key string) (string, bool, error)
}

// sender is the narrow surface the heartbeat loop needs to deliver a nudge.
// service/session's *Service satisfies it via its existing Send method — the
// same path `ao send` uses, so a heartbeat nudge is indistinguishable from any
// other agent-to-agent message.
type sender interface {
	Send(ctx context.Context, id domain.SessionID, message string, sender domain.SessionID) error
}

// Monitor is the heartbeat background loop. Construct it with New; start the
// background goroutine with Start, or drive a single cycle synchronously with
// Tick.
type Monitor struct {
	store  Store
	sender sender

	tick   time.Duration
	clock  func() time.Time
	logger *slog.Logger

	mu sync.Mutex
	// lastSent tracks, per HQ project, the last time a nudge was
	// successfully delivered. Absence means "never sent" (or forgotten after
	// the project vanished) and always makes a project immediately eligible.
	lastSent map[string]time.Time
}

// New constructs a Monitor. store supplies projects/sessions/settings;
// send delivers a nudge message to a session.
func New(store Store, send sender, cfg Config) *Monitor {
	m := &Monitor{
		store:    store,
		sender:   send,
		tick:     cfg.Tick,
		clock:    cfg.Clock,
		logger:   cfg.Logger,
		lastSent: map[string]time.Time{},
	}
	if m.tick <= 0 {
		m.tick = DefaultTickInterval
	}
	if m.clock == nil {
		m.clock = time.Now
	}
	if m.logger == nil {
		m.logger = slog.Default()
	}
	return m
}

// Start launches the background goroutine and returns a channel that closes
// once the loop has exited. The loop exits on ctx cancellation; the channel
// gives the daemon a clean shutdown hook.
func (m *Monitor) Start(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go m.loop(ctx, done)
	return done
}

func (m *Monitor) loop(ctx context.Context, done chan<- struct{}) {
	defer close(done)
	t := time.NewTicker(m.tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := m.Tick(ctx); err != nil {
				m.logger.Error("heartbeat: tick failed", "err", err)
			}
		}
	}
}

// Tick runs one scan cycle: re-reads the global pause setting, then evaluates
// every HQ project with heartbeat enabled. Tick is exported so the daemon
// (and tests) can drive cycles synchronously.
//
// Errors: only the global-setting read and the project-listing failure are
// propagated. Per-project evaluation never returns an error to the caller —
// a bad project or a failed send is logged and handled in place (see
// evaluate) so one bad project can never abort the rest of the cycle.
func (m *Monitor) Tick(ctx context.Context) error {
	paused, ok, err := m.store.GetOrgSetting(ctx, domain.OrgSettingHeartbeatPaused)
	if err != nil {
		// Fail closed: a storage hiccup reading the kill switch must never be
		// treated as "not paused" — skip the whole pass and retry next tick.
		return fmt.Errorf("read heartbeat pause setting: %w", err)
	}
	if ok && paused == "true" {
		m.logger.Debug("heartbeat: paused, skipping tick")
		return nil
	}

	projects, err := m.store.ListProjects(ctx)
	if err != nil {
		return fmt.Errorf("list projects: %w", err)
	}

	now := m.clock()
	for _, p := range projects {
		if p.HQRole == "" || !p.Config.Heartbeat.Enabled {
			continue
		}
		m.evaluate(ctx, p, now)
	}
	return nil
}

// evaluate runs the full decision for one HQ project: the interval gate, the
// active-orchestrator gate, and the idle gate, then sends the wake-up nudge.
func (m *Monitor) evaluate(ctx context.Context, project domain.ProjectRecord, now time.Time) {
	interval, err := time.ParseDuration(project.Config.Heartbeat.Interval)
	if err != nil {
		m.logger.Warn("heartbeat: invalid interval, skipping project", "project", project.ID, "interval", project.Config.Heartbeat.Interval, "err", err)
		return
	}
	if last, ok := m.getLastSent(project.ID); ok && now.Sub(last) < interval {
		return
	}

	sessions, err := m.store.ListSessions(ctx, domain.ProjectID(project.ID))
	if err != nil {
		m.logger.Warn("heartbeat: failed to list sessions, skipping project", "project", project.ID, "err", err)
		return
	}
	orchestrator, ok := activeOrchestrator(sessions)
	if !ok {
		// Nudge-only: no running orchestrator means nothing to nudge. Not an
		// error — a human simply hasn't started (or has stopped) this HQ's
		// orchestrator yet.
		return
	}
	if orchestrator.Activity.State != domain.ActivityIdle {
		// Busy (Active) or waiting on the human (WaitingInput): skip without
		// advancing lastSent, so the nudge fires on the first tick it is next
		// idle rather than waiting out a full interval on top of however long
		// it was busy or blocked.
		return
	}

	message := wakeUpMessage(project)
	if err := m.sender.Send(ctx, orchestrator.ID, message, ""); err != nil {
		m.logger.Warn("heartbeat: send failed, will retry next tick", "project", project.ID, "session", orchestrator.ID, "err", err)
		return
	}
	m.setLastSent(project.ID, now)
	m.logger.Info("heartbeat: sent wake-up nudge", "project", project.ID, "session", orchestrator.ID)
}

// activeOrchestrator returns the project's active (non-terminated)
// orchestrator session, if any.
func activeOrchestrator(sessions []domain.SessionRecord) (domain.SessionRecord, bool) {
	for _, s := range sessions {
		if s.Kind == domain.KindOrchestrator && !s.IsTerminated {
			return s, true
		}
	}
	return domain.SessionRecord{}, false
}

// wakeUpMessage builds the fixed wake-up nudge for an HQ project, naming its
// role (company PM vs holding CEO) so the orchestrator's heartbeat-protocol
// instructions (see session_manager's hqHeartbeatProtocol) have context.
func wakeUpMessage(project domain.ProjectRecord) string {
	role := "PM of company \"" + project.CompanyID + "\""
	if project.HQRole == domain.HQRoleHolding {
		role = "holding CEO"
	}
	return "[AO heartbeat] Wake up: you are the " + role + ". Run `ao org status` first, review current state and your in-flight delegations, and act only where something needs you — do not duplicate work already in progress. If nothing needs action, note it briefly and stand down."
}

func (m *Monitor) getLastSent(projectID string) (time.Time, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.lastSent[projectID]
	return t, ok
}

func (m *Monitor) setLastSent(projectID string, t time.Time) {
	m.mu.Lock()
	m.lastSent[projectID] = t
	m.mu.Unlock()
}
