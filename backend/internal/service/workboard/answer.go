package workboard

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/modernagent/modern-agent/backend/internal/domain"
)

// AnswerStore is the durable input and audit surface for answer-on-behalf.
// Current session activity is authoritative; recent needs-input notifications
// may provide question detail without requiring Hermes hooks.
type AnswerStore interface {
	GetProject(ctx context.Context, id string) (domain.ProjectRecord, bool, error)
	ListWorkCards(ctx context.Context, projectID, boardID string) ([]domain.WorkCard, error)
	UpdateWorkCard(ctx context.Context, card domain.WorkCard) error
	ListSessions(ctx context.Context, projectID domain.ProjectID) ([]domain.SessionRecord, error)
	ListRecentNotifications(ctx context.Context, limit int) ([]domain.NotificationRecord, error)
	PrepareHermesAnswerAttempt(ctx context.Context, project domain.ProjectRecord, event domain.WorkCardEvent, consumeOneShot bool) error
	AppendWorkCardEvent(ctx context.Context, event domain.WorkCardEvent) error
	ListWorkCardEvents(ctx context.Context, cardID string) ([]domain.WorkCardEvent, error)
	ListProjectSessionMessages(ctx context.Context, project domain.ProjectID, limit int) ([]domain.SessionMessageRecord, error)
}

// HermesSender is the established session send boundary. The reconciler sends
// Hermes the grounded request; Hermes then answers the linked worker.
type HermesSender interface {
	Send(ctx context.Context, id domain.SessionID, message string, sender domain.SessionID) error
}

// AnswerDeps configures an Answerer.
type AnswerDeps struct {
	Store  AnswerStore
	Sender HermesSender
	Clock  func() time.Time
	NewID  func() string
}

// Answerer watches running cards whose linked worker needs input and asks the
// active Hermes orchestrator to answer once the applicable timeout expires.
type Answerer struct {
	store  AnswerStore
	sender HermesSender
	clock  func() time.Time
	newID  func() string
}

// NewAnswerer constructs the answer-on-behalf reconciler.
func NewAnswerer(d AnswerDeps) *Answerer {
	clock := d.Clock
	if clock == nil {
		clock = time.Now
	}
	newID := d.NewID
	if newID == nil {
		newID = func() string { return "wce_" + uuid.NewString() }
	}
	return &Answerer{store: d.Store, sender: d.Sender, clock: clock, newID: newID}
}

