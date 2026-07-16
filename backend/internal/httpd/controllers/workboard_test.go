package controllers_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/config"
	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/httpd"
	"github.com/modernagent/modern-agent/backend/internal/httpd/apierr"
	workboardsvc "github.com/modernagent/modern-agent/backend/internal/service/workboard"
)

type fakeWorkboardService struct {
	cards      []domain.WorkCard
	createIn   workboardsvc.CreateInput
	updateID   string
	updateIn   workboardsvc.UpdateInput
	moveID     string
	moveStatus domain.CardStatus
	movePos    int64
}

func (f *fakeWorkboardService) Create(_ context.Context, in workboardsvc.CreateInput) (domain.WorkCard, error) {
	f.createIn = in
	if in.Agent == "" {
		return domain.WorkCard{}, apierr.Invalid("WORK_CARD_AGENT_REQUIRED", "Agent is required", nil)
	}
	return f.cards[0], nil
}

func (f *fakeWorkboardService) List(context.Context, string, string) ([]domain.WorkCard, error) {
	return f.cards, nil
}

func (f *fakeWorkboardService) Get(_ context.Context, id string) (domain.WorkCard, error) {
	for _, card := range f.cards {
		if card.ID == id {
			return card, nil
		}
	}
	return domain.WorkCard{}, apierr.NotFound("WORK_CARD_NOT_FOUND", "Unknown work card")
}

func (f *fakeWorkboardService) Update(_ context.Context, id string, in workboardsvc.UpdateInput) (domain.WorkCard, error) {
	f.updateID, f.updateIn = id, in
	return f.cards[0], nil
}

func (f *fakeWorkboardService) Move(_ context.Context, id string, status domain.CardStatus, position int64) (domain.WorkCard, error) {
	f.moveID, f.moveStatus, f.movePos = id, status, position
	return f.cards[0], nil
}

func newWorkboardTestServer(t *testing.T, svc *fakeWorkboardService) *httptest.Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{Workboard: svc}, httpd.ControlDeps{}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCreateWorkCard_Validation(t *testing.T) {
	srv := newWorkboardTestServer(t, &fakeWorkboardService{})

	body, status, _ := doRequest(t, srv, http.MethodPost, "/api/v1/projects/proj/workboard/cards", `{"title":"Card","notes":"Details","priority":"normal","labels":["api"],"targetPath":"/repo","agent":""}`)
	assertErrorCode(t, body, status, http.StatusBadRequest, "WORK_CARD_AGENT_REQUIRED")
}

func TestMoveWorkCard_RequiresPosition(t *testing.T) {
	svc := &fakeWorkboardService{cards: []domain.WorkCard{{ID: "card_1"}}}
	srv := newWorkboardTestServer(t, svc)

	body, status, _ := doRequest(t, srv, http.MethodPost, "/api/v1/workboard/cards/card_1/move", `{"status":"ready"}`)
	assertErrorCode(t, body, status, http.StatusBadRequest, "WORK_CARD_POSITION_REQUIRED")
}

func TestUpdateWorkCard_NullScheduledAtClearsSchedule(t *testing.T) {
	svc := &fakeWorkboardService{cards: []domain.WorkCard{{ID: "card_1"}}}
	srv := newWorkboardTestServer(t, svc)

	body, status, _ := doRequest(t, srv, http.MethodPatch, "/api/v1/workboard/cards/card_1", `{"scheduledAt":null}`)
	if status != http.StatusOK {
		t.Fatalf("update status = %d, want 200; body=%s", status, body)
	}
	if !svc.updateIn.ScheduledAt.Set || svc.updateIn.ScheduledAt.Value != nil {
		t.Fatalf("scheduledAt update = %+v, want explicit nil", svc.updateIn.ScheduledAt)
	}
}

func TestWorkboardAPI_CardCRUDAndMove(t *testing.T) {
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	svc := &fakeWorkboardService{cards: []domain.WorkCard{{
		ID: "card_1", ProjectID: "proj", BoardID: "default", Title: "Card", Notes: "Details",
		Priority: domain.CardPriorityNormal, Labels: []string{"api"}, Status: domain.CardStatusTriage,
		TargetPath: "/repo", Agent: "codex", GoalVersion: 1, CreatedAt: now, UpdatedAt: now,
	}}}
	srv := newWorkboardTestServer(t, svc)

	body, status, _ := doRequest(t, srv, http.MethodGet, "/api/v1/projects/proj/workboard/cards", "")
	if status != http.StatusOK {
		t.Fatalf("list status = %d, want 200; body=%s", status, body)
	}
	var list struct {
		Cards []struct {
			ID string `json:"id"`
		} `json:"cards"`
	}
	mustJSON(t, body, &list)
	if len(list.Cards) != 1 || list.Cards[0].ID != "card_1" {
		t.Fatalf("list = %+v", list)
	}

	body, status, _ = doRequest(t, srv, http.MethodPost, "/api/v1/projects/proj/workboard/cards", `{"title":"Card","notes":"Details","priority":"normal","labels":["api"],"targetPath":"/repo","agent":"codex"}`)
	if status != http.StatusCreated {
		t.Fatalf("create status = %d, want 201; body=%s", status, body)
	}
	if svc.createIn.ProjectID != "proj" || svc.createIn.BoardID != "" || svc.createIn.Agent != "codex" || svc.createIn.Status != "" {
		t.Fatalf("create input = %+v", svc.createIn)
	}

	body, status, _ = doRequest(t, srv, http.MethodGet, "/api/v1/workboard/cards/card_1", "")
	if status != http.StatusOK {
		t.Fatalf("get status = %d, want 200; body=%s", status, body)
	}

	body, status, _ = doRequest(t, srv, http.MethodPatch, "/api/v1/workboard/cards/card_1", `{"priority":"high","position":4}`)
	if status != http.StatusOK {
		t.Fatalf("update status = %d, want 200; body=%s", status, body)
	}
	if svc.updateID != "card_1" || svc.updateIn.Priority == nil || *svc.updateIn.Priority != domain.CardPriorityHigh || svc.updateIn.Position == nil || *svc.updateIn.Position != 4 {
		t.Fatalf("update = id=%q input=%+v", svc.updateID, svc.updateIn)
	}

	body, status, _ = doRequest(t, srv, http.MethodPost, "/api/v1/workboard/cards/card_1/move", `{"status":"ready","position":0}`)
	if status != http.StatusOK {
		t.Fatalf("move status = %d, want 200; body=%s", status, body)
	}
	if svc.moveID != "card_1" || svc.moveStatus != domain.CardStatusReady || svc.movePos != 0 {
		t.Fatalf("move = id=%q status=%q position=%d", svc.moveID, svc.moveStatus, svc.movePos)
	}
}

func TestWorkboardAPI_NilServiceReturnsNotImplemented(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{}, httpd.ControlDeps{}))
	t.Cleanup(srv.Close)

	body, status, _ := doRequest(t, srv, http.MethodGet, "/api/v1/projects/proj/workboard/cards", "")
	assertErrorCode(t, body, status, http.StatusNotImplemented, "NOT_IMPLEMENTED")
}
