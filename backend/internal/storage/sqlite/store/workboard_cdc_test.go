package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
)

func TestWorkCardCDCUsesTimestampCreatedAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedProject(t, s, "workboard-cdc")

	now := time.Date(2026, time.July, 17, 3, 45, 6, 789_000_000, time.UTC)
	card := domain.WorkCard{
		ID: "card-cdc", ProjectID: "workboard-cdc", BoardID: "default",
		Title: "CDC timestamp", Priority: domain.CardPriorityNormal, Labels: []string{"smoke"},
		Status: domain.CardStatusTriage, TargetPath: "/tmp/workboard-cdc", Agent: "codex",
		GoalVersion: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.CreateWorkCard(ctx, card); err != nil {
		t.Fatalf("create work card: %v", err)
	}

	events, err := s.EventsAfter(ctx, 0, 10)
	if err != nil {
		t.Fatalf("read card CDC event: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}
	if events[0].Type != "work_card_changed" || !events[0].CreatedAt.Equal(now.Truncate(time.Second)) {
		t.Fatalf("event = %+v, want work_card_changed at %s", events[0], now.Truncate(time.Second))
	}

	card.Status = domain.CardStatusReady
	card.UpdatedAt = now.Add(time.Second)
	if err := s.UpdateWorkCard(ctx, card); err != nil {
		t.Fatalf("update work card: %v", err)
	}
	events, err = s.EventsAfter(ctx, events[0].Seq, 10)
	if err != nil {
		t.Fatalf("read updated card CDC event: %v", err)
	}
	if len(events) != 1 || events[0].Type != "work_card_changed" || !events[0].CreatedAt.Equal(card.UpdatedAt.Truncate(time.Second)) {
		t.Fatalf("updated event = %+v, want work_card_changed at %s", events, card.UpdatedAt.Truncate(time.Second))
	}
}
