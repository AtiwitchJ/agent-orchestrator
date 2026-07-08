package heartbeat

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
)

var ctx = context.Background()

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// fakeClock is a mutable, injectable clock so tests can advance "now" between
// ticks deterministically (mirrors stallmon's test fakeClock).
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

type fakeStore struct {
	mu         sync.Mutex
	projects   []domain.ProjectRecord
	sessions   map[string][]domain.SessionRecord
	setting    string
	settingOK  bool
	settingErr error
	listErr    error
}

func (s *fakeStore) ListProjects(context.Context) ([]domain.ProjectRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listErr != nil {
		return nil, s.listErr
	}
	out := make([]domain.ProjectRecord, len(s.projects))
	copy(out, s.projects)
	return out, nil
}

func (s *fakeStore) ListSessions(_ context.Context, p domain.ProjectID) ([]domain.SessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[string(p)], nil
}

func (s *fakeStore) GetOrgSetting(_ context.Context, key string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settingErr != nil {
		return "", false, s.settingErr
	}
	if key != domain.OrgSettingHeartbeatPaused {
		return "", false, nil
	}
	return s.setting, s.settingOK, nil
}

func (s *fakeStore) setSessions(projectID string, sessions ...domain.SessionRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions == nil {
		s.sessions = map[string][]domain.SessionRecord{}
	}
	s.sessions[projectID] = sessions
}

type sentMsg struct {
	SessionID domain.SessionID
	Message   string
}

type fakeSender struct {
	mu       sync.Mutex
	sent     []sentMsg
	failNext bool
}

func (f *fakeSender) Send(_ context.Context, id domain.SessionID, message string, _ domain.SessionID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNext {
		f.failNext = false
		return errors.New("send failed")
	}
	f.sent = append(f.sent, sentMsg{SessionID: id, Message: message})
	return nil
}

func (f *fakeSender) sentMessages() []sentMsg {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]sentMsg, len(f.sent))
	copy(out, f.sent)
	return out
}

func heartbeatProject(id string, companyID string, role domain.HQRole) domain.ProjectRecord {
	return domain.ProjectRecord{
		ID: id, CompanyID: companyID, HQRole: role,
		Config: domain.ProjectConfig{Heartbeat: domain.HeartbeatConfig{Enabled: true, Interval: "30m"}},
	}
}

func orchestratorSession(id domain.SessionID, projectID string, state domain.ActivityState) domain.SessionRecord {
	return domain.SessionRecord{
		ID: id, ProjectID: domain.ProjectID(projectID), Kind: domain.KindOrchestrator,
		Activity: domain.Activity{State: state},
	}
}

func TestTick_PausedSkipsEntirePass(t *testing.T) {
	store := &fakeStore{projects: []domain.ProjectRecord{heartbeatProject("acme-hq", "acme", domain.HQRoleCompany)}, setting: "true", settingOK: true}
	store.setSessions("acme-hq", orchestratorSession("acme-hq-1", "acme-hq", domain.ActivityIdle))
	sender := &fakeSender{}
	m := New(store, sender, Config{Logger: quietLogger()})

	if err := m.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if got := sender.sentMessages(); len(got) != 0 {
		t.Fatalf("sent = %v, want none while paused", got)
	}
}

func TestTick_PauseSettingReadErrorFailsClosed(t *testing.T) {
	store := &fakeStore{projects: []domain.ProjectRecord{heartbeatProject("acme-hq", "acme", domain.HQRoleCompany)}, settingErr: errors.New("db unavailable")}
	store.setSessions("acme-hq", orchestratorSession("acme-hq-1", "acme-hq", domain.ActivityIdle))
	sender := &fakeSender{}
	m := New(store, sender, Config{Logger: quietLogger()})

	if err := m.Tick(ctx); err == nil {
		t.Fatal("Tick with pause-setting read error = nil, want error (fail closed)")
	}
	if got := sender.sentMessages(); len(got) != 0 {
		t.Fatalf("sent = %v, want none when the pause setting can't be read", got)
	}
}

func TestEvaluate_DisabledHeartbeatSkipped(t *testing.T) {
	project := heartbeatProject("acme-hq", "acme", domain.HQRoleCompany)
	project.Config.Heartbeat.Enabled = false
	store := &fakeStore{projects: []domain.ProjectRecord{project}}
	store.setSessions("acme-hq", orchestratorSession("acme-hq-1", "acme-hq", domain.ActivityIdle))
	sender := &fakeSender{}
	m := New(store, sender, Config{Logger: quietLogger()})

	if err := m.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if got := sender.sentMessages(); len(got) != 0 {
		t.Fatalf("sent = %v, want none for a project without heartbeat enabled", got)
	}
}

