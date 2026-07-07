package hermes

import "github.com/modernagent/modern-agent/backend/internal/domain"

// DeriveActivityState maps a hermes hook event onto an AO activity state.
// Currently a no-op since hermes doesn't have full hooks support like Claude Code and Codex.
// The bool is false to indicate no activity signal is available.
//
// TODO(hermes): Implement activity state mapping once hermes has native hook support.
// Until then, runtime exit falls back to the reaper.
func DeriveActivityState(event string, _ []byte) (domain.ActivityState, bool) {
	// No-op for now since hermes doesn't have full hooks support
	return "", false
}