// ReconcileProject derives waiting badges from existing durable session facts
// and recent notifications, then dispatches timed-out safe questions to the
// current Hermes orchestrator. It returns card IDs for successfully handed-off
// answers.
func (a *Answerer) ReconcileProject(ctx context.Context, projectID string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if a.store == nil || a.sender == nil {
		return nil, nil
	}
	project, ok, err := a.store.GetProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("get project %s: %w", projectID, err)
	}
	if !ok {
		return nil, fmt.Errorf("project %s not found", projectID)
	}
	cards, err := a.store.ListWorkCards(ctx, projectID, defaultBoardID)
	if err != nil {
		return nil, fmt.Errorf("list work cards for project %s: %w", projectID, err)
	}
	sessions, err := a.store.ListSessions(ctx, domain.ProjectID(projectID))
	if err != nil {
		return nil, fmt.Errorf("list sessions for project %s: %w", projectID, err)
	}
	notifications, err := a.store.ListRecentNotifications(ctx, 1000)
	if err != nil {
		return nil, fmt.Errorf("list recent notifications: %w", err)
	}
	messages, err := a.store.ListProjectSessionMessages(ctx, domain.ProjectID(projectID), 1000)
	if err != nil {
		return nil, fmt.Errorf("list session messages: %w", err)
	}

	now := a.clock().UTC()
	workers, hermes := answerSessions(sessions)
	questions := needsInputNotifications(notifications, domain.ProjectID(projectID))
	var answered []string
	for _, card := range cards {
		if card.Status != domain.CardStatusRunning || card.SessionID == "" {
			continue
		}
		worker, exists := workers[domain.SessionID(card.SessionID)]
		question, waitingAt, needsInput := needsInput(worker, exists, questions[worker.ID])
		if !needsInput {
			if card.WaitingForInput {
				card.WaitingForInput = false
				card.UpdatedAt = now
				if err := a.store.UpdateWorkCard(ctx, card); err != nil {
					return answered, fmt.Errorf("clear resolved input state for card %s: %w", card.ID, err)
				}
			}
			continue
		}
		events, err := a.store.ListWorkCardEvents(ctx, card.ID)
		if err != nil {
			return answered, fmt.Errorf("list answer events for card %s: %w", card.ID, err)
		}
		attempt := findHermesAnswerAttempt(events, worker.ID, waitingAt)
		if attempt.completed {
			wasWaiting := card.WaitingForInput
			if err := a.completeAnswer(ctx, &card, now); err != nil {
				return answered, err
			}
			if wasWaiting {
				answered = append(answered, card.ID)
			}
			continue
		}
		if !card.WaitingForInput {
			card.WaitingForInput = true
			card.UpdatedAt = now
			if err := a.store.UpdateWorkCard(ctx, card); err != nil {
				return answered, fmt.Errorf("mark card %s waiting for input: %w", card.ID, err)
			}
		}
		if attempt.requested {
			// The persisted attempt payload owns any prior one-shot decision.
			// Do not re-evaluate or consume the project's current override while
			// self-healing it: a user may have enabled a fresh one-shot since.
			// The session service records a message only after its runtime send
			// succeeds. That durable fact closes the crash window between the
			// external send and the work-card completion event.
			if hermesAnswerDelivered(messages, attempt) {
				if err := a.appendHermesAnswer(ctx, card, attempt.payload, now); err != nil {
					return answered, err
				}
				wasWaiting := card.WaitingForInput
				if err := a.completeAnswer(ctx, &card, now); err != nil {
					return answered, err
				}
				if wasWaiting {
					answered = append(answered, card.ID)
				}
			}
			// Missing evidence is ambiguous: the daemon may have delivered the
			// message just before a process/store failure. Keep the card visibly
			// waiting, but never replay that specific external Hermes send.
			continue
		}

		config := answerConfig(project.Config.Workboard)
		if !autoAnswerAllowed(question, config.denylist) || !answerDue(now, waitingAt, config.timeout) || hermes.ID == "" {
			continue
		}

		prompt, ok := hermesAnswerPrompt(card, worker.ID, question)
		if !ok {
			// A truncated question could change the authorization Hermes makes.
			// Leave the card waiting rather than send an unsafe partial request.
			continue
		}
		consumedOneShot := project.Config.Workboard.Autonomous.Enabled && !project.Config.Workboard.Autonomous.Sticky
		if consumedOneShot {
			project.Config.Workboard.Autonomous.Enabled = false
		}
		attemptID := a.newID()
		payload := hermesAnswerPayload{
			AttemptID:       attemptID,
			WorkerSessionID: string(worker.ID),
			HermesSessionID: string(hermes.ID),
			Question:        question,
			Prompt:          prompt,
			WaitingAt:       waitingAt.UTC().Format(time.RFC3339Nano),
			ConsumedOneShot: consumedOneShot,
		}
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			return answered, fmt.Errorf("marshal Hermes answer event for card %s: %w", card.ID, err)
		}
		if err := a.store.PrepareHermesAnswerAttempt(ctx, project, domain.WorkCardEvent{
			ID: attemptID, CardID: card.ID, ProjectID: card.ProjectID, Kind: hermesAnswerRequestedEventKind, Payload: string(payloadJSON), CreatedAt: now,
		}, consumedOneShot); err != nil {
			return answered, fmt.Errorf("prepare Hermes answer event for card %s: %w", card.ID, err)
		}
		if err := a.sender.Send(ctx, hermes.ID, prompt, ""); err != nil {
			// A send error may still represent a delivered runtime write. The
			// session boundary has no reliable pre-delivery error signal, so do
			// not restore this attempt's one-shot authorization.
			return answered, fmt.Errorf("send Hermes answer request for card %s: %w", card.ID, err)
		}
		messages, err = a.store.ListProjectSessionMessages(ctx, domain.ProjectID(projectID), 1000)
		if err != nil {
			return answered, fmt.Errorf("verify Hermes send for card %s: %w", card.ID, err)
		}
		prepared := hermesAnswerAttempt{requested: true, payload: payload, requestedAt: now}
		if !hermesAnswerDelivered(messages, prepared) {
			// Send delivery is externally visible, but without its durable audit
			// fact we cannot safely declare completion or retry it.
			continue
		}
		if err := a.appendHermesAnswer(ctx, card, payload, now); err != nil {
			return answered, err
		}
		if err := a.completeAnswer(ctx, &card, now); err != nil {
			return answered, err
		}
		answered = append(answered, card.ID)
	}
	return answered, nil
}

type answerSettings struct {
	timeout  time.Duration
	denylist []string
}

func answerConfig(config domain.WorkboardConfig) answerSettings {
	defaults := domain.DefaultWorkboardConfig()
	timeoutMinutes := config.AnswerTimeoutMinutes
	if timeoutMinutes <= 0 {
		timeoutMinutes = defaults.AnswerTimeoutMinutes
	}
	if config.Autonomous.Enabled {
		switch config.Autonomous.Mode {
		case "skip_timeout":
			timeoutMinutes = 0
		case "short_timeout":
			timeoutMinutes = config.Autonomous.ShortTimeoutMinutes
			if timeoutMinutes <= 0 {
				timeoutMinutes = defaults.Autonomous.ShortTimeoutMinutes
			}
		}
	}
	return answerSettings{timeout: time.Duration(timeoutMinutes) * time.Minute, denylist: append(defaults.AnswerDenylist, config.AnswerDenylist...)}
}

