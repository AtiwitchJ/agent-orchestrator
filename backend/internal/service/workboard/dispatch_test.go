package workboard

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/ports"
	sessionsvc "github.com/modernagent/modern-agent/backend/internal/service/session"
	"github.com/modernagent/modern-agent/backend/internal/storage/sqlite"
)

func TestDispatchOnce(t *testing.T) {
	now := time.Date(2026, time.July, 17, 9, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		wipLimit    int
		cards       []domain.WorkCard
		spawnErr    error
		wantClaimed []string
		wantStatus  map[string]domain.CardStatus
		wantSpawns  []string
	}{
		{
			name:     "orders priority then ready fifo under WIP cap",
			wipLimit: 2,
			cards: []domain.WorkCard{
				readyCard("normal-old", domain.CardPriorityNormal, now.Add(-3*time.Hour)),
				readyCard("high-new", domain.CardPriorityHigh, now.Add(-time.Hour)),
				readyCard("urgent", domain.CardPriorityUrgent, now.Add(-30*time.Minute)),
				readyCard("high-old", domain.CardPriorityHigh, now.Add(-2*time.Hour)),
			},
			wantClaimed: []string{"urgent", "high-old"},
			wantSpawns:  []string{"urgent", "high-old"},
		},
		{
			name:     "running cards consume project WIP",
			wipLimit: 1,
			cards: []domain.WorkCard{
				{ID: "already-running", ProjectID: "p1", BoardID: defaultBoardID, Status: domain.CardStatusRunning},
				readyCard("ready", domain.CardPriorityUrgent, now.Add(-time.Hour)),
			},
			wantClaimed: nil,
			wantSpawns:  nil,
		},
		{
			name:     "paused retarget card is not claimed",
			wipLimit: 2,
			cards: []domain.WorkCard{
				func() domain.WorkCard {
					c := readyCard("paused", domain.CardPriorityUrgent, now.Add(-time.Hour))
					c.PausedRetarget = true
					return c
				}(),
				readyCard("available", domain.CardPriorityLow, now.Add(-2*time.Hour)),
			},
			wantClaimed: []string{"available"},
			wantSpawns:  []string{"available"},
		},
		{
			name:     "due scheduled card is promoted even while WIP is full",
			wipLimit: 1,
			cards: []domain.WorkCard{
				{ID: "already-running", ProjectID: "p1", BoardID: defaultBoardID, Status: domain.CardStatusRunning},
				{ID: "scheduled", ProjectID: "p1", BoardID: defaultBoardID, Status: domain.CardStatusScheduled, ScheduledAt: ptrTime(now.Add(-time.Minute))},
			},
			wantClaimed: nil,
			wantStatus:  map[string]domain.CardStatus{"scheduled": domain.CardStatusReady},
			wantSpawns:  nil,
		},
		{
			name:     "spawn failure leaves card ready and unlinked",
			wipLimit: 1,
			cards: []domain.WorkCard{
				readyCard("ready", domain.CardPriorityUrgent, now.Add(-time.Hour)),
			},
			spawnErr:    errors.New("runtime unavailable"),
			wantClaimed: nil,
			wantStatus:  map[string]domain.CardStatus{"ready": domain.CardStatusReady},
			wantSpawns:  []string{"ready"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := newDispatchStore(tc.wipLimit, tc.cards)
			spawner := &dispatchSpawner{err: tc.spawnErr}
			dispatcher := NewDispatcher(DispatchDeps{Store: store, Spawner: spawner, Clock: func() time.Time { return now }})

			claimed, err := dispatcher.DispatchOnce(context.Background(), "p1")
			if tc.spawnErr != nil {
				if !errors.Is(err, tc.spawnErr) {
					t.Fatalf("DispatchOnce error = %v, want %v", err, tc.spawnErr)
				}
			} else if err != nil {
				t.Fatalf("DispatchOnce: %v", err)
			}
			if !reflect.DeepEqual(claimed, tc.wantClaimed) {
				t.Fatalf("claimed = %v, want %v", claimed, tc.wantClaimed)
			}
			if got := spawner.cardIDs(); !reflect.DeepEqual(got, tc.wantSpawns) {
				t.Fatalf("spawned cards = %v, want %v", got, tc.wantSpawns)
			}
			for id, want := range tc.wantStatus {
				if got := store.cards[id].Status; got != want {
					t.Fatalf("card %s status = %q, want %q", id, got, want)
				}
			}
			for _, id := range claimed {
				card := store.cards[id]
				if card.Status != domain.CardStatusRunning || card.SessionID == "" {
					t.Fatalf("claimed card %s = %#v, want running and linked", id, card)
				}
			}
			if tc.spawnErr != nil {
				card := store.cards["ready"]
				if card.SessionID != "" {
					t.Fatalf("failed card session_id = %q, want empty", card.SessionID)
				}
				if card.ReadyAt == nil || !card.ReadyAt.Equal(now.Add(-time.Hour)) {
					t.Fatalf("failed card ready_at = %v, want original %v", card.ReadyAt, now.Add(-time.Hour))
				}
			}
		})
	}
}

