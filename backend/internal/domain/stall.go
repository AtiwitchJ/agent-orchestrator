package domain

import "time"

// IsStalled reports whether rec is a worker session whose activity_state has
// claimed ActivityActive for longer than threshold without a fresh signal —
// the harness's own liveness signal has gone stale while claiming to be busy.
//
// This function is the single safety gate shared by two very different
// callers: deriveStatus (to surface the read-only "stalled" badge) and
// stallmon (to decide what may be auto-terminated). Because a bug here can
// kill real in-progress work, every clause is a structural, self-contained
// check — it never trusts a caller to have pre-filtered orchestrators,
// terminated sessions, or the sticky waiting-input state:
//
//   - rec.Kind must be KindWorker. An orchestrator session can never be
//     "stalled" through this function, regardless of activity_state or how
//     long it has been silent.
//   - rec.IsTerminated must be false. A session that is already terminated
//     has nothing left to kill and is never stalled.
//   - rec.Activity.State must be exactly ActivityActive. ActivityWaitingInput
//     is sticky (see ActivityState.IsSticky) — a session legitimately paused
//     for a human is not stuck, and must never be treated as stalled.
//     ActivityIdle/ActivityExited are not "claiming to work", so they are not
//     stalled either.
//   - rec.Activity.LastActivityAt must be non-zero. A zero timestamp means no
//     activity signal has ever been recorded, so there is no reliable silence
//     duration to measure against threshold.
//   - threshold must be > 0. A non-positive threshold disables the check
//     entirely (never stalled) rather than treating every active session as
//     instantly stalled.
//
// Only once every one of those holds does IsStalled compare now minus
// LastActivityAt against threshold.
func IsStalled(rec SessionRecord, now time.Time, threshold time.Duration) bool {
	if rec.Kind != KindWorker {
		return false
	}
	if rec.IsTerminated {
		return false
	}
	if rec.Activity.State != ActivityActive {
		return false
	}
	if rec.Activity.LastActivityAt.IsZero() {
		return false
	}
	if threshold <= 0 {
		return false
	}
	return now.Sub(rec.Activity.LastActivityAt) > threshold
}
