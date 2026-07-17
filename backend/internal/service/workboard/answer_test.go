package workboard

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
)

func TestAnswererReconcileProject_WaitsForConfiguredTimeout(t *testing.T) {
	now := time.Date(2026, time.July, 17, 9, 10, 0, 0, time.UTC)
	store := &answerStore{
		project: domain.ProjectRecord{ID: "p1", Config: domain.ProjectConfig{Workboard: domain.WorkboardConfig{AnswerTimeoutMinutes: 10}}},
		cards: []domain.WorkCard{{
			ID: "card-1", ProjectID: "p1", BoardID: defaultBoardID, Title: "Ship the API", Notes: "Add coverage for the handler.",
			Status: domain.CardStatusRunning, SessionID: "worker-1",
		}},
		sessions: []domain.SessionRecord{
			{ID: "worker-1", ProjectID: "p1", Kind: domain.KindWorker, Activity: domain.Activity{State: domain.ActivityWaitingInput, LastActivityAt: now.Add(-9 * time.Minute)}},
			{ID: "hermes-1", ProjectID: "p1", Kind: domain.KindOrchestrator, Harness: domain.HarnessHermes},
		},
		notifications: []domain.NotificationRecord{{
			ID: "notice-1", SessionID: "worker-1", ProjectID: "p1", Type: domain.NotificationNeedsInput,
			Title: "Worker needs input", Body: "May I run the test suite before continuing?", Status: domain.NotificationUnread,
			CreatedAt: now.Add(-9 * time.Minute),
		}},
	}
	sender := newAnswerSender(store, func() time.Time { return now })
	answerer := NewAnswerer(AnswerDeps{Store: store, Sender: sender, Clock: func() time.Time { return now }, NewID: func() string { return "event-1" }})

	if answered, err := answerer.ReconcileProject(context.Background(), "p1"); err != nil {
		t.Fatalf("ReconcileProject before timeout: %v", err)
	} else if len(answered) != 0 {
		t.Fatalf("answered before timeout = %v, want none", answered)
	}
	if !store.cards[0].WaitingForInput {
		t.Fatal("card should be waiting for input before timeout")
	}
	if len(sender.sent) != 0 || len(store.events) != 0 {
		t.Fatalf("before timeout sent=%v events=%v, want neither", sender.sent, store.events)
	}

	now = now.Add(time.Minute)
	answered, err := answerer.ReconcileProject(context.Background(), "p1")
	if err != nil {
		t.Fatalf("ReconcileProject at timeout: %v", err)
	}
	if len(answered) != 1 || answered[0] != "card-1" {
		t.Fatalf("answered = %v, want [card-1]", answered)
	}
	if store.cards[0].WaitingForInput {
		t.Fatal("card should clear waiting_for_input after Hermes handoff")
	}
	if len(sender.sent) != 1 || sender.sent[0].target != "hermes-1" {
		t.Fatalf("Hermes sends = %#v, want one send to hermes-1", sender.sent)
	}
	if got := sender.sent[0].message; !containsAll(got, "worker-1", "Ship the API", "May I run the test suite") {
		t.Fatalf("grounded Hermes prompt = %q", got)
	}
	if len(store.events) != 2 || store.events[0].Kind != hermesAnswerRequestedEventKind || store.events[1].Kind != hermesAnswerEventKind {
		t.Fatalf("events = %#v, want prepared and completed Hermes answer events", store.events)
	}
	if repeated, err := answerer.ReconcileProject(context.Background(), "p1"); err != nil {
		t.Fatalf("ReconcileProject after handoff: %v", err)
	} else if len(repeated) != 0 || len(sender.sent) != 1 || len(store.events) != 2 || store.cards[0].WaitingForInput {
		t.Fatalf("completed answer replay = answered:%v sent:%v events:%v waiting:%t, want no replay", repeated, sender.sent, store.events, store.cards[0].WaitingForInput)
	}
}

