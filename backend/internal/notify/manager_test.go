package notify

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
)

type fakeStore struct {
	rows      []domain.NotificationRecord
	duplicate bool
	err       error
}

func (f *fakeStore) CreateNotification(_ context.Context, rec domain.NotificationRecord) (domain.NotificationRecord, bool, error) {
	if f.err != nil {
		return domain.NotificationRecord{}, false, f.err
	}
	if f.duplicate {
		return domain.NotificationRecord{}, false, nil
	}
	f.rows = append(f.rows, rec)
	return rec, true, nil
}

func TestManagerNotifyPersistsThenPublishes(t *testing.T) {
	st := &fakeStore{}
	hub := NewHub()
	ch, unsub := hub.Subscribe("")
	defer unsub()
	now := time.Date(2026, 6, 11, 10, 0, 0, 0, time.UTC)
	mgr := New(Deps{Store: st, Publisher: hub, Clock: func() time.Time { return now }, NewID: func() string { return "ntf_1" }})

	if err := mgr.Notify(context.Background(), Intent{Type: domain.NotificationNeedsInput, SessionID: "mer-1", ProjectID: "mer", SessionDisplayName: "checkout-flow", Detail: "May I run tests?"}); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if len(st.rows) != 1 {
		t.Fatalf("stored rows = %d, want 1", len(st.rows))
	}
	if got := st.rows[0]; got.ID != "ntf_1" || got.CreatedAt != now || got.Status != domain.NotificationUnread || got.Title != "checkout-flow needs input" {
		t.Fatalf("stored notification = %+v", got)
	}
	if got := st.rows[0].Body; got != "May I run tests?" {
		t.Fatalf("stored body = %q", got)
	}
	select {
	case got := <-ch:
		if got.ID != "ntf_1" {
			t.Fatalf("published = %+v", got)
		}
	default:
		t.Fatal("expected published notification")
	}
}

// TestManagerNotifyAutoTerminatedNeedsNoPRURL pins the enrich.go exemption:
// auto_terminated is session-centric (the stallmon audit trail for an
// auto-kill), so — like needs_input — it must persist without a PR URL.
func TestManagerNotifyAutoTerminatedNeedsNoPRURL(t *testing.T) {
	st := &fakeStore{}
	mgr := New(Deps{Store: st, Clock: func() time.Time { return time.Now() }, NewID: func() string { return "ntf_1" }})

	err := mgr.Notify(context.Background(), Intent{
		Type:               domain.NotificationAutoTerminated,
		SessionID:          "mer-1",
		ProjectID:          "mer",
		SessionDisplayName: "checkout-flow",
	})
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if len(st.rows) != 1 {
		t.Fatalf("stored rows = %d, want 1", len(st.rows))
	}
	got := st.rows[0]
	if got.PRURL != "" {
		t.Fatalf("PRURL = %q, want empty", got.PRURL)
	}
	if got.Title != "checkout-flow was auto-terminated (stalled)" {
		t.Fatalf("Title = %q", got.Title)
	}
	if got.Body == "" {
		t.Fatal("Body is empty, want a stalled-session explanation")
	}
}

func TestManagerNotifyDuplicateDoesNotPublish(t *testing.T) {
	st := &fakeStore{duplicate: true}
	hub := NewHub()
	ch, unsub := hub.Subscribe("")
	defer unsub()
	mgr := New(Deps{Store: st, Publisher: hub, Clock: func() time.Time { return time.Now() }, NewID: func() string { return "ntf_1" }})

	if err := mgr.Notify(context.Background(), Intent{Type: domain.NotificationNeedsInput, SessionID: "mer-1", ProjectID: "mer", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("Notify duplicate: %v", err)
	}
	select {
	case got := <-ch:
		t.Fatalf("duplicate published %+v", got)
	default:
	}
}

func TestManagerNotifyRejectsUnknownType(t *testing.T) {
	mgr := New(Deps{Store: &fakeStore{}, Clock: func() time.Time { return time.Now() }})
	err := mgr.Notify(context.Background(), Intent{Type: "surprise", SessionID: "mer-1", ProjectID: "mer"})
	if !errors.Is(err, domain.ErrInvalidNotificationType) {
		t.Fatalf("err = %v, want invalid type", err)
	}
}

func TestHubProjectFilter(t *testing.T) {
	hub := NewHub()
	ch, unsub := hub.Subscribe("mer")
	defer unsub()
	_ = hub.Publish(context.Background(), domain.NotificationRecord{ID: "skip", ProjectID: "ao"})
	_ = hub.Publish(context.Background(), domain.NotificationRecord{ID: "keep", ProjectID: "mer"})
	select {
	case got := <-ch:
		if got.ID != "keep" {
			t.Fatalf("published = %+v", got)
		}
	default:
		t.Fatal("expected filtered notification")
	}
}
