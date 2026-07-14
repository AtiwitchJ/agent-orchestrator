package policy

import (
	"errors"
	"fmt"
)

// Config is the per-project policy engine configuration. It is the typed
// accessor over the JSON blob already accepted by PUT /projects/{id}/config;
// the service layer hydrates a domain.ProjectConfig.Policy field from this
// struct and validates it on read.
//
// Defaults are conservative: a project with Enabled=false is unaffected by the
// engine. A project with Enabled=true and no other overrides gets the full
// four-gate sequence with same_agent review and squash merge.
//
// Reference: docs/superpowers/specs/2026-07-14-hybrid-approval-gates-design.md §4
type Config struct {
	// Master switches — opt-in.
	Enabled      bool   `json:"enabled"`
	TrackerLabel string `json:"tracker_label"` // default "agent-ready"

	// Gate 1: CI.
	AutoFixOnCIFailure bool `json:"auto_fix_on_ci_failure"` // default true
	MaxAutoFixRounds   int  `json:"max_auto_fix_rounds"`    // default 3

	// Gate 2: Agent self-review.
	RequireAgentReview bool   `json:"require_agent_review"` // default true
	ReviewStrategy     string `json:"review_strategy"`      // default "same_agent"
	// "same_agent" | "cross_agent" | "second_only".
	ReviewAgent     string `json:"review_agent"`      // required when cross_agent
	MaxReviseRounds int    `json:"max_revise_rounds"` // default 3

	// Gate 3: Human approve.
	RequireHumanApproval bool `json:"require_human_approval"` // default true
	HumanTimeoutHours    int  `json:"human_timeout_hours"`    // 0 = no timeout

	// Gate 4: Agent final-pass.
	AgentFinalPass  bool   `json:"agent_final_pass"`  // default true
	VetoSecondAgent string `json:"veto_second_agent"` // default = ReviewAgent

	// Merge policy.
	MergeStrategy string `json:"merge_strategy"` // default "squash"
	// "squash" | "rebase" | "merge".
	MinPRAgeMinutes int  `json:"min_pr_age_minutes"` // default 5
	BlockOnDraft    bool `json:"block_on_draft"`     // default true
}

// Review strategy constants (Gate 2).
const (
	ReviewStrategySameAgent  = "same_agent"
	ReviewStrategyCrossAgent = "cross_agent"
	ReviewStrategySecondOnly = "second_only"
)

// Merge strategy constants.
const (
	MergeStrategySquash = "squash"
	MergeStrategyRebase = "rebase"
	MergeStrategyMerge  = "merge"
)

// DefaultTrackerLabel is applied when Config.TrackerLabel is empty.
// Matches the design doc §2 "Opt-in per project" default.
const DefaultTrackerLabel = "agent-ready"

// MaxRoundCeiling is the round-limit hard ceiling (design doc §7 "Round
// limits too high → waste"). Any config value above this is rejected by
// Validate regardless of override.
const MaxRoundCeiling = 5

// DefaultPolicyConfig returns the conservative defaults documented in §2 and
// §4 of the design doc. Callers should apply these to a fresh project before
// persisting, then re-validate after any user override.
func DefaultPolicyConfig() Config {
	return Config{
		Enabled:              false, // opt-in
		TrackerLabel:         DefaultTrackerLabel,
		AutoFixOnCIFailure:   true,
		MaxAutoFixRounds:     3,
		RequireAgentReview:   true,
		ReviewStrategy:       ReviewStrategySameAgent,
		ReviewAgent:          "",
		MaxReviseRounds:      3,
		RequireHumanApproval: true,
		HumanTimeoutHours:    0, // no timeout by default
		AgentFinalPass:       true,
		VetoSecondAgent:      "", // resolved at engine time to ReviewAgent
		MergeStrategy:        MergeStrategySquash,
		MinPRAgeMinutes:      5,
		BlockOnDraft:         true,
	}
}

// Validate enforces the documented invariants on Config. It returns
// nil when the config is ready to persist, otherwise an error suitable for
// surfacing through the existing usageError / 422 path.
//
// The validation is intentionally lightweight — it checks string enums and
// numeric ceilings, not cross-field semantics that require a project record.
// Callers needing project context (e.g. resolving VetoSecondAgent default)
// should layer that on top.
func (c Config) Validate() error {
	if c.TrackerLabel != "" && !isValidLabel(c.TrackerLabel) {
		return fmt.Errorf("%w: tracker_label %q is not a valid label", ErrInvalidConfig, c.TrackerLabel)
	}
	if err := validateReviewStrategy(c.ReviewStrategy); err != nil {
		return err
	}
	if err := validateMergeStrategy(c.MergeStrategy); err != nil {
		return err
	}
	if c.RequireAgentReview && c.ReviewStrategy == ReviewStrategyCrossAgent && c.ReviewAgent == "" {
		return fmt.Errorf("%w: review_agent is required when review_strategy is %q", ErrInvalidConfig, ReviewStrategyCrossAgent)
	}
	if err := validateRoundLimit("max_auto_fix_rounds", c.MaxAutoFixRounds); err != nil {
		return err
	}
	if err := validateRoundLimit("max_revise_rounds", c.MaxReviseRounds); err != nil {
		return err
	}
	if c.HumanTimeoutHours < 0 {
		return fmt.Errorf("%w: human_timeout_hours must be >= 0", ErrInvalidConfig)
	}
	if c.MinPRAgeMinutes < 0 {
		return fmt.Errorf("%w: min_pr_age_minutes must be >= 0", ErrInvalidConfig)
	}
	return nil
}

// ErrInvalidConfig is returned by Validate when the config cannot be persisted.
// Transport layers map it to 422 (HTTP) or usageError (CLI) just like other
// domain validation failures.
var ErrInvalidConfig = errors.New("policy: invalid config")

func validateReviewStrategy(s string) error {
	if s == "" {
		return nil // will be defaulted by callers
	}
	switch s {
	case ReviewStrategySameAgent, ReviewStrategyCrossAgent, ReviewStrategySecondOnly:
		return nil
	default:
		return fmt.Errorf("%w: review_strategy %q is not one of same_agent|cross_agent|second_only", ErrInvalidConfig, s)
	}
}

func validateMergeStrategy(s string) error {
	if s == "" {
		return nil
	}
	switch s {
	case MergeStrategySquash, MergeStrategyRebase, MergeStrategyMerge:
		return nil
	default:
		return fmt.Errorf("%w: merge_strategy %q is not one of squash|rebase|merge", ErrInvalidConfig, s)
	}
}

func validateRoundLimit(name string, n int) error {
	if n < 0 {
		return fmt.Errorf("%w: %s must be >= 0", ErrInvalidConfig, name)
	}
	if n > MaxRoundCeiling {
		return fmt.Errorf("%w: %s=%d exceeds hard ceiling %d", ErrInvalidConfig, name, n, MaxRoundCeiling)
	}
	return nil
}

// isValidLabel keeps the tracker label to a safe subset. The SCM observer
// passes this value to the tracker adapter for filtering; we want to reject
// whitespace, empty strings, and absurd lengths early.
func isValidLabel(label string) bool {
	if label == "" || len(label) > 100 {
		return false
	}
	for _, r := range label {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return false
		}
	}
	return true
}