func TestDispatchOnceSpawnsWorkerWithCardHarnessAndPrompt(t *testing.T) {
	now := time.Date(2026, time.July, 17, 9, 0, 0, 0, time.UTC)
	card := readyCard("card", domain.CardPriorityNormal, now)
	card.TargetPath = "/repo/services/api"
	store := newDispatchStore(1, []domain.WorkCard{card})
	spawner := &dispatchSpawner{}
	dispatcher := NewDispatcher(DispatchDeps{Store: store, Spawner: spawner, Clock: func() time.Time { return now }})

	if _, err := dispatcher.DispatchOnce(context.Background(), "p1"); err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}
	if len(spawner.configs) != 1 {
		t.Fatalf("spawn count = %d, want 1", len(spawner.configs))
	}
	got := spawner.configs[0]
	if got.ProjectID != "p1" || got.Kind != domain.KindWorker || got.Harness != domain.HarnessCodex || got.Prompt != "card title\n\ncard notes" || got.TargetPath != "/repo/services/api" {
		t.Fatalf("spawn config = %#v", got)
	}
}

func TestDispatchOnceDoesNotSpawnWhenDurableClaimFails(t *testing.T) {
	now := time.Date(2026, time.July, 17, 9, 0, 0, 0, time.UTC)
	claimErr := errors.New("database unavailable")
	store := newDispatchStore(1, []domain.WorkCard{readyCard("card", domain.CardPriorityNormal, now)})
	store.failClaimErr = claimErr
	spawner := &dispatchSpawner{}
	dispatcher := NewDispatcher(DispatchDeps{Store: store, Spawner: spawner, Clock: func() time.Time { return now }})

	claimed, err := dispatcher.DispatchOnce(context.Background(), "p1")
	if !errors.Is(err, claimErr) {
		t.Fatalf("DispatchOnce error = %v, want %v", err, claimErr)
	}
	if len(claimed) != 0 {
		t.Fatalf("claimed = %v, want none", claimed)
	}
	if got := spawner.cardIDs(); len(got) != 0 {
		t.Fatalf("spawned cards = %v, want none", got)
	}
	card := store.cards["card"]
	if card.Status != domain.CardStatusReady || card.SessionID != "" {
		t.Fatalf("card after failed claim = %#v, want ready and unlinked", card)
	}
}

