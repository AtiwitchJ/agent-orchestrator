package controllers_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/controllers"
	messagesvc "github.com/aoagents/agent-orchestrator/backend/internal/service/message"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

func TestMessagesRoute_DefaultsToStubWithoutManager(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{}, httpd.ControlDeps{}))
	t.Cleanup(srv.Close)

	body, status, _ := doRequest(t, srv, "GET", "/api/v1/projects/ao/messages", "")
	assertErrorCode(t, body, status, http.StatusNotImplemented, "NOT_IMPLEMENTED")
}

// fakeMessageService lets validation-path tests (bad limit) run without a
// real store.
type fakeMessageService struct {
	gotFilter messagesvc.ListFilter
	rows      []domain.SessionMessageRecord
	err       error
}

func (f *fakeMessageService) ListProjectMessages(_ context.Context, filter messagesvc.ListFilter) ([]domain.SessionMessageRecord, error) {
	f.gotFilter = filter
	return f.rows, f.err
}

func newMessagesFakeTestServer(t *testing.T, svc *fakeMessageService) *httptest.Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{
		Messages: svc,
	}, httpd.ControlDeps{}))
	t.Cleanup(srv.Close)
	return srv
}

func TestMessagesAPI_InvalidLimitIsBadRequest(t *testing.T) {
	srv := newMessagesFakeTestServer(t, &fakeMessageService{})

	body, status, _ := doRequest(t, srv, "GET", "/api/v1/projects/ao/messages?limit=0", "")
	assertErrorCode(t, body, status, http.StatusBadRequest, "INVALID_LIMIT")

	body, status, _ = doRequest(t, srv, "GET", "/api/v1/projects/ao/messages?limit=nope", "")
	assertErrorCode(t, body, status, http.StatusBadRequest, "INVALID_LIMIT")
}

func TestMessagesAPI_DefaultsAndCapsLimit(t *testing.T) {
	svc := &fakeMessageService{}
	srv := newMessagesFakeTestServer(t, svc)

	_, status, _ := doRequest(t, srv, "GET", "/api/v1/projects/ao/messages", "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if svc.gotFilter.ProjectID != "ao" || svc.gotFilter.Limit != messagesvc.DefaultListLimit {
		t.Fatalf("filter = %+v, want project ao and default limit", svc.gotFilter)
	}

	_, status, _ = doRequest(t, srv, "GET", "/api/v1/projects/ao/messages?limit=10000", "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if svc.gotFilter.Limit != messagesvc.MaxListLimit {
		t.Fatalf("limit = %d, want capped at %d", svc.gotFilter.Limit, messagesvc.MaxListLimit)
	}
}

// TestMessagesAPI_ListsPersistedMessagesForProjectNewestFirst is the
// end-to-end read-side check: messages persisted through the real sqlite
// store (as Send would after a successful delivery) come back through
// GET /projects/{id}/messages scoped to the target's project and ordered
// newest first, and a message targeting a different project is excluded.
func TestMessagesAPI_ListsPersistedMessagesForProjectNewestFirst(t *testing.T) {
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	if err := store.UpsertProject(ctx, domain.ProjectRecord{ID: "mer", Path: "/tmp/mer", RegisteredAt: now}); err != nil {
		t.Fatalf("seed project mer: %v", err)
	}
	if err := store.UpsertProject(ctx, domain.ProjectRecord{ID: "other", Path: "/tmp/other", RegisteredAt: now}); err != nil {
		t.Fatalf("seed project other: %v", err)
	}
	merSession, err := store.CreateSession(ctx, domain.SessionRecord{
		ProjectID: "mer", Kind: domain.KindWorker, Harness: domain.HarnessClaudeCode,
		Activity: domain.Activity{State: domain.ActivityActive, LastActivityAt: now}, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("seed mer session: %v", err)
	}
	otherSession, err := store.CreateSession(ctx, domain.SessionRecord{
		ProjectID: "other", Kind: domain.KindWorker, Harness: domain.HarnessClaudeCode,
		Activity: domain.Activity{State: domain.ActivityActive, LastActivityAt: now}, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("seed other session: %v", err)
	}

	msgs := []domain.SessionMessageRecord{
		{ID: "m1", TargetSessionID: merSession.ID, Content: "first", CreatedAt: now},
		{ID: "m2", SenderSessionID: merSession.ID, TargetSessionID: merSession.ID, Content: "second", CreatedAt: now.Add(time.Second)},
		{ID: "m3", TargetSessionID: otherSession.ID, Content: "other project", CreatedAt: now.Add(2 * time.Second)},
	}
	for _, m := range msgs {
		if err := store.InsertSessionMessage(ctx, m); err != nil {
			t.Fatalf("insert %s: %v", m.ID, err)
		}
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{
		Messages: messagesvc.New(messagesvc.Deps{Store: store}),
	}, httpd.ControlDeps{}))
	t.Cleanup(srv.Close)

	body, status, headers := doRequest(t, srv, "GET", "/api/v1/projects/mer/messages", "")
	if status != http.StatusOK {
		t.Fatalf("GET messages = %d, want 200; body=%s", status, body)
	}
	assertJSON(t, headers)

	var got struct {
		Messages []controllers.SessionMessage `json:"messages"`
	}
	mustJSON(t, body, &got)
	if len(got.Messages) != 2 || got.Messages[0].ID != "m2" || got.Messages[1].ID != "m1" {
		t.Fatalf("messages = %+v, want [m2, m1] (newest first, other project excluded)", got.Messages)
	}
	if got.Messages[0].SenderSessionID != string(merSession.ID) {
		t.Fatalf("m2 senderSessionId = %q, want %s", got.Messages[0].SenderSessionID, merSession.ID)
	}
	if got.Messages[1].SenderSessionID != "" {
		t.Fatalf("m1 senderSessionId = %q, want empty (human sender)", got.Messages[1].SenderSessionID)
	}
}
