package stallmon

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/ports"
)

var ctx = context.Background()

const testThreshold = 4 * time.Minute

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// fakeClock is a mutable, injectable clock so tests can advance "now"
// between ticks deterministically.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock(t time.Time) *fakeClock { return &fakeClock{now: t} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

type fakeSessions struct {
	mu   sync.Mutex
	rows map[domain.SessionID]domain.SessionRecord
}

func newFakeSessions(rows ...domain.SessionRecord) *fakeSessions {
	s := &fakeSessions{rows: map[domain.SessionID]domain.SessionRecord{}}
	for _, r := range rows {
		s.rows[r.ID] = r
	}
	return s
}

func (s *fakeSessions) ListAllSessions(context.Context) ([]domain.SessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.SessionRecord, 0, len(s.rows))
	for _, r := range s.rows {
		out = append(out, r)
	}
	return out, nil
}

func (s *fakeSessions) set(rec domain.SessionRecord) {
	s.mu.Lock()
	s.rows[rec.ID] = rec
	s.mu.Unlock()
}

func (s *fakeSessions) remove(id domain.SessionID) {
	s.mu.Lock()
	delete(s.rows, id)
	s.mu.Unlock()
}

// recorder is a shared call-order log so tests can assert notify-before-kill
// ordering across the two fakes below.
type recorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *recorder) add(s string) {
	r.mu.Lock()
	r.calls = append(r.calls, s)
	r.mu.Unlock()
}

func (r *recorder) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.calls))
	copy(out, r.calls)
	return out
}

type fakeKiller struct {
	rec      *recorder
	mu       sync.Mutex
	killed   []domain.SessionID
	failNext bool
	err      error
}

func (k *fakeKiller) Kill(_ context.Context, id domain.SessionID) (bool, error) {
	if k.rec != nil {
		k.rec.add("kill:" + string(id))
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.failNext {
		k.failNext = false
		if k.err == nil {
			k.err = errors.New("kill failed")
		}
		return false, k.err
	}
	k.killed = append(k.killed, id)
	return true, nil
}

func (k *fakeKiller) killedIDs() []domain.SessionID {
	k.mu.Lock()
	defer k.mu.Unlock()
	out := make([]domain.SessionID, len(k.killed))
	copy(out, k.killed)
	return out
}

type fakeNotifier struct {
	rec     *recorder
	mu      sync.Mutex
	intents []ports.NotificationIntent
	err     error
}

func (n *fakeNotifier) Notify(_ context.Context, intent ports.NotificationIntent) error {
	if n.rec != nil {
		n.rec.add("notify:" + string(intent.SessionID))
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.intents = append(n.intents, intent)
	return n.err
}

func (n *fakeNotifier) notified() []ports.NotificationIntent {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]ports.NotificationIntent, len(n.intents))
	copy(out, n.intents)
	return out
}

type fakePulse struct {
	mu   sync.Mutex
	data map[string]time.Time
}

func newFakePulse() *fakePulse { return &fakePulse{data: map[string]time.Time{}} }

func (p *fakePulse) LastOutputAt(id string) (time.Time, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	t, ok := p.data[id]
	return t, ok
}

func (p *fakePulse) set(id string, t time.Time) {
	p.mu.Lock()
	p.data[id] = t
	p.mu.Unlock()
}

// stalledWorker builds a worker session whose activity_state has claimed
// "active" starting at t0, i.e. it becomes stalled once now-t0 > threshold.
func stalledWorker(id domain.SessionID, t0 time.Time) domain.SessionRecord {
	return domain.SessionRecord{
		ID:       id,
		Kind:     domain.KindWorker,
		Activity: domain.Activity{State: domain.ActivityActive, LastActivityAt: t0},
	}
}

func newMonitor(sessions sessionSource, killer sessionKiller, notifier auditNotifier, clock *fakeClock, autoKill bool, pulse outputPulseSource) *Monitor {
	return New(sessions, killer, notifier, Config{
		Threshold:   testThreshold,
		AutoKill:    autoKill,
		Clock:       clock.Now,
		Logger:      quietLogger(),
		OutputPulse: pulse,
	})
}