func TestDispatchOnce_TwoDispatchersAtomicallyRespectWIPLimit(t *testing.T) {
	now := time.Date(2026, time.July, 17, 9, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.UpsertProject(context.Background(), domain.ProjectRecord{
		ID: "p1", Path: t.TempDir(), RegisteredAt: now,
		Config: domain.ProjectConfig{Workboard: domain.WorkboardConfig{WIPLimit: 1}},
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	for _, card := range []domain.WorkCard{
		readyCard("first", domain.CardPriorityHigh, now.Add(-time.Hour)),
		readyCard("second", domain.CardPriorityNormal, now.Add(-time.Hour)),
	} {
		if err := store.CreateWorkCard(context.Background(), card); err != nil {
			t.Fatalf("seed card %s: %v", card.ID, err)
		}
	}

	barrier := &listBarrierStore{Store: store, arrived: make(chan struct{}, 2), release: make(chan struct{})}
	spawner := &dispatchSpawner{}
	dispatcherA := NewDispatcher(DispatchDeps{Store: barrier, Spawner: spawner, Clock: func() time.Time { return now }})
	dispatcherB := NewDispatcher(DispatchDeps{Store: barrier, Spawner: spawner, Clock: func() time.Time { return now }})
	type result struct {
		claimed []string
		err     error
	}
	results := make(chan result, 2)
	go func() { claimed, err := dispatcherA.DispatchOnce(context.Background(), "p1"); results <- result{claimed, err} }()
	go func() { claimed, err := dispatcherB.DispatchOnce(context.Background(), "p1"); results <- result{claimed, err} }()
	<-barrier.arrived
	<-barrier.arrived
	close(barrier.release)

	var totalClaims int
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatalf("DispatchOnce: %v", result.err)
		}
		totalClaims += len(result.claimed)
	}
	if totalClaims != 1 {
		t.Fatalf("total claims = %d, want 1", totalClaims)
	}
	if got := spawner.cardIDs(); len(got) != 1 {
		t.Fatalf("spawned cards = %v, want exactly one", got)
	}
	cards, err := store.ListWorkCards(context.Background(), "p1", defaultBoardID)
	if err != nil {
		t.Fatalf("list cards: %v", err)
	}
	running := 0
	for _, card := range cards {
		if card.Status == domain.CardStatusRunning {
			running++
		}
	}
	if running != 1 {
		t.Fatalf("running cards = %d, want 1", running)
	}
}

func TestDispatchOnceRollsBackSpawnWhenSessionLinkFails(t *testing.T) {
	now := time.Date(2026, time.July, 17, 9, 0, 0, 0, time.UTC)
	linkErr := errors.New("database unavailable")
	store := newDispatchStore(1, []domain.WorkCard{readyCard("card", domain.CardPriorityNormal, now)})
	store.failLinkErr = linkErr
	spawner := &dispatchSpawner{}
	dispatcher := NewDispatcher(DispatchDeps{Store: store, Spawner: spawner, Clock: func() time.Time { return now }})

	claimed, err := dispatcher.DispatchOnce(context.Background(), "p1")
	if !errors.Is(err, linkErr) {
		t.Fatalf("DispatchOnce error = %v, want %v", err, linkErr)
	}
	if len(claimed) != 0 {
		t.Fatalf("claimed = %v, want none", claimed)
	}
	if got := spawner.rollbackIDs; !reflect.DeepEqual(got, []domain.SessionID{"session-card title\n\ncard notes"}) {
		t.Fatalf("rollback sessions = %v", got)
	}
	card := store.cards["card"]
	if card.Status != domain.CardStatusReady || card.SessionID != "" {
		t.Fatalf("card after failed link = %#v, want ready and unlinked", card)
	}
}

func TestDispatchOnceDurablyClaimsCardWhenSessionLinkAndRollbackFail(t *testing.T) {
	now := time.Date(2026, time.July, 17, 9, 0, 0, 0, time.UTC)
	linkErr := errors.New("database unavailable")
	rollbackErr := errors.New("runtime teardown unavailable")
	store := newDispatchStore(1, []domain.WorkCard{readyCard("card", domain.CardPriorityNormal, now)})
	store.failLinkErr = linkErr
	spawner := &dispatchSpawner{rollbackErr: rollbackErr}
	dispatcher := NewDispatcher(DispatchDeps{Store: store, Spawner: spawner, Clock: func() time.Time { return now }})

	claimed, err := dispatcher.DispatchOnce(context.Background(), "p1")
	if !errors.Is(err, linkErr) || !errors.Is(err, rollbackErr) {
		t.Fatalf("DispatchOnce error = %v, want joined link and rollback errors", err)
	}
	if !strings.Contains(err.Error(), "remains durably claimed") {
		t.Fatalf("DispatchOnce error = %v, want durable claim annotation", err)
	}
	if len(claimed) != 0 {
		t.Fatalf("claimed = %v, want none", claimed)
	}
	card := store.cards["card"]
	if card.Status != domain.CardStatusRunning || card.SessionID != "" {
		t.Fatalf("card after failed rollback = %#v, want running and unlinked durable claim", card)
	}

	newDispatcher := NewDispatcher(DispatchDeps{Store: store, Spawner: spawner, Clock: func() time.Time { return now }})
	claimed, err = newDispatcher.DispatchOnce(context.Background(), "p1")
	if err != nil {
		t.Fatalf("second DispatchOnce: %v", err)
	}
	if len(claimed) != 0 {
		t.Fatalf("second claimed = %v, want none", claimed)
	}
	if got := spawner.cardIDs(); !reflect.DeepEqual(got, []string{"card"}) {
		t.Fatalf("spawned cards = %v, want original spawn only", got)
	}
}

type dispatchStore struct {
	project      domain.ProjectRecord
	cards        map[string]domain.WorkCard
	failClaimErr error
	failLinkErr  error
	mu           sync.Mutex
}

func newDispatchStore(wipLimit int, cards []domain.WorkCard) *dispatchStore {
	s := &dispatchStore{project: domain.ProjectRecord{ID: "p1", Config: domain.ProjectConfig{Workboard: domain.WorkboardConfig{WIPLimit: wipLimit}}}, cards: make(map[string]domain.WorkCard, len(cards))}
	for _, card := range cards {
		s.cards[card.ID] = card
	}
	return s
}

func (s *dispatchStore) GetProject(_ context.Context, id string) (domain.ProjectRecord, bool, error) {
	return s.project, id == s.project.ID, nil
}

func (s *dispatchStore) ListWorkCards(_ context.Context, projectID, boardID string) ([]domain.WorkCard, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if projectID != s.project.ID || boardID != defaultBoardID {
		return nil, nil
	}
	cards := make([]domain.WorkCard, 0, len(s.cards))
	for _, card := range s.cards {
		cards = append(cards, card)
	}
	return cards, nil
}

func (s *dispatchStore) UpdateWorkCard(_ context.Context, card domain.WorkCard) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if card.SessionID != "" && s.failLinkErr != nil {
		return s.failLinkErr
	}
	s.cards[card.ID] = card
	return nil
}

