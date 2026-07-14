package domain

import (
	"testing"
	"time"
)

var stallNow = time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

const stallTestThreshold = 4 * time.Minute

func stalledWorker() SessionRecord {
	return SessionRecord{
		Kind:     KindWorker,
		Activity: Activity{State: ActivityActive, LastActivityAt: stallNow.Add(-2 * stallTestThreshold)},
	}
}

func TestIsStalled(t *testing.T) {
	tests := []struct {
		name      string
		rec       SessionRecord
		threshold time.Duration
		want      bool
	}{
		{"stale-active-worker-is-stalled", stalledWorker(), stallTestThreshold, true},
		{
			"orchestrator-never-stalled",
			func() SessionRecord { r := stalledWorker(); r.Kind = KindOrchestrator; return r }(),
			stallTestThreshold, false,
		},
		{
			"terminated-never-stalled",
			func() SessionRecord { r := stalledWorker(); r.IsTerminated = true; return r }(),
			stallTestThreshold, false,
		},
		{
			"waiting-input-sticky-never-stalled",
			func() SessionRecord { r := stalledWorker(); r.Activity.State = ActivityWaitingInput; return r }(),
			stallTestThreshold, false,
		},
		{
			"idle-never-stalled",
			func() SessionRecord { r := stalledWorker(); r.Activity.State = ActivityIdle; return r }(),
			stallTestThreshold, false,
		},
		{
			"exited-never-stalled",
			func() SessionRecord { r := stalledWorker(); r.Activity.State = ActivityExited; return r }(),
			stallTestThreshold, false,
		},
		{
			"zero-last-activity-never-stalled",
			func() SessionRecord {
				r := stalledWorker()
				r.Activity.LastActivityAt = time.Time{}
				return r
			}(),
			stallTestThreshold, false,
		},
		{
			"within-threshold-not-stalled",
			func() SessionRecord {
				r := stalledWorker()
				r.Activity.LastActivityAt = stallNow.Add(-1 * time.Minute)
				return r
			}(),
			stallTestThreshold, false,
		},
		{
			"exactly-at-threshold-not-stalled",
			func() SessionRecord {
				r := stalledWorker()
				r.Activity.LastActivityAt = stallNow.Add(-stallTestThreshold)
				return r
			}(),
			stallTestThreshold, false,
		},
		{"zero-threshold-disables-check", stalledWorker(), 0, false},
		{"negative-threshold-disables-check", stalledWorker(), -time.Minute, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsStalled(tt.rec, stallNow, tt.threshold); got != tt.want {
				t.Fatalf("IsStalled() = %v, want %v", got, tt.want)
			}
		})
	}
}