// TestTick_NoKillOnFirstStalledTick pins the two-tick confirmation rule: a
// session observed stalled for the very first time must never be killed
// immediately, guarding against a single boundary reading (clock skew, a
// tick landing right at the threshold).
func TestTick_NoKillOnFirstStalledTick(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	stalledSince := clock.Now().Add(-2 * testThreshold)
	sessions := newFakeSessions(stalledWorker("s1", stalledSince))
	killer := &fakeKiller{}
	notifier := &fakeNotifier{}
	m := newMonitor(sessions, killer, notifier, clock, true, nil)

	if err := m.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(killer.killedIDs()) != 0 {
		t.Fatalf("first stalled tick must not kill, killed=%v", killer.killedIDs())
	}
	if len(notifier.notified()) != 0 {
		t.Fatalf("first stalled tick must not notify, notified=%v", notifier.notified())
	}
}

// TestTick_NotifyThenKillOnSecondConsecutiveTick pins both the two-tick
// confirmation AND the notify-before-kill ordering.
func TestTick_NotifyThenKillOnSecondConsecutiveTick(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	stalledSince := clock.Now().Add(-2 * testThreshold)
	sessions := newFakeSessions(stalledWorker("s1", stalledSince))
	rec := &recorder{}
	killer := &fakeKiller{rec: rec}
	notifier := &fakeNotifier{rec: rec}
	m := newMonitor(sessions, killer, notifier, clock, true, nil)

	if err := m.Tick(ctx); err != nil { // 1st tick: observed, not confirmed
		t.Fatalf("tick 1: %v", err)
	}
	clock.Advance(m.tick) // does not matter for detection, just simulates real cadence
	if err := m.Tick(ctx); err != nil { // 2nd tick: confirmed -> notify then kill
		t.Fatalf("tick 2: %v", err)
	}

	if got := killer.killedIDs(); len(got) != 1 || got[0] != "s1" {
		t.Fatalf("killed = %v, want [s1]", got)
	}
	if got := notifier.notified(); len(got) != 1 || got[0].SessionID != "s1" || got[0].Type != domain.NotificationAutoTerminated {
		t.Fatalf("notified = %+v", got)
	}
	order := rec.all()
	if len(order) != 2 || order[0] != "notify:s1" || order[1] != "kill:s1" {
		t.Fatalf("call order = %v, want [notify:s1 kill:s1]", order)
	}
}

// TestTick_KillProceedsWhenNotifyFails pins that a failed audit notification
// never blocks a genuine kill: losing the audit trail is bad, refusing to
// kill an actually-stalled session over a notify hiccup is worse.
func TestTick_KillProceedsWhenNotifyFails(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	stalledSince := clock.Now().Add(-2 * testThreshold)
	sessions := newFakeSessions(stalledWorker("s1", stalledSince))
	killer := &fakeKiller{}
	notifier := &fakeNotifier{err: errors.New("notify store down")}
	m := newMonitor(sessions, killer, notifier, clock, true, nil)

	_ = m.Tick(ctx)
	if err := m.Tick(ctx); err != nil {
		t.Fatalf("tick 2: %v", err)
	}
	if got := killer.killedIDs(); len(got) != 1 || got[0] != "s1" {
		t.Fatalf("kill must proceed despite notify failure, killed=%v", got)
	}
}