func TestEvaluate_NoOrchestratorSkipped(t *testing.T) {
	store := &fakeStore{projects: []domain.ProjectRecord{heartbeatProject("acme-hq", "acme", domain.HQRoleCompany)}}
	sender := &fakeSender{}
	m := New(store, sender, Config{Logger: quietLogger()})

	if err := m.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if got := sender.sentMessages(); len(got) != 0 {
		t.Fatalf("sent = %v, want none with no active orchestrator", got)
	}
}

func TestEvaluate_TerminatedOrchestratorSkipped(t *testing.T) {
	store := &fakeStore{projects: []domain.ProjectRecord{heartbeatProject("acme-hq", "acme", domain.HQRoleCompany)}}
	sess := orchestratorSession("acme-hq-1", "acme-hq", domain.ActivityIdle)
	sess.IsTerminated = true
	store.setSessions("acme-hq", sess)
	sender := &fakeSender{}
	m := New(store, sender, Config{Logger: quietLogger()})

	if err := m.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if got := sender.sentMessages(); len(got) != 0 {
		t.Fatalf("sent = %v, want none for a terminated orchestrator", got)
	}
}

// TestEvaluate_NotIdleSkippedWithoutAdvancingClock: an Active or WaitingInput
// orchestrator is skipped, and — critically — the per-project interval clock
// is NOT advanced by that skip, so the nudge fires on the very next tick the
// orchestrator is idle rather than waiting out a full interval afterwards.
func TestEvaluate_NotIdleSkippedWithoutAdvancingClock(t *testing.T) {
	for _, state := range []domain.ActivityState{domain.ActivityActive, domain.ActivityWaitingInput} {
		t.Run(string(state), func(t *testing.T) {
			clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
			store := &fakeStore{projects: []domain.ProjectRecord{heartbeatProject("acme-hq", "acme", domain.HQRoleCompany)}}
			store.setSessions("acme-hq", orchestratorSession("acme-hq-1", "acme-hq", state))
			sender := &fakeSender{}
			m := New(store, sender, Config{Clock: clock.Now, Logger: quietLogger()})

			if err := m.Tick(ctx); err != nil {
				t.Fatalf("Tick (not idle): %v", err)
			}
			if got := sender.sentMessages(); len(got) != 0 {
				t.Fatalf("sent = %v, want none while %s", got, state)
			}

			// Flip to idle at the SAME clock time (no interval has elapsed) and
			// tick again: it must send immediately, proving the skip above did
			// not start (or advance) the interval clock.
			store.setSessions("acme-hq", orchestratorSession("acme-hq-1", "acme-hq", domain.ActivityIdle))
			if err := m.Tick(ctx); err != nil {
				t.Fatalf("Tick (now idle): %v", err)
			}
			if got := sender.sentMessages(); len(got) != 1 {
				t.Fatalf("sent = %v, want exactly one nudge once idle", got)
			}
		})
	}
}

// TestEvaluate_SendsOnceThenWaitsForInterval: an idle orchestrator gets
// exactly one nudge, then silence on subsequent ticks until the configured
// interval elapses, at which point it sends again.
func TestEvaluate_SendsOnceThenWaitsForInterval(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	store := &fakeStore{projects: []domain.ProjectRecord{heartbeatProject("acme-hq", "acme", domain.HQRoleCompany)}}
	store.setSessions("acme-hq", orchestratorSession("acme-hq-1", "acme-hq", domain.ActivityIdle))
	sender := &fakeSender{}
	m := New(store, sender, Config{Clock: clock.Now, Logger: quietLogger()})

	if err := m.Tick(ctx); err != nil {
		t.Fatalf("first tick: %v", err)
	}
	if got := sender.sentMessages(); len(got) != 1 {
		t.Fatalf("sent after first tick = %v, want exactly one", got)
	}

	// Immediately tick again at the same clock time: still within the
	// interval, so no second send.
	if err := m.Tick(ctx); err != nil {
		t.Fatalf("second tick: %v", err)
	}
	if got := sender.sentMessages(); len(got) != 1 {
		t.Fatalf("sent after second tick = %v, want still exactly one", got)
	}

	// Advance past the 30m interval: the next tick sends again.
	clock.Advance(31 * time.Minute)
	if err := m.Tick(ctx); err != nil {
		t.Fatalf("third tick: %v", err)
	}
	if got := sender.sentMessages(); len(got) != 2 {
		t.Fatalf("sent after interval elapsed = %v, want two", got)
	}
}

