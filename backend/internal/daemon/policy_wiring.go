package daemon

import (
	"context"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/notify"
	"github.com/modernagent/modern-agent/backend/internal/policy"
	"github.com/modernagent/modern-agent/backend/internal/policy/gates"
	"github.com/modernagent/modern-agent/backend/internal/ports"
	"github.com/modernagent/modern-agent/backend/internal/storage/sqlite"
)

// newPolicyEngine wires the production policy.Engine: the SQLite store
// (already implementing policy.Store — see
// internal/storage/sqlite/store/policy_store.go) driving CIGate (graduated
// to real PR-facts data via store.ListPRFactsForSession) and HumanGate
// (graduated to real notifications via notifier.Notify). ReviewGate and
// FinalGate remain Phase 1 stubs; see docs/superpowers/plans/
// 2026-07-14-hybrid-approval-gates.md's gate graduation order.
func newPolicyEngine(store *sqlite.Store, notifier *notify.Manager) policy.Engine {
	return policy.NewEngine(store, []policy.Gate{
		gates.NewCIGateWithChecker(policyPRChecker(store)),
		gates.NewReviewGate(""),
		gates.NewHumanGate(policyNotifyFunc(notifier)),
		gates.NewFinalGate(),
	})
}

// policyPRChecker adapts store.ListPRFactsForSession to gates.PRChecker,
// matching prID against domain.PRFacts.URL (a run's PRID is the PR's URL,
// same convention as the rest of the PR read path).
func policyPRChecker(store *sqlite.Store) gates.PRChecker {
	return func(ctx context.Context, sessionID, prID string) (gates.PRCIState, error) {
		facts, err := store.ListPRFactsForSession(ctx, domain.SessionID(sessionID))
		if err != nil {
			return "", err
		}
		for _, f := range facts {
			if f.URL == prID {
				return policyCIStateFromDomain(f.CI), nil
			}
		}
		return gates.PRCIUnknown, nil
	}
}

func policyCIStateFromDomain(s domain.CIState) gates.PRCIState {
	switch s {
	case domain.CIPassing:
		return gates.PRCIPassing
	case domain.CIFailing:
		return gates.PRCIFailing
	case domain.CIPending:
		return gates.PRCIPending
	default:
		return gates.PRCIUnknown
	}
}

// policyNotifyFunc adapts notifier.Notify to gates.NotifyFunc. HumanGate only
// ever emits the needs_input kind (see gates/human.go), so the mapping to
// domain.NotificationNeedsInput is direct.
func policyNotifyFunc(notifier *notify.Manager) gates.NotifyFunc {
	return func(ctx context.Context, intent gates.NotifyIntent) error {
		return notifier.Notify(ctx, ports.NotificationIntent{
			Type:      domain.NotificationNeedsInput,
			SessionID: domain.SessionID(intent.SessionID),
			ProjectID: domain.ProjectID(intent.ProjectID),
			PRURL:     intent.PRID,
			CreatedAt: time.Now().UTC(),
		})
	}
}
