package workboard_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/service/workboard"
	"github.com/modernagent/modern-agent/backend/internal/storage/sqlite"
)

func TestCreateRequiresAgentAndPath(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.Create(context.Background(), workboard.CreateInput{
		ProjectID: "p1", Title: "t", Notes: "n", Priority: domain.CardPriorityNormal,
		Labels: []string{"bug"}, TargetPath: "/nope", Agent: "",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestCreateDefaultsAndReadyTimestamp(t *testing.T) {
	svc, root := newTestService(t)
	ctx := context.Background()

	card, err := svc.Create(ctx, workboard.CreateInput{
		ProjectID: "p1", Title: "triage", Notes: "notes", Priority: domain.CardPriorityNormal,
		Labels: []string{"bug"}, TargetPath: filepath.Join(root, "app"), Agent: "codex",
	})
	if err != nil {
		t.Fatalf("Create triage: %v", err)
	}
	if card.ID == "" || card.BoardID != "default" || card.Status != domain.CardStatusTriage || card.ReadyAt != nil {
		t.Fatalf("triage card = %#v", card)
	}

	ready, err := svc.Create(ctx, workboard.CreateInput{
		ProjectID: "p1", Title: "ready", Notes: "notes", Priority: domain.CardPriorityHigh,
		Labels: []string{"feature"}, Status: domain.CardStatusReady, TargetPath: root, Agent: "codex",
	})
	if err != nil {
		t.Fatalf("Create ready: %v", err)
	}
	if ready.ReadyAt == nil || !ready.ReadyAt.Equal(testNow) {
		t.Fatalf("ready timestamp = %v, want %v", ready.ReadyAt, testNow)
	}
}

func TestCreateRequiresLabels(t *testing.T) {
	svc, root := newTestService(t)
	_, err := svc.Create(context.Background(), workboard.CreateInput{
		ProjectID: "p1", Title: "missing labels", Notes: "notes", Priority: domain.CardPriorityNormal,
		TargetPath: root, Agent: "codex",
	})
	if err == nil {
		t.Fatal("Create without labels = nil error, want validation error")
	}
}

func TestCRUDUsesPartialUpdateAndMove(t *testing.T) {
	svc, root := newTestService(t)
	ctx := context.Background()
	card, err := svc.Create(ctx, workboard.CreateInput{
		ProjectID: "p1", Title: "original", Notes: "notes", Priority: domain.CardPriorityNormal,
		Labels: []string{"bug"}, TargetPath: root, Agent: "codex",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	title := "renamed"
	labels := []string{"bug", "urgent"}
	updated, err := svc.Update(ctx, card.ID, workboard.UpdateInput{Title: &title, Labels: &labels})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Title != title || len(updated.Labels) != 2 || updated.Agent != "codex" || updated.Status != domain.CardStatusTriage {
		t.Fatalf("updated card = %#v", updated)
	}
	outside := t.TempDir()
	if _, err := svc.Update(ctx, card.ID, workboard.UpdateInput{TargetPath: &outside}); err == nil {
		t.Fatal("Update outside registered repo = nil error, want validation error")
	}

	moved, err := svc.Move(ctx, card.ID, domain.CardStatusReady, 3)
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if moved.Status != domain.CardStatusReady || moved.Position != 3 || moved.ReadyAt == nil || !moved.ReadyAt.Equal(testNow) {
		t.Fatalf("moved card = %#v", moved)
	}

	got, err := svc.Get(ctx, card.ID)
	if err != nil || got.ID != card.ID {
		t.Fatalf("Get = %#v, %v", got, err)
	}
	list, err := svc.List(ctx, "p1", "default")
	if err != nil || len(list) != 1 || list[0].ID != card.ID {
		t.Fatalf("List = %#v, %v", list, err)
	}
}

func TestMoveAndUpdatePreserveReadyTimestamp(t *testing.T) {
	now := testNow
	svc, root := newTestServiceWithClock(t, func() time.Time { return now })
	ctx := context.Background()
	card, err := svc.Create(ctx, workboard.CreateInput{
		ProjectID: "p1", Title: "ready", Notes: "notes", Priority: domain.CardPriorityNormal,
		Labels: []string{"bug"}, Status: domain.CardStatusReady, TargetPath: root, Agent: "codex",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	readyAt := *card.ReadyAt

	now = now.Add(time.Hour)
	moved, err := svc.Move(ctx, card.ID, domain.CardStatusReady, 1)
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if moved.ReadyAt == nil || !moved.ReadyAt.Equal(readyAt) {
		t.Fatalf("Move ReadyAt = %v, want %v", moved.ReadyAt, readyAt)
	}

	now = now.Add(time.Hour)
	status := domain.CardStatusReady
	updated, err := svc.Update(ctx, card.ID, workboard.UpdateInput{Status: &status})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.ReadyAt == nil || !updated.ReadyAt.Equal(readyAt) {
		t.Fatalf("Update ReadyAt = %v, want %v", updated.ReadyAt, readyAt)
	}
}

var testNow = time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC)

func newTestService(t *testing.T) (*workboard.Service, string) {
	return newTestServiceWithClock(t, func() time.Time { return testNow })
}

func newTestServiceWithClock(t *testing.T, clock func() time.Time) (*workboard.Service, string) {
	t.Helper()
	root := t.TempDir()
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.UpsertProject(context.Background(), domain.ProjectRecord{
		ID: "p1", Path: root, DisplayName: "Project", RegisteredAt: testNow,
	}); err != nil {
		t.Fatalf("register project: %v", err)
	}
	return workboard.NewWithDeps(workboard.Deps{Store: store, Clock: clock}), root
}