func TestAnswererReconcileProject_StockNeedsInputRemainsWaitingWithoutQuestionDetail(t *testing.T) {
	now := time.Date(2026, time.July, 17, 9, 10, 0, 0, time.UTC)
	store := &answerStore{
		project: domain.ProjectRecord{ID: "p1", Config: domain.ProjectConfig{Workboard: domain.WorkboardConfig{AnswerTimeoutMinutes: 10}}},
		cards:   []domain.WorkCard{{ID: "card-1", ProjectID: "p1", BoardID: defaultBoardID, Status: domain.CardStatusRunning, SessionID: "worker-1"}},
		sessions: []domain.SessionRecord{
			{ID: "worker-1", ProjectID: "p1", Kind: domain.KindWorker, Activity: domain.Activity{State: domain.ActivityWaitingInput, LastActivityAt: now.Add(-time.Hour)}},
			{ID: "hermes-1", ProjectID: "p1", Kind: domain.KindOrchestrator, Harness: domain.HarnessHermes},
		},
		notifications: []domain.NotificationRecord{{
			ID: "notice-1", SessionID: "worker-1", ProjectID: "p1", Type: domain.NotificationNeedsInput,
			Title: "worker-1 needs input", Body: "The agent is waiting for your response.", Status: domain.NotificationUnread,
			CreatedAt: now.Add(-time.Hour),
		}},
	}
	sender := &answerSender{}
	answerer := NewAnswerer(AnswerDeps{Store: store, Sender: sender, Clock: func() time.Time { return now }})

	answered, err := answerer.ReconcileProject(context.Background(), "p1")
	if err != nil {
		t.Fatalf("ReconcileProject: %v", err)
	}
	if len(answered) != 0 || len(sender.sent) != 0 || len(store.events) != 0 {
		t.Fatalf("stock notification answer = answered:%v sent:%v events:%v, want none", answered, sender.sent, store.events)
	}
	if !store.cards[0].WaitingForInput {
		t.Fatal("stock notification should leave card waiting for user input")
	}
}

func TestAnswererReconcileProject_CurrentActivityClearsStaleNotification(t *testing.T) {
	now := time.Date(2026, time.July, 17, 9, 10, 0, 0, time.UTC)
	for _, state := range []domain.ActivityState{domain.ActivityActive, domain.ActivityIdle} {
		t.Run(string(state), func(t *testing.T) {
			store := &answerStore{
				project: domain.ProjectRecord{ID: "p1"},
				cards:   []domain.WorkCard{{ID: "card-1", ProjectID: "p1", BoardID: defaultBoardID, Status: domain.CardStatusRunning, SessionID: "worker-1", WaitingForInput: true}},
				sessions: []domain.SessionRecord{
					{ID: "worker-1", ProjectID: "p1", Kind: domain.KindWorker, Activity: domain.Activity{State: state, LastActivityAt: now}},
					{ID: "hermes-1", ProjectID: "p1", Kind: domain.KindOrchestrator, Harness: domain.HarnessHermes},
				},
				notifications: []domain.NotificationRecord{{
					ID: "notice-1", SessionID: "worker-1", ProjectID: "p1", Type: domain.NotificationNeedsInput,
					Title: "worker-1 needs input", Body: "May I run the test suite before continuing?", Status: domain.NotificationUnread,
					CreatedAt: now.Add(-time.Hour),
				}},
			}
			answerer := NewAnswerer(AnswerDeps{Store: store, Sender: &answerSender{}, Clock: func() time.Time { return now }})

			if answered, err := answerer.ReconcileProject(context.Background(), "p1"); err != nil {
				t.Fatalf("ReconcileProject: %v", err)
			} else if len(answered) != 0 {
				t.Fatalf("answered = %v, want none", answered)
			}
			if store.cards[0].WaitingForInput {
				t.Fatal("current active/idle state should clear stale waiting badge")
			}
		})
	}
}

