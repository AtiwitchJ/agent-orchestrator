// Package stallmon implements the background monitor that watches for
// "stalled" worker sessions — a session whose activity_state has claimed
// ActivityActive far longer than a configured threshold, with no fresh
// signal — and can auto-terminate them.
//
// This is the most safety-sensitive piece of the stall feature: a bug here
// kills real, in-progress agent work. Every decision path re-asserts its own
// safety gates rather than trusting a caller or an upstream computation, so a
// single mistake anywhere else in the stack cannot, by itself, cause an
// unwanted kill:
//
//   - Only domain.KindWorker sessions are ever candidates (domain.IsStalled
//     gates on this; Monitor re-asserts it again before every Kill call).
//   - Only domain.ActivityActive can be stalled; the sticky
//     ActivityWaitingInput state (see domain.ActivityState.IsSticky) can
//     never be, structurally, via domain.IsStalled.
//   - A session must be observed stalled on two CONSECUTIVE ticks before it
//     is ever killed — a single boundary reading (clock skew, a tick that
//     lands right at the threshold) never triggers an immediate kill.
//   - The audit notification is always sent BEFORE the kill, and a failed
//     notification never blocks the kill (logged, not fatal) — losing the
//     audit trail is bad, refusing to kill a genuinely stalled session over
//     a notification hiccup is worse.
//   - AutoKill=false disables killing entirely (log-only); the "stalled"
//     badge itself is derived independently in service/session's
//     deriveStatus and is unaffected by this flag.
//   - The terminal-output secondary signal (OutputPulse) is CONSERVATIVE
//     ONLY: fresh recorded output can prevent a kill decision, but its
//     absence never causes one — no data means "fall back to
//     activity-state-only", never "assume stalled".
package stallmon

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/ports"
)

// DefaultTickInterval is the cadence used when Config.Tick is zero.
const DefaultTickInterval = 30 * time.Second

// Config holds the externally-tunable knobs for a Monitor. Every field is
// optional except Threshold (a zero/negative Threshold makes
// domain.IsStalled always false, i.e. disables detection entirely — see its
// doc comment).
type Config struct {
	// Tick is the interval between ticks. <=0 means DefaultTickInterval.
	Tick time.Duration
	// Threshold is how long a worker session may claim ActivityActive
	// without a fresh signal before it is considered stalled. See
	// domain.IsStalled.
	Threshold time.Duration
	// AutoKill gates whether a session confirmed stalled on two consecutive
	// ticks is actually terminated. false means log-only: the monitor still
	// tracks and reports confirmed-stalled sessions, it just never calls
	// Kill. The "stalled" status badge is unaffected either way — it is
	// derived independently by service/session's deriveStatus.
	AutoKill bool
	// OutputPulse is the optional conservative secondary signal (see
	// terminal.OutputPulse). nil means the signal is simply unavailable and
	// every session falls back to activity-state-only logic.
	OutputPulse outputPulseSource
	// Clock supplies the "now" used for stall comparisons. nil means
	// time.Now. Injected in tests so assertions don't race wallclock.
	Clock func() time.Time
	// Logger receives operational diagnostics and the confirmed-stalled /
	// kill audit trail. nil means slog.Default.
	Logger *slog.Logger
}

type sessionSource interface {
	ListAllSessions(ctx context.Context) ([]domain.SessionRecord, error)
}

// sessionKiller is the narrow surface stallmon needs to terminate a session.
// service/session's *Service satisfies it via its existing Kill method — the
// full teardown path (runtime + workspace) is reused verbatim; stallmon never
// reimplements termination.
type sessionKiller interface {
	Kill(ctx context.Context, id domain.SessionID) (bool, error)
}

// auditNotifier is the narrow surface stallmon needs to record the auto-kill
// audit trail. notify.Manager satisfies it via its existing Notify method.
type auditNotifier interface {
	Notify(ctx context.Context, intent ports.NotificationIntent) error
}

// outputPulseSource is the small port the conservative terminal-output
// secondary signal is consumed through. *terminal.OutputPulse satisfies it
// structurally; stallmon does not import the terminal package so as not to
// couple this safety-sensitive package to terminal internals.
type outputPulseSource interface {
	LastOutputAt(id string) (time.Time, bool)
}

// Monitor is the stall-detection-and-kill background loop. Construct it with
// New; start the background goroutine with Start, or drive a single cycle
// synchronously with Tick.
type Monitor struct {
	sessions sessionSource
	killer   sessionKiller
	notifier auditNotifier
	pulse    outputPulseSource

	tick      time.Duration
	threshold time.Duration
	autoKill  bool
	clock     func() time.Time
	logger    *slog.Logger

	mu sync.Mutex
	// pending tracks, per session, the time it was FIRST observed stalled.
	// Presence in this map means "seen stalled on the immediately-prior
	// tick"; a session absent from it that is observed stalled this tick is
	// on its first (unconfirmed) observation and is never killed yet. This is
	// in-memory-only bookkeeping — never persisted — per the durable-facts
	// principle: "stalled" itself is always recomputed at read time.
	pending map[domain.SessionID]time.Time
}

// New constructs a Monitor. sessions supplies the rows to scan; killer
// terminates a confirmed-stalled session; notifier records the audit trail
// before each kill attempt.
func New(sessions sessionSource, killer sessionKiller, notifier auditNotifier, cfg Config) *Monitor {
	m := &Monitor{
		sessions:  sessions,
		killer:    killer,
		notifier:  notifier,
		pulse:     cfg.OutputPulse,
		tick:      cfg.Tick,
		threshold: cfg.Threshold,
		autoKill:  cfg.AutoKill,
		clock:     cfg.Clock,
		logger:    cfg.Logger,
		pending:   map[domain.SessionID]time.Time{},
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
				m.logger.Error("stallmon: tick failed", "err", err)
			}
		}
	}
}

