package workboard

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/ports"
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
			}
		})
	}
}

func TestDispatchOnceSpawnsWorkerWithCardHarnessAndPrompt(t *testing.T) {
	now := time.Date(2026, time.July, 17, 9, 0, 0, 0, time.UTC)
	store := newDispatchStore(1, []domain.WorkCard{readyCard("card", domain.CardPriorityNormal, now)})
	spawner := &dispatchSpawner{}
	dispatcher := NewDispatcher(DispatchDeps{Store: store, Spawner: spawner, Clock: func() time.Time { return now }})

	if _, err := dispatcher.DispatchOnce(context.Background(), "p1"); err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}
	if len(spawner.configs) != 1 {
		t.Fatalf("spawn count = %d, want 1", len(spawner.configs))
	}
	got := spawner.configs[0]
	if got.ProjectID != "p1" || got.Kind != domain.KindWorker || got.Harness != domain.HarnessCodex || got.Prompt != "card title\n\ncard notes" {
		t.Fatalf("spawn config = %#v", got)
	}
}

type dispatchStore struct {
	project domain.ProjectRecord
	cards   map[string]domain.WorkCard
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
	s.cards[card.ID] = card
	return nil
}

type dispatchSpawner struct {
	err     error
	configs []ports.SpawnConfig
}

func (s *dispatchSpawner) Spawn(_ context.Context, cfg ports.SpawnConfig) (domain.Session, error) {
	s.configs = append(s.configs, cfg)
	if s.err != nil {
		return domain.Session{}, s.err
	}
	return domain.Session{SessionRecord: domain.SessionRecord{ID: domain.SessionID("session-" + cfg.Prompt)}}, nil
}

func (s *dispatchSpawner) cardIDs() []string {
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