// TestEvaluate_SendFailureRetriesNextTick: a failed send never advances
// lastSent, so the very next tick retries rather than waiting a full interval.
func TestEvaluate_SendFailureRetriesNextTick(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	store := &fakeStore{projects: []domain.ProjectRecord{heartbeatProject("acme-hq", "acme", domain.HQRoleCompany)}}
	store.setSessions("acme-hq", orchestratorSession("acme-hq-1", "acme-hq", domain.ActivityIdle))
	sender := &fakeSender{failNext: true}
	m := New(store, sender, Config{Clock: clock.Now, Logger: quietLogger()})

	if err := m.Tick(ctx); err != nil {
		t.Fatalf("first tick: %v", err)
	}
	if got := sender.sentMessages(); len(got) != 0 {
		t.Fatalf("sent after failed send = %v, want none", got)
	}

	if err := m.Tick(ctx); err != nil {
		t.Fatalf("retry tick: %v", err)
	}
	if got := sender.sentMessages(); len(got) != 1 {
		t.Fatalf("sent after retry = %v, want exactly one", got)
	}
}

func TestEvaluate_InvalidIntervalSkipsWithoutError(t *testing.T) {
	project := heartbeatProject("acme-hq", "acme", domain.HQRoleCompany)
	project.Config.Heartbeat.Interval = "not-a-duration"
	store := &fakeStore{projects: []domain.ProjectRecord{project}}
	store.setSessions("acme-hq", orchestratorSession("acme-hq-1", "acme-hq", domain.ActivityIdle))
	sender := &fakeSender{}
	m := New(store, sender, Config{Logger: quietLogger()})

	if err := m.Tick(ctx); err != nil {
		t.Fatalf("Tick with invalid interval must not error: %v", err)
	}
	if got := sender.sentMessages(); len(got) != 0 {
		t.Fatalf("sent = %v, want none for an invalid interval", got)
	}
}

func TestEvaluate_MessageNamesRoleForCompanyAndHolding(t *testing.T) {
	store := &fakeStore{projects: []domain.ProjectRecord{
		heartbeatProject("acme-hq", "acme", domain.HQRoleCompany),
		heartbeatProject("uppu-hq", "", domain.HQRoleHolding),
	}}
	store.setSessions("acme-hq", orchestratorSession("acme-hq-1", "acme-hq", domain.ActivityIdle))
	store.setSessions("uppu-hq", orchestratorSession("uppu-hq-1", "uppu-hq", domain.ActivityIdle))
	sender := &fakeSender{}
	m := New(store, sender, Config{Logger: quietLogger()})

	if err := m.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	sent := sender.sentMessages()
	if len(sent) != 2 {
		t.Fatalf("sent = %v, want two nudges", sent)
	}
	byID := map[domain.SessionID]string{}
	for _, s := range sent {
		byID[s.SessionID] = s.Message
	}
	if !strings.Contains(byID["acme-hq-1"], `PM of company "acme"`) {
		t.Fatalf("company nudge = %q, want it to name the PM role", byID["acme-hq-1"])
	}
	if !strings.Contains(byID["uppu-hq-1"], "holding CEO") {
		t.Fatalf("holding nudge = %q, want it to name the CEO role", byID["uppu-hq-1"])
	}
	if !strings.Contains(byID["acme-hq-1"], "ao org status") || !strings.Contains(byID["uppu-hq-1"], "ao org status") {
		t.Fatalf("nudge messages must instruct running `ao org status` first: %v", sent)
	}
}

func TestEvaluate_OrdinaryProjectNeverEligible(t *testing.T) {
	store := &fakeStore{projects: []domain.ProjectRecord{
		{ID: "acme-api", CompanyID: "acme", Config: domain.ProjectConfig{Heartbeat: domain.HeartbeatConfig{Enabled: true, Interval: "30m"}}},
	}}
	store.setSessions("acme-api", orchestratorSession("acme-api-1", "acme-api", domain.ActivityIdle))
	sender := &fakeSender{}
	m := New(store, sender, Config{Logger: quietLogger()})

	if err := m.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if got := sender.sentMessages(); len(got) != 0 {
		t.Fatalf("sent = %v, want none for a non-HQ project even with heartbeat enabled", got)
	}
}