func (s *dispatchStore) ClaimReadyWorkCard(_ context.Context, cardID, projectID string, wipLimit int, at time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failClaimErr != nil {
		return false, s.failClaimErr
	}
	card, ok := s.cards[cardID]
	if !ok || card.ProjectID != projectID || card.Status != domain.CardStatusReady || card.PausedRetarget {
		return false, nil
	}
	running := 0
	for _, card := range s.cards {
		if card.ProjectID == projectID && card.Status == domain.CardStatusRunning {
			running++
		}
	}
	if running >= wipLimit {
		return false, nil
	}
	card.Status = domain.CardStatusRunning
	card.SessionID = ""
	card.UpdatedAt = at
	s.cards[cardID] = card
	return true, nil
}

type listBarrierStore struct {
	*sqlite.Store
	arrived chan<- struct{}
	release <-chan struct{}
}

func (s *listBarrierStore) ListWorkCards(ctx context.Context, projectID, boardID string) ([]domain.WorkCard, error) {
	cards, err := s.Store.ListWorkCards(ctx, projectID, boardID)
	if err != nil {
		return nil, err
	}
	s.arrived <- struct{}{}
	<-s.release
	return cards, nil
}

type dispatchSpawner struct {
	err         error
	rollbackErr error
	configs     []ports.SpawnConfig
	rollbackIDs []domain.SessionID
	mu          sync.Mutex
}

func (s *dispatchSpawner) Spawn(_ context.Context, cfg ports.SpawnConfig) (domain.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configs = append(s.configs, cfg)
	if s.err != nil {
		return domain.Session{}, s.err
	}
	return domain.Session{SessionRecord: domain.SessionRecord{ID: domain.SessionID("session-" + cfg.Prompt)}}, nil
}

func (s *dispatchSpawner) RollbackSpawn(_ context.Context, id domain.SessionID) (sessionsvc.RollbackOutcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rollbackIDs = append(s.rollbackIDs, id)
	return sessionsvc.RollbackOutcome{Killed: s.rollbackErr == nil}, s.rollbackErr
}

func (s *dispatchSpawner) cardIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []string
	for _, cfg := range s.configs {
		title, _, _ := strings.Cut(cfg.Prompt, "\n\n")
		ids = append(ids, strings.TrimSuffix(title, " title"))
	}
	return ids
}

func readyCard(id string, priority domain.CardPriority, readyAt time.Time) domain.WorkCard {
	return domain.WorkCard{
		ID: id, ProjectID: "p1", BoardID: defaultBoardID, Title: id + " title", Notes: id + " notes",
		Priority: priority, Status: domain.CardStatusReady, ReadyAt: ptrTime(readyAt), Agent: string(domain.HarnessCodex),
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