func answerSessions(sessions []domain.SessionRecord) (map[domain.SessionID]domain.SessionRecord, domain.SessionRecord) {
	workers := make(map[domain.SessionID]domain.SessionRecord, len(sessions))
	var hermes domain.SessionRecord
	for _, session := range sessions {
		if session.IsTerminated {
			continue
		}
		workers[session.ID] = session
		if session.Kind == domain.KindOrchestrator && session.Harness == domain.HarnessHermes && (hermes.ID == "" || session.UpdatedAt.After(hermes.UpdatedAt)) {
			hermes = session
		}
	}
	return workers, hermes
}

type inputQuestion struct {
	text string
	at   time.Time
}

func needsInputNotifications(notifications []domain.NotificationRecord, projectID domain.ProjectID) map[domain.SessionID][]inputQuestion {
	questions := make(map[domain.SessionID][]inputQuestion)
	for _, notification := range notifications {
		if notification.ProjectID != projectID || notification.Type != domain.NotificationNeedsInput {
			continue
		}
		questions[notification.SessionID] = append(questions[notification.SessionID], inputQuestion{text: notificationQuestion(notification), at: notification.CreatedAt})
	}
	return questions
}

func needsInput(worker domain.SessionRecord, exists bool, notifications []inputQuestion) (string, time.Time, bool) {
	if !exists || worker.IsTerminated {
		return "", time.Time{}, false
	}
	if worker.Activity.State != domain.ActivityWaitingInput {
		return "", time.Time{}, false
	}
	at := worker.Activity.LastActivityAt
	if at.IsZero() {
		at = worker.UpdatedAt
	}
	var question inputQuestion
	for _, notification := range notifications {
		// Notifications are durable history even after acknowledgement. Only
		// detail written when this waiting episode began (or later) can describe
		// the current prompt.
		if notification.at.Before(at) || notification.at.Before(question.at) {
			continue
		}
		question = notification
	}
	return question.text, at, true
}

// notificationQuestion returns only real question detail. needs_input titles
// are labels, and the current notifier body is a stock state message, neither
// of which describes an action Hermes can safely classify.
func notificationQuestion(notification domain.NotificationRecord) string {
	question := strings.TrimSpace(notification.Body)
	if normalizeIntent(question) == "the agent is waiting for your response" {
		return ""
	}
	return question
}

const (
	hermesAnswerRequestedEventKind = "hermes_answer_requested"
	hermesAnswerEventKind          = "hermes_answer"
)

type hermesAnswerPayload struct {
	AttemptID       string `json:"attemptId,omitempty"`
	WorkerSessionID string `json:"workerSessionId"`
	HermesSessionID string `json:"hermesSessionId"`
	Question        string `json:"question"`
	Prompt          string `json:"prompt"`
	WaitingAt       string `json:"waitingAt"`
	ConsumedOneShot bool   `json:"consumedOneShot,omitempty"`
}

type hermesAnswerAttempt struct {
	requested   bool
	completed   bool
	payload     hermesAnswerPayload
	requestedAt time.Time
}

func findHermesAnswerAttempt(events []domain.WorkCardEvent, workerID domain.SessionID, waitingAt time.Time) hermesAnswerAttempt {
	waitingAtText := waitingAt.UTC().Format(time.RFC3339Nano)
	completed := make(map[string]bool)
	var attempt hermesAnswerAttempt
	for _, event := range events {
		if event.Kind != hermesAnswerRequestedEventKind && event.Kind != hermesAnswerEventKind {
			continue
		}
		var payload hermesAnswerPayload
		if json.Unmarshal([]byte(event.Payload), &payload) != nil || payload.WorkerSessionID != string(workerID) || payload.WaitingAt != waitingAtText {
			continue
		}
		if event.Kind == hermesAnswerRequestedEventKind {
			if payload.AttemptID == "" || payload.AttemptID != event.ID {
				continue
			}
			attempt = hermesAnswerAttempt{requested: true, payload: payload, requestedAt: event.CreatedAt}
			continue
		}
		if payload.AttemptID != "" {
			completed[payload.AttemptID] = true
		}
	}
	attempt.completed = attempt.requested && attempt.payload.AttemptID != "" && completed[attempt.payload.AttemptID]
	return attempt
}

func hermesAnswerDelivered(messages []domain.SessionMessageRecord, attempt hermesAnswerAttempt) bool {
	if !attempt.requested {
		return false
	}
	for _, message := range messages {
		if message.TargetSessionID == domain.SessionID(attempt.payload.HermesSessionID) &&
			message.Content == attempt.payload.Prompt && !message.CreatedAt.Before(attempt.requestedAt) {
			return true
		}
	}
	return false
}