func TestAnswererReconcileProject_IgnoresDetailFromOlderWaitingEpisode(t *testing.T) {
	now := time.Date(2026, time.July, 17, 9, 10, 0, 0, time.UTC)
	store := &answerStore{
		project: domain.ProjectRecord{ID: "p1", Config: domain.ProjectConfig{Workboard: domain.WorkboardConfig{AnswerTimeoutMinutes: 10}}},
		cards:   []domain.WorkCard{{ID: "card-1", ProjectID: "p1", BoardID: defaultBoardID, Status: domain.CardStatusRunning, SessionID: "worker-1"}},
		sessions: []domain.SessionRecord{
			{ID: "worker-1", ProjectID: "p1", Kind: domain.KindWorker, Activity: domain.Activity{State: domain.ActivityWaitingInput, LastActivityAt: now.Add(-time.Hour)}},
			{ID: "hermes-1", ProjectID: "p1", Kind: domain.KindOrchestrator, Harness: domain.HarnessHermes},
		},
		notifications: []domain.NotificationRecord{{
			ID: "notice-old", SessionID: "worker-1", ProjectID: "p1", Type: domain.NotificationNeedsInput,
			Body: "May I run the test suite before continuing?", Status: domain.NotificationUnread, CreatedAt: now.Add(-2 * time.Hour),
		}},
	}
	sender := newAnswerSender(store, func() time.Time { return now })
	answerer := NewAnswerer(AnswerDeps{Store: store, Sender: sender, Clock: func() time.Time { return now }, NewID: eventIDs()})

	answered, err := answerer.ReconcileProject(context.Background(), "p1")
	if err != nil {
		t.Fatalf("ReconcileProject: %v", err)
	}
	if len(answered) != 0 || len(sender.sent) != 0 || len(store.events) != 0 {
		t.Fatalf("stale episode answer = answered:%v sent:%v events:%v, want none", answered, sender.sent, store.events)
	}
	if !store.cards[0].WaitingForInput {
		t.Fatal("current waiting state should remain visible without current-episode detail")
	}
}

func TestAnswererReconcileProject_UsesCurrentReadNotificationDetail(t *testing.T) {
	now := time.Date(2026, time.July, 17, 9, 10, 0, 0, time.UTC)
	store := answerStoreWithQuestion(now, domain.WorkboardConfig{AnswerTimeoutMinutes: 10})
	store.notifications[0].Status = domain.NotificationRead
	sender := newAnswerSender(store, func() time.Time { return now })
	answerer := NewAnswerer(AnswerDeps{Store: store, Sender: sender, Clock: func() time.Time { return now }, NewID: eventIDs()})

	answered, err := answerer.ReconcileProject(context.Background(), "p1")
	if err != nil {
		t.Fatalf("ReconcileProject: %v", err)
	}
	if len(answered) != 1 || len(sender.sent) != 1 || !containsAll(sender.sent[0].message, "May I run the test suite") {
		t.Fatalf("read notification answer = answered:%v sent:%#v", answered, sender.sent)
	}
}

func TestAnswererReconcileProject_DenylistedIntentNeverAutoAnswers(t *testing.T) {
	now := time.Date(2026, time.July, 17, 9, 10, 0, 0, time.UTC)
	store := &answerStore{
		project: domain.ProjectRecord{ID: "p1", Config: domain.ProjectConfig{Workboard: domain.WorkboardConfig{AnswerTimeoutMinutes: 10}}},
		cards: []domain.WorkCard{{
			ID: "card-1", ProjectID: "p1", BoardID: defaultBoardID, Title: "Release", Notes: "Publish the branch.",
			Status: domain.CardStatusRunning, SessionID: "worker-1",
		}},
		sessions: []domain.SessionRecord{
			{ID: "worker-1", ProjectID: "p1", Kind: domain.KindWorker, Activity: domain.Activity{State: domain.ActivityWaitingInput, LastActivityAt: now.Add(-time.Hour)}},
			{ID: "hermes-1", ProjectID: "p1", Kind: domain.KindOrchestrator, Harness: domain.HarnessHermes},
		},
		notifications: []domain.NotificationRecord{{
			ID: "notice-1", SessionID: "worker-1", ProjectID: "p1", Type: domain.NotificationNeedsInput,
			Title: "Worker needs input", Body: "May I git push --force the release branch?", Status: domain.NotificationUnread,
			CreatedAt: now.Add(-time.Hour),
		}},
	}
	sender := &answerSender{}
	answerer := NewAnswerer(AnswerDeps{Store: store, Sender: sender, Clock: func() time.Time { return now }})

	answered, err := answerer.ReconcileProject(context.Background(), "p1")
	if err != nil {
		t.Fatalf("ReconcileProject: %v", err)
	}
	if len(answered) != 0 || len(sender.sent) != 0 || len(store.events) != 0 {
		t.Fatalf("denylisted answer = answered:%v sent:%v events:%v, want none", answered, sender.sent, store.events)
	}
	if !store.cards[0].WaitingForInput {
		t.Fatal("denylisted card should remain waiting for a user")
	}
}

