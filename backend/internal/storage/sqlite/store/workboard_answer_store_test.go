package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
)

func TestPrepareHermesAnswerAttemptAtomicallyConsumesOneShot(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, time.July, 17, 9, 10, 0, 0, time.UTC)
	project := domain.ProjectRecord{
		ID: "mer", Path: "/tmp/mer", RegisteredAt: now,
		Config: domain.ProjectConfig{Workboard: domain.WorkboardConfig{Autonomous: domain.WorkboardAutonomousConfig{Enabled: true, Mode: "skip_timeout", Sticky: false}}},
	}
	if err := s.UpsertProject(ctx, project); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	card := domain.WorkCard{ID: "card-1", ProjectID: "mer", BoardID: "default", Title: "Ship API", Priority: domain.CardPriorityNormal, Labels: []string{}, Status: domain.CardStatusRunning, TargetPath: "/tmp/mer", Agent: "codex", GoalVersion: 1, CreatedAt: now, UpdatedAt: now}
	if err := s.CreateWorkCard(ctx, card); err != nil {
		t.Fatalf("create card: %v", err)
	}
	project.Config.Workboard.Autonomous.Enabled = false
	event := domain.WorkCardEvent{ID: "attempt-1", CardID: card.ID, ProjectID: card.ProjectID, Kind: "hermes_answer_requested", Payload: `{}`, CreatedAt: now}
	if err := s.PrepareHermesAnswerAttempt(ctx, project, event, true); err != nil {
		t.Fatalf("PrepareHermesAnswerAttempt: %v", err)
	}
	gotProject, ok, err := s.GetProject(ctx, "mer")
	if err != nil || !ok || gotProject.Config.Workboard.Autonomous.Enabled {
		t.Fatalf("project after prepare = %+v ok=%t err=%v", gotProject.Config.Workboard.Autonomous, ok, err)
	}
	events, err := s.ListWorkCardEvents(ctx, card.ID)
	if err != nil || len(events) != 1 || events[0].ID != event.ID {
		t.Fatalf("events after prepare = %+v err=%v", events, err)
	}
}

func TestPrepareHermesAnswerAttemptRollsBackOneShotWithoutEvent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, time.July, 17, 9, 10, 0, 0, time.UTC)
	project := domain.ProjectRecord{
		ID: "mer", Path: "/tmp/mer", RegisteredAt: now,
		Config: domain.ProjectConfig{Workboard: domain.WorkboardConfig{Autonomous: domain.WorkboardAutonomousConfig{Enabled: true, Mode: "skip_timeout", Sticky: false}}},
	}
	if err := s.UpsertProject(ctx, project); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	card := domain.WorkCard{ID: "card-1", ProjectID: "mer", BoardID: "default", Title: "Ship API", Priority: domain.CardPriorityNormal, Labels: []string{}, Status: domain.CardStatusRunning, TargetPath: "/tmp/mer", Agent: "codex", GoalVersion: 1, CreatedAt: now, UpdatedAt: now}
	if err := s.CreateWorkCard(ctx, card); err != nil {
		t.Fatalf("create card: %v", err)
	}
	event := domain.WorkCardEvent{ID: "attempt-1", CardID: card.ID, ProjectID: card.ProjectID, Kind: "hermes_answer_requested", Payload: `{}`, CreatedAt: now}
	if err := s.AppendWorkCardEvent(ctx, event); err != nil {
		t.Fatalf("seed event: %v", err)
	}
	project.Config.Workboard.Autonomous.Enabled = false
	if err := s.PrepareHermesAnswerAttempt(ctx, project, event, true); err == nil {
		t.Fatal("PrepareHermesAnswerAttempt error = nil, want duplicate event failure")
	}
	gotProject, ok, err := s.GetProject(ctx, "mer")
	if err != nil || !ok || !gotProject.Config.Workboard.Autonomous.Enabled {
		t.Fatalf("project after rollback = %+v ok=%t err=%v", gotProject.Config.Workboard.Autonomous, ok, err)
	}
	events, err := s.ListWorkCardEvents(ctx, card.ID)
	if err != nil || len(events) != 1 || events[0].ID != event.ID {
		t.Fatalf("events after rollback = %+v err=%v", events, err)
	}
}
