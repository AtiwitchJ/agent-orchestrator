package domain

import "time"

// SessionMessageRecord is the durable persistence shape for one agent-to-agent
// (or human-to-agent) `ao send` message. It is a plain durable fact — no
// derivation — recorded after the message is delivered to the target
// session's live runtime pane, so the send itself never blocks on storage.
// SenderSessionID is "" when a human sent the message (no senderSessionId was
// supplied on POST /send); TargetSessionID is always the recipient session.
type SessionMessageRecord struct {
	ID              string
	SenderSessionID SessionID
	TargetSessionID SessionID
	Content         string
	CreatedAt       time.Time
}