func TestAnswererReconcileProject_PreparedAnswerDoesNotRetryAmbiguousSend(t *testing.T) {
	now := time.Date(2026, time.July, 17, 9, 10, 0, 0, time.UTC)
	store := answerStoreWithQuestion(now, domain.WorkboardConfig{AnswerTimeoutMinutes: 10})
	sender := &answerSender{err: context.DeadlineExceeded}
	answerer := NewAnswerer(AnswerDeps{Store: store, Sender: sender, Clock: func() time.Time { return now }, NewID: eventIDs()})

	if _, err := answerer.ReconcileProject(context.Background(), "p1"); err == nil {
		t.Fatal("ReconcileProject error = nil, want send error")
	}
	sender.err = nil
	if answered, err := answerer.ReconcileProject(context.Background(), "p1"); err != nil {
		t.Fatalf("ReconcileProject retry: %v", err)
	} else if len(answered) != 0 {
		t.Fatalf("retry answered = %v, want none", answered)
	}
	if len(sender.sent) != 1 || len(store.events) != 1 || store.events[0].Kind != hermesAnswerRequestedEventKind {
		t.Fatalf("ambiguous send sent=%#v events=%#v, want one prepared attempt and no replay", sender.sent, store.events)
	}
	if !store.cards[0].WaitingForInput {
		t.Fatal("ambiguous delivery should remain visible as waiting for user input")
	}
}

func TestAnswererReconcileProject_RequiresDurablePostSendEvidence(t *testing.T) {
	now := time.Date(2026, time.July, 17, 9, 10, 0, 0, time.UTC)
	store := answerStoreWithQuestion(now, domain.WorkboardConfig{AnswerTimeoutMinutes: 10})
	sender := &answerSender{} // Simulates a delivered runtime send whose message-store write failed.
	answerer := NewAnswerer(AnswerDeps{Store: store, Sender: sender, Clock: func() time.Time { return now }, NewID: eventIDs()})

	answered, err := answerer.ReconcileProject(context.Background(), "p1")
	if err != nil {
		t.Fatalf("ReconcileProject: %v", err)
	}
	if len(answered) != 0 || len(sender.sent) != 1 || len(store.events) != 1 || store.events[0].Kind != hermesAnswerRequestedEventKind || !store.cards[0].WaitingForInput {
		t.Fatalf("unproven send = answered:%v sent:%v events:%v waiting:%t", answered, sender.sent, store.events, store.cards[0].WaitingForInput)
	}
	if answered, err = answerer.ReconcileProject(context.Background(), "p1"); err != nil {
		t.Fatalf("ReconcileProject repeat: %v", err)
	} else if len(answered) != 0 || len(sender.sent) != 1 || len(store.events) != 1 {
		t.Fatalf("unproven send replay = answered:%v sent:%v events:%v", answered, sender.sent, store.events)
	}
}