// TestTick_RecoveryClearsPendingState: a session observed stalled once, then
// recovering (a fresh activity signal) before the second tick, must reset the
// confirmation counter — a subsequent stall must again wait for two fresh
// consecutive ticks, not be killed on the very next observation.
func TestTick_RecoveryClearsPendingState(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	stalledSince := clock.Now().Add(-2 * testThreshold)
	sessions := newFakeSessions(stalledWorker("s1", stalledSince))
	killer := &fakeKiller{}
	notifier := &fakeNotifier{}
	m := newMonitor(sessions, killer, notifier, clock, true, nil)

	if err := m.Tick(ctx); err != nil { // tick 1: observed stalled
		t.Fatalf("tick 1: %v", err)
	}

	// Recovery: a fresh signal arrives before tick 2.
	sessions.set(stalledWorker("s1", clock.Now()))
	if err := m.Tick(ctx); err != nil { // tick 2: recovered, not stalled
		t.Fatalf("tick 2: %v", err)
	}
	if len(killer.killedIDs()) != 0 {
		t.Fatalf("recovered session must never be killed, killed=%v", killer.killedIDs())
	}

	// Stalls again: this must be treated as a FRESH first observation, not an
	// immediate second confirmation.
	sessions.set(stalledWorker("s1", clock.Now().Add(-2*testThreshold)))
	if err := m.Tick(ctx); err != nil { // tick 3: first observation of the new stall
		t.Fatalf("tick 3: %v", err)
	}
	if len(killer.killedIDs()) != 0 {
		t.Fatalf("a fresh stall observation must not kill immediately, killed=%v", killer.killedIDs())
	}
	if err := m.Tick(ctx); err != nil { // tick 4: confirmed -> kill
		t.Fatalf("tick 4: %v", err)
	}
	if got := killer.killedIDs(); len(got) != 1 || got[0] != "s1" {
		t.Fatalf("killed = %v, want [s1] after the second consecutive confirmation", got)
	}
}

// TestTick_OrchestratorNeverKilled pins structural immunity: an orchestrator
// in the identical stale-active shape as a stalled worker must never be
// killed, across any number of ticks.
func TestTick_OrchestratorNeverKilled(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	stalledSince := clock.Now().Add(-2 * testThreshold)
	rec := stalledWorker("orch-1", stalledSince)
	rec.Kind = domain.KindOrchestrator
	sessions := newFakeSessions(rec)
	killer := &fakeKiller{}
	notifier := &fakeNotifier{}
	m := newMonitor(sessions, killer, notifier, clock, true, nil)

	for i := 0; i < 5; i++ {
		if err := m.Tick(ctx); err != nil {
			t.Fatalf("tick %d: %v", i, err)
		}
	}
	if len(killer.killedIDs()) != 0 {
		t.Fatalf("orchestrator must never be killed, killed=%v", killer.killedIDs())
	}
	if len(notifier.notified()) != 0 {
		t.Fatalf("orchestrator must never be notified about, notified=%v", notifier.notified())
	}
}

// TestTick_WaitingInputNeverKilled pins that the sticky ActivityWaitingInput
// state is immune even when stale far past the threshold.
func TestTick_WaitingInputNeverKilled(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	stalledSince := clock.Now().Add(-2 * testThreshold)
	rec := stalledWorker("s1", stalledSince)
	rec.Activity.State = domain.ActivityWaitingInput
	sessions := newFakeSessions(rec)
	killer := &fakeKiller{}
	notifier := &fakeNotifier{}
	m := newMonitor(sessions, killer, notifier, clock, true, nil)

	for i := 0; i < 5; i++ {
		if err := m.Tick(ctx); err != nil {
			t.Fatalf("tick %d: %v", i, err)
		}
	}
	if len(killer.killedIDs()) != 0 {
		t.Fatalf("waiting_input session must never be killed, killed=%v", killer.killedIDs())
	}
}

