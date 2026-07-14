package policy

// state.go holds helpers for the gate-sequence state machine. Real
// transition logic lands with the gate implementations in subsequent tasks;
// this file is intentionally thin so it can grow without churn.
//
// Why a separate file: keeping transition helpers out of engine.go lets the
// engine file stay focused on the public Engine interface and its callers,
// and lets the state-machine helpers be unit-tested in isolation without
// spinning up a Store.

import "fmt"

// NextGate returns the gate that follows current in GateOrder, or the empty
// GateID when current is the final gate (GateFinal). It is the canonical
// "advance" helper used after a pass or override outcome.
//
// Per design doc §2 decision 1, after a human "request_changes" the run
// restarts at GateReview (index 1), not GateCI; callers handle that by
// passing GateReview directly rather than calling NextGate on GateHuman.
func NextGate(current GateID) GateID {
	for i, g := range GateOrder {
		if g == current {
			if i+1 >= len(GateOrder) {
				return ""
			}
			return GateOrder[i+1]
		}
	}
	return ""
}

// GateIndex returns the 0-indexed position of g in GateOrder, or -1 if g is
// not part of the canonical sequence. Useful for "restart at gate N" logic
// and for the read model that surfaces "run is on gate 2 of 4".
func GateIndex(g GateID) int {
	for i, candidate := range GateOrder {
		if candidate == g {
			return i
		}
	}
	return -1
}

// IsTerminalOutcome reports whether the given gate outcome ends the run
// without further state transitions. OutcomeExhausted stops the run and
// escalates to the human; OutcomeOverridden is the hybrid-veto path where
// the human explicitly chose to proceed; OutcomePass and OutcomeFail are
// per-attempt and not terminal.
func IsTerminalOutcome(o GateOutcome) bool {
	switch o {
	case OutcomeExhausted, OutcomeOverridden:
		return true
	default:
		return false
	}
}

// IsRetryable reports whether the engine should automatically re-enter the
// current gate on this outcome. Only OutcomeFail is retryable; pass,
// exhausted, and overridden are all final for the gate's role in this run
// (overridden is the override-skip-and-advance case).
func IsRetryable(o GateOutcome) bool {
	return o == OutcomeFail
}

// ValidateGateID returns an error when g is not one of the four GateID
// constants. Engine implementations use this when hydrating a Run from
// persisted state to guard against schema drift.
func ValidateGateID(g GateID) error {
	switch g {
	case GateCI, GateReview, GateHuman, GateFinal:
		return nil
	case "":
		return nil // empty is the "not yet entered" sentinel
	default:
		return fmt.Errorf("policy: unknown gate id %q", string(g))
	}
}