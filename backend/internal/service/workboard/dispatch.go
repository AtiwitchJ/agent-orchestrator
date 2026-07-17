package workboard

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/ports"
	sessionsvc "github.com/modernagent/modern-agent/backend/internal/service/session"
)

// DispatchStore is the durable surface required to promote and claim cards.
// Workboard v1 has one board per project, so ListWorkCards uses defaultBoardID
// for both candidate selection and the project-wide running-card count.
type DispatchStore interface {
	GetProject(ctx context.Context, id string) (domain.ProjectRecord, bool, error)
	ListWorkCards(ctx context.Context, projectID, boardID string) ([]domain.WorkCard, error)
	UpdateWorkCard(ctx context.Context, card domain.WorkCard) error
}

// WorkerSpawner starts a worker through the existing session-service boundary.
type WorkerSpawner interface {
	Spawn(ctx context.Context, cfg ports.SpawnConfig) (domain.Session, error)
}

// SpawnRollbacker compensates a successful spawn when the card cannot durably
// link to it. Keeping this separate from WorkerSpawner keeps the dispatcher
// dependent on only the recovery operation it needs.
type SpawnRollbacker interface {
	RollbackSpawn(ctx context.Context, id domain.SessionID) (sessionsvc.RollbackOutcome, error)
}

// DispatchDeps configures a Dispatcher.
type DispatchDeps struct {
	Store      DispatchStore
	Spawner    WorkerSpawner
	Rollbacker SpawnRollbacker
	Clock      func() time.Time
}

// Dispatcher promotes due cards and claims ready cards under a project's WIP
// limit. It stores only card facts; session status and runtime liveness remain
// owned by their existing services.
type Dispatcher struct {
	store      DispatchStore
	spawner    WorkerSpawner
	rollbacker SpawnRollbacker
	clock      func() time.Time
	locks      sync.Map // map[string]*sync.Mutex, one serialized dispatch per project
}

// NewDispatcher constructs a workboard dispatcher.
func NewDispatcher(d DispatchDeps) *Dispatcher {
	clock := d.Clock
	if clock == nil {
		clock = time.Now
	}
	rollbacker := d.Rollbacker
	if rollbacker == nil {
		rollbacker, _ = d.Spawner.(SpawnRollbacker)
	}
	return &Dispatcher{store: d.Store, spawner: d.Spawner, rollbacker: rollbacker, clock: clock}
}