// TestTick_TerminatedNeverTouched covers both a session already terminated
// before the monitor ever sees it, and one that becomes terminated (by some
// other path) between its first and second stalled observation — the
// pending entry must be cleared, never acted on.
func TestTick_TerminatedNeverTouched(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	stalledSince := clock.Now().Add(-2 * testThreshold)

	t.Run("already terminated", func(t *testing.T) {
		rec := stalledWorker("s1", stalledSince)
		rec.IsTerminated = true
		sessions := newFakeSessions(rec)
		killer := &fakeKiller{}
		m := newMonitor(sessions, killer, &fakeNotifier{}, clock, true, nil)
		for i := 0; i < 3; i++ {
			_ = m.Tick(ctx)
		}
		if len(killer.killedIDs()) != 0 {
			t.Fatalf("already-terminated session must never be killed, killed=%v", killer.killedIDs())
		}
	})

	t.Run("terminated between observations", func(t *testing.T) {
		sessions := newFakeSessions(stalledWorker("s2", stalledSince))
		killer := &fakeKiller{}
		m := newMonitor(sessions, killer, &fakeNotifier{}, clock, true, nil)
		if err := m.Tick(ctx); err != nil { // observed stalled
			t.Fatalf("tick 1: %v", err)
		}
		terminated := stalledWorker("s2", stalledSince)
		terminated.IsTerminated = true
		sessions.set(terminated)
		if err := m.Tick(ctx); err != nil { // now terminated externally
			t.Fatalf("tick 2: %v", err)
		}
		if len(killer.killedIDs()) != 0 {
			t.Fatalf("session terminated externally must never be killed, killed=%v", killer.killedIDs())
		}
		// Even if it somehow "un-terminates" and stalls again, it must again
		// require two fresh consecutive ticks (pending was cleared).
		sessions.set(stalledWorker("s2", stalledSince))
		if err := m.Tick(ctx); err != nil {
			t.Fatalf("tick 3: %v", err)
		}
		if len(killer.killedIDs()) != 0 {
			t.Fatalf("must require fresh confirmation after clearing, killed=%v", killer.killedIDs())
		}
	})
}

// TestTick_AutoKillFalseNeverKills pins that AutoKill=false fully disables
// killing (log-only), even across many confirmed-stalled ticks.
func TestTick_AutoKillFalseNeverKills(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	stalledSince := clock.Now().Add(-2 * testThreshold)
	sessions := newFakeSessions(stalledWorker("s1", stalledSince))
	killer := &fakeKiller{}
	notifier := &fakeNotifier{}
	m := newMonitor(sessions, killer, notifier, clock, false, nil)

	for i := 0; i < 5; i++ {
		if err := m.Tick(ctx); err != nil {
			t.Fatalf("tick %d: %v", i, err)
		}
	}
	if len(killer.killedIDs()) != 0 {
		t.Fatalf("AutoKill=false must never kill, killed=%v", killer.killedIDs())
	}
	if len(notifier.notified()) != 0 {
		t.Fatalf("AutoKill=false must never send the kill audit notification, notified=%v", notifier.notified())
	}
}

// TestTick_KillErrorRetriesNextTick pins that a failed Kill call keeps the
// session's pending entry so the very next tick retries — without requiring
// a fresh two-tick confirmation, since it was already confirmed.
func TestTick_KillErrorRetriesNextTick(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	stalledSince := clock.Now().Add(-2 * testThreshold)
	sessions := newFakeSessions(stalledWorker("s1", stalledSince))
	killer := &fakeKiller{failNext: true}
	notifier := &fakeNotifier{}
	m := newMonitor(sessions, killer, notifier, clock, true, nil)

	if err := m.Tick(ctx); err != nil { // tick 1: observed
		t.Fatalf("tick 1: %v", err)
	}
	if err := m.Tick(ctx); err != nil { // tick 2: confirmed, kill fails
		t.Fatalf("tick 2: %v", err)
	}
	if len(killer.killedIDs()) != 0 {
		t.Fatalf("failed kill must not report success, killed=%v", killer.killedIDs())
	}
	if err := m.Tick(ctx); err != nil { // tick 3: retries immediately, succeeds
		t.Fatalf("tick 3: %v", err)
	}
	if got := killer.killedIDs(); len(got) != 1 || got[0] != "s1" {
		t.Fatalf("killed = %v, want [s1] after the retry succeeds", got)
	}
}