func TestAnswererReconcileProject_ConfirmsPreparedAnswerFromDurableMessage(t *testing.T) {
	now := time.Date(2026, time.July, 17, 9, 10, 0, 0, time.UTC)
	store := answerStoreWithQuestion(now, domain.WorkboardConfig{AnswerTimeoutMinutes: 10})
	waitingAt := store.sessions[0].Activity.LastActivityAt
	payload := hermesAnswerPayload{
		AttemptID: "attempt-1", WorkerSessionID: "worker-1", HermesSessionID: "hermes-1",
		Question: "May I run the test suite before continuing?", WaitingAt: waitingAt.Format(time.RFC3339Nano),
	}
	payload.Prompt, _ = hermesAnswerPrompt(store.cards[0], "worker-1", payload.Question)
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	store.events = append(store.events, domain.WorkCardEvent{ID: payload.AttemptID, CardID: "card-1", ProjectID: "p1", Kind: hermesAnswerRequestedEventKind, Payload: string(payloadJSON), CreatedAt: now.Add(-time.Minute)})
	store.messages = append(store.messages, domain.SessionMessageRecord{ID: "msg-1", TargetSessionID: "hermes-1", Content: payload.Prompt, CreatedAt: now})
	sender := newAnswerSender(store, func() time.Time { return now })
	answerer := NewAnswerer(AnswerDeps{Store: store, Sender: sender, Clock: func() time.Time { return now }, NewID: eventIDs()})

	answered, err := answerer.ReconcileProject(context.Background(), "p1")
	if err != nil {
		t.Fatalf("ReconcileProject: %v", err)
	}
	if len(answered) != 1 || len(sender.sent) != 0 || store.cards[0].WaitingForInput {
		t.Fatalf("self-heal = answered:%v sent:%v waiting:%t", answered, sender.sent, store.cards[0].WaitingForInput)
	}
	if len(store.events) != 2 || store.events[1].Kind != hermesAnswerEventKind || !strings.Contains(store.events[1].Payload, `"attemptId":"attempt-1"`) {
		t.Fatalf("events = %#v, want completion linked to attempt-1", store.events)
	}
}

func TestAnswererReconcileProject_RetriesOnlyForNewWaitingEpisode(t *testing.T) {
	now := time.Date(2026, time.July, 17, 9, 10, 0, 0, time.UTC)
	store := answerStoreWithQuestion(now, domain.WorkboardConfig{AnswerTimeoutMinutes: 10})
	oldPayload := hermesAnswerPayload{
		AttemptID: "old-attempt", WorkerSessionID: "worker-1", HermesSessionID: "hermes-1",
		Prompt: "old Hermes prompt", WaitingAt: now.Add(-2 * time.Hour).Format(time.RFC3339Nano),
	}
	oldPayloadJSON, err := json.Marshal(oldPayload)
	if err != nil {
		t.Fatalf("marshal old payload: %v", err)
	}
	store.events = append(store.events, domain.WorkCardEvent{ID: oldPayload.AttemptID, CardID: "card-1", ProjectID: "p1", Kind: hermesAnswerRequestedEventKind, Payload: string(oldPayloadJSON), CreatedAt: now.Add(-2 * time.Hour)})
	sender := newAnswerSender(store, func() time.Time { return now })
	answerer := NewAnswerer(AnswerDeps{Store: store, Sender: sender, Clock: func() time.Time { return now }, NewID: eventIDs()})

	answered, err := answerer.ReconcileProject(context.Background(), "p1")
	if err != nil {
		t.Fatalf("ReconcileProject: %v", err)
	}
	if len(answered) != 1 || len(sender.sent) != 1 || len(store.events) != 3 {
		t.Fatalf("new episode retry = answered:%v sent:%v events:%v", answered, sender.sent, store.events)
	}
	if strings.Contains(sender.sent[0].message, oldPayload.Prompt) {
		t.Fatalf("replayed old prompt: %q", sender.sent[0].message)
	}
}