// DispatchOnce promotes due scheduled cards, then claims ready cards in
// priority/FIFO order until the project's WIP limit is reached. Before
// starting a worker, it writes a durable running claim with no session ID so
// a dispatcher created after a restart cannot select the same card again.
func (d *Dispatcher) DispatchOnce(ctx context.Context, projectID string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if d.store == nil || d.spawner == nil {
		return nil, nil
	}
	if d.rollbacker == nil {
		return nil, fmt.Errorf("workboard dispatcher requires spawn rollback support")
	}

	unlock := d.lockProject(projectID)
	defer unlock()

	project, ok, err := d.store.GetProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("get project %s: %w", projectID, err)
	}
	if !ok {
		return nil, fmt.Errorf("project %s not found", projectID)
	}
	cards, err := d.store.ListWorkCards(ctx, projectID, defaultBoardID)
	if err != nil {
		return nil, fmt.Errorf("list work cards for project %s: %w", projectID, err)
	}

	now := d.clock().UTC()
	for i := range cards {
		card := &cards[i]
		if card.Status != domain.CardStatusScheduled || card.ScheduledAt == nil || card.ScheduledAt.After(now) {
			continue
		}
		card.Status = domain.CardStatusReady
		card.ReadyAt = timePtr(now)
		card.UpdatedAt = now
		if err := d.store.UpdateWorkCard(ctx, *card); err != nil {
			return nil, fmt.Errorf("promote scheduled card %s: %w", card.ID, err)
		}
	}

	wipLimit := project.Config.Workboard.WIPLimit
	if wipLimit <= 0 {
		wipLimit = domain.DefaultWorkboardConfig().WIPLimit
	}
	running := 0
	candidates := make([]domain.WorkCard, 0, len(cards))
	for _, card := range cards {
		if card.Status == domain.CardStatusRunning {
			running++
		}
		if card.Status == domain.CardStatusReady && !card.PausedRetarget {
			candidates = append(candidates, card)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if left.Priority.Rank() != right.Priority.Rank() {
			return left.Priority.Rank() > right.Priority.Rank()
		}
		leftReady, rightReady := cardReadyAt(left), cardReadyAt(right)
		if !leftReady.Equal(rightReady) {
			return leftReady.Before(rightReady)
		}
		return left.ID < right.ID
	})

	var claimed []string
	for _, card := range candidates {
		if running >= wipLimit {
			break
		}
		card.Status = domain.CardStatusRunning
		card.SessionID = ""
		card.UpdatedAt = now
		if err := d.store.UpdateWorkCard(ctx, card); err != nil {
			return claimed, fmt.Errorf("persist dispatch claim for card %s: %w", card.ID, err)
		}

		session, err := d.spawner.Spawn(ctx, ports.SpawnConfig{
			ProjectID:  domain.ProjectID(projectID),
			Kind:       domain.KindWorker,
			Harness:    domain.AgentHarness(card.Agent),
			Prompt:     card.Title + "\n\n" + card.Notes,
			TargetPath: card.TargetPath,
		})
		if err != nil {
			spawnErr := fmt.Errorf("spawn worker for card %s: %w", card.ID, err)
			card.Status = domain.CardStatusReady
			card.SessionID = ""
			card.UpdatedAt = now
			if releaseErr := d.store.UpdateWorkCard(context.WithoutCancel(ctx), card); releaseErr != nil {
				return claimed, errors.Join(
					spawnErr,
					fmt.Errorf("release dispatch claim for card %s: %w", card.ID, releaseErr),
					fmt.Errorf("card %s remains durably claimed as running without a session ID", card.ID),
				)
			}
			return claimed, spawnErr
		}

		card.SessionID = string(session.ID)
		card.UpdatedAt = now
		if err := d.store.UpdateWorkCard(ctx, card); err != nil {
			linkErr := fmt.Errorf("link worker session for card %s: %w", card.ID, err)
			persistenceCtx := context.WithoutCancel(ctx)
			if _, rollbackErr := d.rollbacker.RollbackSpawn(persistenceCtx, session.ID); rollbackErr != nil {
				return claimed, errors.Join(
					linkErr,
					fmt.Errorf("rollback session %s: %w", session.ID, rollbackErr),
					fmt.Errorf("card %s remains durably claimed as running without a session ID", card.ID),
				)
			}
			card.Status = domain.CardStatusReady
			card.SessionID = ""
			card.UpdatedAt = now
			if releaseErr := d.store.UpdateWorkCard(persistenceCtx, card); releaseErr != nil {
				return claimed, errors.Join(
					linkErr,
					fmt.Errorf("release dispatch claim for card %s after rollback: %w", card.ID, releaseErr),
					fmt.Errorf("card %s remains durably claimed as running without a session ID", card.ID),
				)
			}
			return claimed, linkErr
		}
		claimed = append(claimed, card.ID)
		running++
	}
	return claimed, nil
}

func (d *Dispatcher) lockProject(projectID string) func() {
	value, _ := d.locks.LoadOrStore(projectID, &sync.Mutex{})
	lock := value.(*sync.Mutex)
	lock.Lock()
	return lock.Unlock
}

func cardReadyAt(card domain.WorkCard) time.Time {
	if card.ReadyAt != nil {
		return *card.ReadyAt
	}
	return card.CreatedAt
}

func timePtr(t time.Time) *time.Time { return &t }