// Tick runs one observation cycle: it enumerates every session, evaluates
// each one for stall confirmation, and (subject to AutoKill) terminates any
// session confirmed stalled on two consecutive ticks.
//
// Tick is exported so the daemon (and tests) can drive cycles synchronously.
//
// Errors: only the session-listing failure is propagated. Per-session
// evaluation never returns an error to the caller — Kill/Notify failures are
// logged and handled in place (see evaluate) so one bad session can never
// abort the rest of the cycle.
func (m *Monitor) Tick(ctx context.Context) error {
	now := m.clock()

	sessions, err := m.sessions.ListAllSessions(ctx)
	if err != nil {
		return err
	}

	seen := make(map[domain.SessionID]struct{}, len(sessions))
	for _, rec := range sessions {
		seen[rec.ID] = struct{}{}
		m.evaluate(ctx, rec, now)
	}
	m.forgetVanished(seen)
	return nil
}

// evaluate runs the full decision for one session: compute stalled (with the
// conservative terminal-output override applied), track the two-tick
// confirmation, and act on a confirmed-stalled worker.
func (m *Monitor) evaluate(ctx context.Context, rec domain.SessionRecord, now time.Time) {
	// Structural safety gate, independent of domain.IsStalled: this loop may
	// never carry a non-worker or already-terminated session forward into
	// pending/kill logic, even if IsStalled itself had a bug. A session that
	// leaves the stalled set for either reason also has its confirmation
	// state cleared ("recovery clears pending state").
	if rec.Kind != domain.KindWorker || rec.IsTerminated {
		m.clearPending(rec.ID)
		return
	}

	stalled := domain.IsStalled(rec, now, m.threshold)
	if stalled && m.pulse != nil {
		// Conservative-only: fresh recorded PTY output despite a stale/stuck
		// activity_state can PREVENT this session from being treated as
		// stalled. It can never do the reverse — absence of data (ok=false)
		// or stale data (older than threshold) both fall through to the
		// activity-only "stalled" verdict computed above.
		if last, ok := m.pulse.LastOutputAt(rec.Metadata.RuntimeHandleID); ok && now.Sub(last) <= m.threshold {
			stalled = false
		}
	}

	if !stalled {
		m.clearPending(rec.ID)
		return
	}

	firstSeen, confirmed := m.markPending(rec.ID, now)
	if !confirmed {
		m.logger.Warn("stallmon: session observed stalled, awaiting second consecutive tick before acting",
			"session", rec.ID, "lastActivityAt", rec.Activity.LastActivityAt, "threshold", m.threshold)
		return
	}

	m.logger.Warn("stallmon: session confirmed stalled on second consecutive tick",
		"session", rec.ID, "firstObservedStalledAt", firstSeen, "autoKill", m.autoKill)

	if !m.autoKill {
		return
	}
	m.killStalled(ctx, rec)
}

// killStalled notifies (best-effort) then kills a confirmed-stalled worker
// session. Notify runs FIRST and its failure is only logged: the audit trail
// is important but must never block a genuine kill. A Kill failure leaves
// the session's pending entry in place so the next tick retries immediately
// (no fresh two-tick confirmation is required for a retry — it was already
// confirmed).
func (m *Monitor) killStalled(ctx context.Context, rec domain.SessionRecord) {
	// Defense in depth: re-assert the structural gate immediately before the
	// side-effecting call, so a future refactor of domain.IsStalled or of
	// evaluate's early-return cannot silently open a path to killing a
	// non-worker session.
	if rec.Kind != domain.KindWorker {
		m.logger.Error("stallmon: refusing to kill non-worker session (unreachable, safety net tripped)",
			"session", rec.ID, "kind", rec.Kind)
		m.clearPending(rec.ID)
		return
	}

	intent := ports.NotificationIntent{
		Type:               domain.NotificationAutoTerminated,
		SessionID:          rec.ID,
		ProjectID:          rec.ProjectID,
		SessionDisplayName: rec.DisplayName,
		CreatedAt:          m.clock().UTC(),
	}
	if err := m.notifier.Notify(ctx, intent); err != nil {
		m.logger.Error("stallmon: audit notification failed; killing anyway", "session", rec.ID, "err", err)
	}

	if _, err := m.killer.Kill(ctx, rec.ID); err != nil {
		m.logger.Error("stallmon: kill failed, will retry next tick", "session", rec.ID, "err", err)
		return
	}

	m.logger.Warn("stallmon: killed stalled session", "session", rec.ID)
	m.clearPending(rec.ID)
}

// markPending records the first-observed-stalled time for id if absent, and
// reports whether id was already pending (i.e. this is its second-or-later
// consecutive stalled tick).
func (m *Monitor) markPending(id domain.SessionID, now time.Time) (firstSeen time.Time, confirmed bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if first, ok := m.pending[id]; ok {
		return first, true
	}
	m.pending[id] = now
	return now, false
}

func (m *Monitor) clearPending(id domain.SessionID) {
	m.mu.Lock()
	delete(m.pending, id)
	m.mu.Unlock()
}

// forgetVanished drops pending entries for sessions no longer returned by
// ListAllSessions at all (deleted rows), so the in-memory map cannot grow
// without bound.
func (m *Monitor) forgetVanished(seen map[domain.SessionID]struct{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id := range m.pending {
		if _, ok := seen[id]; !ok {
			delete(m.pending, id)
		}
	}
}
