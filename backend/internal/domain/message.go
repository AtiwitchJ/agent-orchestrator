package domain

import "time"

// SessionMessageRecord is the durable persistence shape for one agent-to-agent
// (or human-to-agent) `ao send` message. It is a plain durable fact — no
// derivation — recorded after the message is delivered to the target
// session's live runtime pane, so the send itself never blocks on storage.
// SenderSessionID is "" when a human sent the message (no senderSessionId was
// supplied on POST /send), or when a non-empty senderSessionId was supplied
// but named no known session; TargetSessionID is always the recipient
// session.
//
// Trust boundary: SenderSessionID is caller-supplied and only existence-
// checked by Service.Send, never ownership-verified against the HTTP caller
// — this daemon has no session-level auth, so any process that can reach the
// API can claim to be any session. Treat SenderSessionID as a hint for
// debugging/UX, not a cryptographic attribution of who actually sent it.
type SessionMessageRecord struct {
	ID              string
	SenderSessionID SessionID
	TargetSessionID SessionID
	Content         string
	CreatedAt       time.Time
}
