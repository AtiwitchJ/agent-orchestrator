package terminal

import (
	"sync"
	"time"
)

// OutputPulse is a conservative secondary liveness signal for the stall
// monitor (observe/stallmon): it tracks the last time real PTY output was
// observed for a given terminal/runtime-handle id, independent of the
// agent's own activity_state hook signal.
//
// It exists because a session's activity_state hook pipeline can go quiet
// (a broken/blocked hook) while the agent's pane is still visibly producing
// output — that session is not actually stalled. stallmon consumes this
// registry through LastOutputAt STRICTLY as a conservative override: fresh
// data can only PREVENT a kill decision it would otherwise make, never
// trigger one, and the absence of any data for an id (no terminal ever
// attached) must fall back to activity-state-only logic rather than failing
// closed into "must be stalled".
type OutputPulse struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

// NewOutputPulse constructs an empty registry.
func NewOutputPulse() *OutputPulse {
	return &OutputPulse{seen: map[string]time.Time{}}
}

// Touch records that PTY output was observed for id at t. Called from
// attachment.copyOut on every non-empty read from the pane.
func (p *OutputPulse) Touch(id string, t time.Time) {
	if p == nil || id == "" {
		return
	}
	p.mu.Lock()
	p.seen[id] = t
	p.mu.Unlock()
}

// LastOutputAt returns the last time output was observed for id and whether
// any observation exists at all. false means no attachment has ever produced
// output for id (including "no attachment was ever opened") — callers must
// treat that as "no secondary signal available", not as evidence of
// silence.
func (p *OutputPulse) LastOutputAt(id string) (time.Time, bool) {
	if p == nil || id == "" {
		return time.Time{}, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	t, ok := p.seen[id]
	return t, ok
}
