package message

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type fakeStore struct {
	gotProject domain.ProjectID
	gotLimit   int
	rows       []domain.SessionMessageRecord
	err        error
}

func (f *fakeStore) ListProjectSessionMessages(_ context.Context, project domain.ProjectID, limit int) ([]domain.SessionMessageRecord, error) {
	f.gotProject = project
	f.gotLimit = limit
	return f.rows, f.err
}

func TestListProjectMessagesAppliesDefaultLimit(t *testing.T) {
	st := &fakeStore{}
	mgr := New(Deps{Store: st})
	if _, err := mgr.ListProjectMessages(context.Background(), ListFilter{ProjectID: "mer"}); err != nil {
		t.Fatalf("ListProjectMessages: %v", err)
	}
	if st.gotProject != "mer" || st.gotLimit != DefaultListLimit {
		t.Fatalf("project=%s limit=%d, want mer/%d", st.gotProject, st.gotLimit, DefaultListLimit)
	}
}

func TestListProjectMessagesCapsLimit(t *testing.T) {
	st := &fakeStore{}
	mgr := New(Deps{Store: st})
	if _, err := mgr.ListProjectMessages(context.Background(), ListFilter{ProjectID: "mer", Limit: 100000}); err != nil {
		t.Fatalf("ListProjectMessages: %v", err)
	}
	if st.gotLimit != MaxListLimit {
		t.Fatalf("limit = %d, want capped at %d", st.gotLimit, MaxListLimit)
	}
}

func TestListProjectMessagesPassesThroughRows(t *testing.T) {
	now := time.Now().UTC()
	st := &fakeStore{rows: []domain.SessionMessageRecord{
		{ID: "m1", TargetSessionID: "mer-1", Content: "hi", CreatedAt: now},
	}}
	mgr := New(Deps{Store: st})
	got, err := mgr.ListProjectMessages(context.Background(), ListFilter{ProjectID: "mer", Limit: 5})
	if err != nil {
		t.Fatalf("ListProjectMessages: %v", err)
	}
	if len(got) != 1 || got[0].ID != "m1" {
		t.Fatalf("messages = %+v", got)
	}
	if st.gotLimit != 5 {
		t.Fatalf("limit = %d, want 5 (under cap, passed through)", st.gotLimit)
	}
}

func TestListProjectMessagesRequiresStore(t *testing.T) {
	_, err := New(Deps{}).ListProjectMessages(context.Background(), ListFilter{ProjectID: "mer"})
	if err == nil {
		t.Fatal("want missing store error")
	}
}