func TestAnswererReconcileProject_OldCompletedAttemptDoesNotConsumeCurrentAutonomy(t *testing.T) {
	now := time.Date(2026, time.July, 17, 9, 10, 0, 0, time.UTC)
	store := answerStoreWithQuestion(now, domain.WorkboardConfig{Autonomous: domain.WorkboardAutonomousConfig{Enabled: true, Mode: "skip_timeout", Sticky: false}})
	store.notifications[0].Body = "The agent is waiting for your response."
	oldPayload := hermesAnswerPayload{AttemptID: "old-attempt", WorkerSessionID: "worker-1", HermesSessionID: "hermes-1", WaitingAt: now.Add(-2 * time.Hour).Format(time.RFC3339Nano)}
	oldPayloadJSON, err := json.Marshal(oldPayload)
	if err != nil {
		t.Fatalf("marshal old payload: %v", err)
	}
	store.events = []domain.WorkCardEvent{
		{ID: oldPayload.AttemptID, CardID: "card-1", ProjectID: "p1", Kind: hermesAnswerRequestedEventKind, Payload: string(oldPayloadJSON), CreatedAt: now.Add(-2 * time.Hour)},
		{ID: "old-complete", CardID: "card-1", ProjectID: "p1", Kind: hermesAnswerEventKind, Payload: string(oldPayloadJSON), CreatedAt: now.Add(-2*time.Hour + time.Minute)},
	}
	sender := newAnswerSender(store, func() time.Time { return now })
	answerer := NewAnswerer(AnswerDeps{Store: store, Sender: sender, Clock: func() time.Time { return now }, NewID: eventIDs()})

	answered, err := answerer.ReconcileProject(context.Background(), "p1")
	if err != nil {
		t.Fatalf("ReconcileProject: %v", err)
	}
	if len(answered) != 0 || !store.cards[0].WaitingForInput || !store.project.Config.Workboard.Autonomous.Enabled {
		t.Fatalf("old completion affected current episode: answered=%v waiting=%t autonomous=%+v", answered, store.cards[0].WaitingForInput, store.project.Config.Workboard.Autonomous)
	}
}

func TestAnswererReconcileProject_OlderAttemptSelfHealDoesNotConsumeFreshOneShot(t *testing.T) {
	now := time.Date(2026, time.July, 17, 9, 10, 0, 0, time.UTC)
	store := answerStoreWithQuestion(now, domain.WorkboardConfig{AnswerTimeoutMinutes: 10})
	sender := &answerSender{} // The initial send has no durable message evidence yet.
	answerer := NewAnswerer(AnswerDeps{Store: store, Sender: sender, Clock: func() time.Time { return now }, NewID: eventIDs()})

	if answered, err := answerer.ReconcileProject(context.Background(), "p1"); err != nil || len(answered) != 0 || len(store.events) != 1 {
		t.Fatalf("initial ambiguous attempt = answered:%v err:%v events:%#v", answered, err, store.events)
	}
	var payload hermesAnswerPayload
	if err := json.Unmarshal([]byte(store.events[0].Payload), &payload); err != nil {
		t.Fatalf("unmarshal prepared payload: %v", err)
	}
	if payload.ConsumedOneShot {
		t.Fatalf("initial timeout attempt consumed one-shot = true, want false")
	}

	store.project.Config.Workboard.Autonomous = domain.WorkboardAutonomousConfig{Enabled: true, Mode: "skip_timeout", Sticky: false}
	store.messages = append(store.messages, domain.SessionMessageRecord{ID: "message-1", TargetSessionID: "hermes-1", Content: payload.Prompt, CreatedAt: now})
	answered, err := answerer.ReconcileProject(context.Background(), "p1")
	if err != nil {
		t.Fatalf("ReconcileProject self-heal: %v", err)
	}
	if len(answered) != 1 || !store.project.Config.Workboard.Autonomous.Enabled {
		t.Fatalf("self-healed older attempt consumed fresh one-shot: answered=%v autonomous=%+v", answered, store.project.Config.Workboard.Autonomous)
	}
}

func TestAnswererReconcileProject_ConsumesNonStickyAutonomousOverride(t *testing.T) {
	now := time.Date(2026, time.July, 17, 9, 10, 0, 0, time.UTC)
	store := answerStoreWithQuestion(now, domain.WorkboardConfig{Autonomous: domain.WorkboardAutonomousConfig{Enabled: true, Mode: "skip_timeout", Sticky: false}})
	sender := newAnswerSender(store, func() time.Time { return now })
	answerer := NewAnswerer(AnswerDeps{Store: store, Sender: sender, Clock: func() time.Time { return now }, NewID: eventIDs()})

	answered, err := answerer.ReconcileProject(context.Background(), "p1")
	if err != nil {
		t.Fatalf("ReconcileProject: %v", err)
	}
	if len(answered) != 1 || store.project.Config.Workboard.Autonomous.Enabled {
		t.Fatalf("answered=%v autonomous=%+v, want answer and consumed one-shot override", answered, store.project.Config.Workboard.Autonomous)
	}
	var payload hermesAnswerPayload
	if err := json.Unmarshal([]byte(store.events[0].Payload), &payload); err != nil {
		t.Fatalf("unmarshal prepared payload: %v", err)
	}
	if !payload.ConsumedOneShot {
		t.Fatalf("prepared payload = %+v, want consumed one-shot marker", payload)
	}
}