func (a *Answerer) appendHermesAnswer(ctx context.Context, card domain.WorkCard, payload hermesAnswerPayload, now time.Time) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal completed Hermes answer event for card %s: %w", card.ID, err)
	}
	if err := a.store.AppendWorkCardEvent(ctx, domain.WorkCardEvent{
		ID: a.newID(), CardID: card.ID, ProjectID: card.ProjectID, Kind: hermesAnswerEventKind, Payload: string(payloadJSON), CreatedAt: now,
	}); err != nil {
		return fmt.Errorf("append Hermes answer event for card %s: %w", card.ID, err)
	}
	return nil
}

func (a *Answerer) completeAnswer(ctx context.Context, card *domain.WorkCard, now time.Time) error {
	if card.WaitingForInput {
		card.WaitingForInput = false
		card.UpdatedAt = now
		if err := a.store.UpdateWorkCard(ctx, *card); err != nil {
			return fmt.Errorf("clear waiting state after Hermes answer for card %s: %w", card.ID, err)
		}
	}
	return nil
}

func answerDue(now, waitingAt time.Time, timeout time.Duration) bool {
	return timeout <= 0 || !now.Before(waitingAt.Add(timeout))
}

func autoAnswerAllowed(question string, denylist []string) bool {
	normalized := normalizeIntent(question)
	for _, denied := range denylist {
		if term := normalizeIntent(denied); term != "" && containsIntentPhrase(normalized, term) {
			return false
		}
	}
	for _, denied := range []string{"force push", "push force", "delete repo", "remove repo", "exfil secret", "exfiltrate secret", "upload secret", "send secret"} {
		if containsIntentPhrase(normalized, denied) {
			return false
		}
	}
	for _, word := range strings.Fields(normalized) {
		if _, allowed := allowedAnswerWords[word]; allowed {
			return true
		}
	}
	return false
}

var allowedAnswerWords = map[string]struct{}{
	"test": {}, "tests": {}, "build": {}, "compile": {}, "lint": {}, "format": {}, "read": {}, "inspect": {}, "review": {},
	"continue": {}, "retry": {}, "check": {}, "status": {}, "log": {}, "logs": {}, "explain": {}, "clarify": {},
}

func normalizeIntent(value string) string {
	value = strings.ToLower(value)
	value = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		return ' '
	}, value)
	return strings.Join(strings.Fields(value), " ")
}

func containsIntentPhrase(value, phrase string) bool {
	return strings.Contains(" "+value+" ", " "+phrase+" ")
}

const maxHermesPromptBytes = 4096

// hermesAnswerPrompt prepares a terminal-safe message without ever truncating
// the question that Hermes must authorize. Context can be shortened, but an
// oversized question is left for the user rather than changing its meaning.
func hermesAnswerPrompt(card domain.WorkCard, workerID domain.SessionID, question string) (string, bool) {
	const instruction = "A worker linked to a Workboard card needs input. Review the card and relevant workspace before deciding, then send the worker a grounded answer through the normal session send path. Do not authorize destructive, credential, or data-exfiltration actions; leave those for the user."
	question = domain.SanitizeControlChars(question)
	prefix := fmt.Sprintf("%s\n\nCard ID: %s\n", instruction, domain.SanitizeControlChars(card.ID))
	suffix := fmt.Sprintf("Worker session: %s\nQuestion: %s", domain.SanitizeControlChars(string(workerID)), question)
	base := prefix + suffix
	if len(base) > maxHermesPromptBytes {
		return "", false
	}
	context := []struct {
		label string
		value string
	}{
		{"Title", domain.SanitizeControlChars(card.Title)},
		{"Notes", domain.SanitizeControlChars(card.Notes)},
		{"Target path", domain.SanitizeControlChars(card.TargetPath)},
	}
	var details strings.Builder
	for _, field := range context {
		if field.value == "" {
			continue
		}
		details.WriteString(field.label)
		details.WriteString(": ")
		details.WriteString(field.value)
		details.WriteByte('\n')
	}
	prompt := prefix + details.String() + suffix
	if len(prompt) <= maxHermesPromptBytes {
		return prompt, true
	}

	// Preserve the complete authorization question and add only as much card
	// context as fits before it. Truncation happens at rune boundaries.
	remaining := maxHermesPromptBytes - len(base) - len("Card context: \n")
	if remaining <= 0 {
		return base, true
	}
	return prefix + "Card context: " + truncateUTF8(domain.SanitizeControlChars(details.String()), remaining) + "\n" + suffix, true
}

func truncateUTF8(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	for maxBytes > 0 && (value[maxBytes]&0xc0) == 0x80 {
		maxBytes--
	}
	return value[:maxBytes]
}