// TestTick_FreshTerminalOutputPreventsKill pins the conservative-only
// contract: recent recorded PTY output overrides an otherwise-stalled
// activity reading and the session is never killed, even across many ticks.
func TestTick_FreshTerminalOutputPreventsKill(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	stalledSince := clock.Now().Add(-2 * testThreshold)
	rec := stalledWorker("s1", stalledSince)
	rec.Metadata.RuntimeHandleID = "handle-1"
	sessions := newFakeSessions(rec)
	killer := &fakeKiller{}
	pulse := newFakePulse()
	pulse.set("handle-1", clock.Now().Add(-1*time.Minute)) // fresh, within threshold
	m := newMonitor(sessions, killer, &fakeNotifier{}, clock, true, pulse)

	for i := 0; i < 5; i++ {
		if err := m.Tick(ctx); err != nil {
			t.Fatalf("tick %d: %v", i, err)
		}
	}
	if len(killer.killedIDs()) != 0 {
		t.Fatalf("fresh terminal output must prevent kill, killed=%v", killer.killedIDs())
	}
}

// TestTick_NoPulseDataFallsBackToActivityOnly pins that an absent secondary
// signal (no attachment ever opened for this session) never blocks a kill:
// missing data must fall back to activity-state-only logic, not fail closed.
func TestTick_NoPulseDataFallsBackToActivityOnly(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	stalledSince := clock.Now().Add(-2 * testThreshold)
	rec := stalledWorker("s1", stalledSince)
	rec.Metadata.RuntimeHandleID = "handle-1"
	sessions := newFakeSessions(rec)
	killer := &fakeKiller{}
	pulse := newFakePulse() // no data recorded for "handle-1"
	m := newMonitor(sessions, killer, &fakeNotifier{}, clock, true, pulse)

	_ = m.Tick(ctx)
	if err := m.Tick(ctx); err != nil {
		t.Fatalf("tick 2: %v", err)
	}
	if got := killer.killedIDs(); len(got) != 1 || got[0] != "s1" {
		t.Fatalf("missing pulse data must not block the kill, killed=%v", got)
	}
}

// TestTick_StaleTerminalOutputDoesNotPreventKill pins that pulse data older
// than the threshold does not count as "fresh": it must not block the kill.
func TestTick_StaleTerminalOutputDoesNotPreventKill(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	stalledSince := clock.Now().Add(-2 * testThreshold)
	rec := stalledWorker("s1", stalledSince)
	rec.Metadata.RuntimeHandleID = "handle-1"
	sessions := newFakeSessions(rec)
	killer := &fakeKiller{}
	pulse := newFakePulse()
	pulse.set("handle-1", clock.Now().Add(-2*testThreshold)) // stale, older than threshold
	m := newMonitor(sessions, killer, &fakeNotifier{}, clock, true, pulse)

	_ = m.Tick(ctx)
	if err := m.Tick(ctx); err != nil {
		t.Fatalf("tick 2: %v", err)
	}
	if got := killer.killedIDs(); len(got) != 1 || got[0] != "s1" {
		t.Fatalf("stale pulse data must not block the kill, killed=%v", got)
	}
}

// TestTick_HealthySessionsUntouched is a broad sanity sweep: a healthy
// working session, an idle session, and a session with no activity signal at
// all must never enter the pending/kill path.
func TestTick_HealthySessionsUntouched(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	healthy := stalledWorker("s1", clock.Now()) // fresh, well within threshold
	idle := domain.SessionRecord{ID: "s2", Kind: domain.KindWorker, Activity: domain.Activity{State: domain.ActivityIdle, LastActivityAt: clock.Now().Add(-2 * testThreshold)}}
	noSignal := domain.SessionRecord{ID: "s3", Kind: domain.KindWorker, Activity: domain.Activity{State: domain.ActivityActive}} // zero LastActivityAt
	sessions := newFakeSessions(healthy, idle, noSignal)
	killer := &fakeKiller{}
	m := newMonitor(sessions, killer, &fakeNotifier{}, clock, true, nil)

	for i := 0; i < 3; i++ {
		if err := m.Tick(ctx); err != nil {
			t.Fatalf("tick %d: %v", i, err)
		}
	}
	if len(killer.killedIDs()) != 0 {
		t.Fatalf("no healthy session should ever be killed, killed=%v", killer.killedIDs())
	}
}