func TestAnswererReconcileProject_PrepareFailureDoesNotSpendOneShot(t *testing.T) {
	now := time.Date(2026, time.July, 17, 9, 10, 0, 0, time.UTC)
	store := answerStoreWithQuestion(now, domain.WorkboardConfig{Autonomous: domain.WorkboardAutonomousConfig{Enabled: true, Mode: "skip_timeout", Sticky: false}})
	store.prepareErr = errors.New("write failed")
	answerer := NewAnswerer(AnswerDeps{Store: store, Sender: &answerSender{}, Clock: func() time.Time { return now }, NewID: eventIDs()})

	if _, err := answerer.ReconcileProject(context.Background(), "p1"); err == nil {
		t.Fatal("ReconcileProject error = nil, want prepare failure")
	}
	if !store.project.Config.Workboard.Autonomous.Enabled || len(store.events) != 0 {
		t.Fatalf("prepare failure spent one-shot or recorded attempt: autonomous=%+v events=%+v", store.project.Config.Workboard.Autonomous, store.events)
	}
}

func TestHermesAnswerPromptSanitizesAndBoundsContext(t *testing.T) {
	question := "May I run the focused tests?\x1b[2J"
	prompt, ok := hermesAnswerPrompt(domain.WorkCard{
		ID: "card-1", Title: "Ship API", Notes: strings.Repeat("界", 3_000), TargetPath: "/workspace/api",
	}, "worker-1", question)
	if !ok {
		t.Fatal("hermesAnswerPrompt rejected a prompt with truncatable context")
	}
	if len(prompt) > maxHermesPromptBytes || strings.ContainsRune(prompt, '\x1b') {
		t.Fatalf("unsafe bounded prompt len=%d prompt=%q", len(prompt), prompt)
	}
	if !strings.Contains(prompt, "May I run the focused tests?[2J") {
		t.Fatalf("prompt did not preserve sanitized question: %q", prompt)
	}
}

func TestHermesAnswerPromptRejectsOversizedQuestion(t *testing.T) {
	if prompt, ok := hermesAnswerPrompt(domain.WorkCard{ID: "card-1"}, "worker-1", strings.Repeat("x", maxHermesPromptBytes)); ok || prompt != "" {
		t.Fatalf("oversized question prompt=%q ok=%t, want rejected", prompt, ok)
	}
}

func answerStoreWithQuestion(now time.Time, config domain.WorkboardConfig) *answerStore {
	return &answerStore{
		project: domain.ProjectRecord{ID: "p1", Config: domain.ProjectConfig{Workboard: config}},
		cards:   []domain.WorkCard{{ID: "card-1", ProjectID: "p1", BoardID: defaultBoardID, Title: "Ship API", Status: domain.CardStatusRunning, SessionID: "worker-1"}},
		sessions: []domain.SessionRecord{
			{ID: "worker-1", ProjectID: "p1", Kind: domain.KindWorker, Activity: domain.Activity{State: domain.ActivityWaitingInput, LastActivityAt: now.Add(-time.Hour)}},
			{ID: "hermes-1", ProjectID: "p1", Kind: domain.KindOrchestrator, Harness: domain.HarnessHermes},
		},
		notifications: []domain.NotificationRecord{{
			ID: "notice-1", SessionID: "worker-1", ProjectID: "p1", Type: domain.NotificationNeedsInput,
			Title: "worker-1 needs input", Body: "May I run the test suite before continuing?", Status: domain.NotificationUnread,
			CreatedAt: now.Add(-time.Hour),
		}},
	}
}

