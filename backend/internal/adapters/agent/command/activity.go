package command

import "github.com/modernagent/modern-agent/backend/internal/domain"

// DeriveActivityState maps `ao hooks command <event>` onto an AO activity state.
func DeriveActivityState(event string, _ []byte) (domain.ActivityState, bool) {
	switch event {
	case "active", "start", "session-start":
		return domain.ActivityActive, true
	case "idle", "stop":
		return domain.ActivityIdle, true
	case "waiting", "waiting-input", "permission-request":
		return domain.ActivityWaitingInput, true
	case "exited", "exit":
		return domain.ActivityExited, true
	default:
		return "", false
	}
}
