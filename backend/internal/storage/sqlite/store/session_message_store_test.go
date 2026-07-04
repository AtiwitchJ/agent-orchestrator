package store_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/cdc"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// TestInsertSessionMessage_PersistsAndEmitsChangeLogRow is the primary
// correctness signal for migration 0023's change_log rebuild: it asserts a
// session_message_created row actually lands in change_log via the new
// session_messages_cdc_insert trigger, and that the payload resolves
// project_id from the TARGET session (not the sender) and omits content.
func TestInsertSessionMessage_PersistsAndEmitsChangeLogRow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedProject(t, s, "mer")
	seedProject(t, s, "other")

	sender, err := s.CreateSession(ctx, sampleRecord("other"))
	if err != nil {
		t.Fatal(err)
	}
	target, err := s.CreateSession(ctx, sampleRecord("mer"))
	if err != nil {
		t.Fatal(err)
	}

	base, err := s.LatestSeq(ctx)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	rec := domain.SessionMessageRecord{
		ID:              "msg_1",
		SenderSessionID: sender.ID,
		TargetSessionID: target.ID,
		Content:         "ping, are you still working on the login bug?",
		CreatedAt:       now,
	}
	if err := s.InsertSessionMessage(ctx, rec); err != nil {
		t.Fatalf("insert session message: %v", err)
	}

	evs, err := s.EventsAfter(ctx, base, 100)
	if err != nil {
		t.Fatal(err)
	}
	var found []cdc.Event
	for _, e := range evs {
		if e.Type == cdc.EventSessionMessageCreated {
			found = append(found, e)
		}
	}
	if len(found) != 1 {
		t.Fatalf("session_message_created events = %d, want 1; all=%v", len(found), evs)
	}
	ev := found[0]
	// The event's project_id must resolve from the TARGET session's project
	// ("mer"), not the sender's ("other") — live subscribers of the target's
	// project are the ones who need to see the new message arrive.
	if ev.ProjectID != "mer" {
		t.Fatalf("event project = %s, want mer (target's project)", ev.ProjectID)
	}
	if ev.SessionID != string(target.ID) {
		t.Fatalf("event session = %s, want target session %s", ev.SessionID, target.ID)
	}

	var payload map[string]any
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		t.Fatalf("payload JSON: %v", err)
	}
	if payload["id"] != "msg_1" {
		t.Fatalf("payload id = %v, want msg_1", payload["id"])
	}
	if payload["senderSessionId"] != string(sender.ID) {
		t.Fatalf("payload senderSessionId = %v, want %s", payload["senderSessionId"], sender.ID)
	}
	if payload["targetSessionId"] != string(target.ID) {
		t.Fatalf("payload targetSessionId = %v, want %s", payload["targetSessionId"], target.ID)
	}
	if _, ok := payload["content"]; ok {
		t.Fatalf("payload must not carry content (broadcast over CDC/SSE): %v", payload)
	}
}

// TestInsertSessionMessage_NilSenderMeansHuman covers the "" == human sender
// convention: a NULL sender_session_id round-trips to SessionMessageRecord's
// zero value and the CDC payload's senderSessionId is JSON null.
func TestInsertSessionMessage_NilSenderMeansHuman(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedProject(t, s, "mer")
	target, err := s.CreateSession(ctx, sampleRecord("mer"))
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	rec := domain.SessionMessageRecord{
		ID:              "msg_human",
		TargetSessionID: target.ID,
		Content:         "hello from the dashboard",
		CreatedAt:       now,
	}
	if err := s.InsertSessionMessage(ctx, rec); err != nil {
		t.Fatalf("insert session message: %v", err)
	}

	list, err := s.ListProjectSessionMessages(ctx, "mer", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].SenderSessionID != "" {
		t.Fatalf("list = %+v, want one row with empty SenderSessionID", list)
	}

	evs, err := s.EventsAfter(ctx, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	for _, e := range evs {
		if e.Type == cdc.EventSessionMessageCreated {
			if err := json.Unmarshal(e.Payload, &payload); err != nil {
				t.Fatalf("payload JSON: %v", err)
			}
		}
	}
	if payload == nil {
		t.Fatal("no session_message_created event found")
	}
	if payload["senderSessionId"] != nil {
		t.Fatalf("senderSessionId = %v, want JSON null for a human sender", payload["senderSessionId"])
	}
}

// TestListProjectSessionMessages_ScopesToTargetProjectAndOrdersNewestFirst
// covers the join-on-target + ordering + limit contract of
// ListProjectSessionMessages.
func TestListProjectSessionMessages_ScopesToTargetProjectAndOrdersNewestFirst(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedProject(t, s, "mer")
	seedProject(t, s, "other")

	merTarget, err := s.CreateSession(ctx, sampleRecord("mer"))
	if err != nil {
		t.Fatal(err)
	}
	otherTarget, err := s.CreateSession(ctx, sampleRecord("other"))
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	msgs := []domain.SessionMessageRecord{
		{ID: "m1", TargetSessionID: merTarget.ID, Content: "first", CreatedAt: now},
		{ID: "m2", TargetSessionID: merTarget.ID, Content: "second", CreatedAt: now.Add(time.Second)},
		{ID: "m3", TargetSessionID: otherTarget.ID, Content: "other project", CreatedAt: now.Add(2 * time.Second)},
	}
	for _, m := range msgs {
		if err := s.InsertSessionMessage(ctx, m); err != nil {
			t.Fatalf("insert %s: %v", m.ID, err)
		}
	}

	got, err := s.ListProjectSessionMessages(ctx, "mer", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "m2" || got[1].ID != "m1" {
		t.Fatalf("mer messages = %+v, want [m2, m1] (newest first, other project excluded)", got)
	}

	limited, err := s.ListProjectSessionMessages(ctx, "mer", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 1 || limited[0].ID != "m2" {
		t.Fatalf("limited mer messages = %+v, want [m2]", limited)
	}
}