func eventIDs() func() string {
	n := 0
	return func() string {
		n++
		return "event-" + strconv.Itoa(n)
	}
}

type answerStore struct {
	project       domain.ProjectRecord
	cards         []domain.WorkCard
	sessions      []domain.SessionRecord
	notifications []domain.NotificationRecord
	events        []domain.WorkCardEvent
	messages      []domain.SessionMessageRecord
	prepareErr    error
}

func (s *answerStore) GetProject(_ context.Context, id string) (domain.ProjectRecord, bool, error) {
	return s.project, s.project.ID == id, nil
}

func (s *answerStore) UpsertProject(_ context.Context, project domain.ProjectRecord) error {
	s.project = project
	return nil
}

func (s *answerStore) ListWorkCards(_ context.Context, projectID, boardID string) ([]domain.WorkCard, error) {
	var cards []domain.WorkCard
	for _, card := range s.cards {
		if card.ProjectID == projectID && card.BoardID == boardID {
			cards = append(cards, card)
		}
	}
	return cards, nil
}

func (s *answerStore) UpdateWorkCard(_ context.Context, card domain.WorkCard) error {
	for i := range s.cards {
		if s.cards[i].ID == card.ID {
			s.cards[i] = card
			return nil
		}
	}
	return nil
}

func (s *answerStore) ListSessions(_ context.Context, projectID domain.ProjectID) ([]domain.SessionRecord, error) {
	var sessions []domain.SessionRecord
	for _, session := range s.sessions {
		if session.ProjectID == projectID {
			sessions = append(sessions, session)
		}
	}
	return sessions, nil
}

func (s *answerStore) ListRecentNotifications(_ context.Context, _ int) ([]domain.NotificationRecord, error) {
	return append([]domain.NotificationRecord(nil), s.notifications...), nil
}

func (s *answerStore) AppendWorkCardEvent(_ context.Context, event domain.WorkCardEvent) error {
	s.events = append(s.events, event)
	return nil
}

func (s *answerStore) PrepareHermesAnswerAttempt(_ context.Context, project domain.ProjectRecord, event domain.WorkCardEvent, consumeOneShot bool) error {
	if s.prepareErr != nil {
		return s.prepareErr
	}
	if consumeOneShot {
		s.project = project
	}
	s.events = append(s.events, event)
	return nil
}

func (s *answerStore) ListWorkCardEvents(_ context.Context, cardID string) ([]domain.WorkCardEvent, error) {
	var events []domain.WorkCardEvent
	for _, event := range s.events {
		if event.CardID == cardID {
			events = append(events, event)
		}
	}
	return events, nil
}

func (s *answerStore) ListProjectSessionMessages(_ context.Context, projectID domain.ProjectID, _ int) ([]domain.SessionMessageRecord, error) {
	var messages []domain.SessionMessageRecord
	for _, message := range s.messages {
		for _, session := range s.sessions {
			if session.ID == message.TargetSessionID && session.ProjectID == projectID {
				messages = append(messages, message)
				break
			}
		}
	}
	return messages, nil
}

type answerSend struct {
	target  domain.SessionID
	message string
	sender  domain.SessionID
}

type answerSender struct {
	sent  []answerSend
	err   error
	store *answerStore
	now   func() time.Time
}

func (s *answerSender) Send(_ context.Context, target domain.SessionID, message string, sender domain.SessionID) error {
	s.sent = append(s.sent, answerSend{target: target, message: message, sender: sender})
	if s.err == nil && s.store != nil {
		now := time.Now().UTC()
		if s.now != nil {
			now = s.now().UTC()
		}
		s.store.messages = append(s.store.messages, domain.SessionMessageRecord{ID: "message-" + strconv.Itoa(len(s.store.messages)+1), TargetSessionID: target, Content: message, CreatedAt: now})
	}
	return s.err
}

func newAnswerSender(store *answerStore, now func() time.Time) *answerSender {
	return &answerSender{store: store, now: now}
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
