package config

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	// PolicyReviewSameAgent reuses the primary agent for Gate 2 review.
	PolicyReviewSameAgent = "same_agent"
	// PolicyReviewCrossAgent uses ReviewAgent for Gate 2 review.
	PolicyReviewCrossAgent = "cross_agent"
	// PolicyReviewSecondOnly skips Gate 2 and reserves review for a final-pass veto.
	PolicyReviewSecondOnly = "second_only"

	// PolicyMergeSquash squash-merges an approved policy run.
	PolicyMergeSquash = "squash"
	// PolicyMergeRebase rebases and merges an approved policy run.
	PolicyMergeRebase = "rebase"
	// PolicyMergeMerge creates a merge commit for an approved policy run.
	PolicyMergeMerge = "merge"

	maxPolicyRounds = 5
)

// PolicyConfig controls the approval gates for tracker-driven work. Enabled is
// deliberately false by default; enabling an otherwise empty policy applies the
// conservative four-gate defaults returned by DefaultPolicyConfig.
type PolicyConfig struct {
	Enabled      bool   `json:"enabled"`
	TrackerLabel string `json:"tracker_label"`

	AutoFixOnCIFailure bool `json:"auto_fix_on_ci_failure"`
	MaxAutoFixRounds   int  `json:"max_auto_fix_rounds"`

	RequireAgentReview bool   `json:"require_agent_review"`
	ReviewStrategy     string `json:"review_strategy"`
	ReviewAgent        string `json:"review_agent"`
	MaxReviseRounds    int    `json:"max_revise_rounds"`

	RequireHumanApproval bool `json:"require_human_approval"`
	HumanTimeoutHours    int  `json:"human_timeout_hours"`

	AgentFinalPass  bool   `json:"agent_final_pass"`
	VetoSecondAgent string `json:"veto_second_agent"`

	MergeStrategy   string `json:"merge_strategy"`
	MinPRAgeMinutes int    `json:"min_pr_age_minutes"`
	BlockOnDraft    bool   `json:"block_on_draft"`
}

// DefaultPolicyConfig returns the conservative policy defaults. Policy remains
// opt-in, but once enabled these defaults require all four gates and block draft
// PRs before a squash merge.
func DefaultPolicyConfig() PolicyConfig {
	return PolicyConfig{
		Enabled:              false,
		TrackerLabel:         "agent-ready",
		AutoFixOnCIFailure:   true,
		MaxAutoFixRounds:     3,
		RequireAgentReview:   true,
		ReviewStrategy:       PolicyReviewSameAgent,
		MaxReviseRounds:      3,
		RequireHumanApproval: true,
		HumanTimeoutHours:    0,
		AgentFinalPass:       true,
		MergeStrategy:        PolicyMergeSquash,
		MinPRAgeMinutes:      5,
		BlockOnDraft:         true,
	}
}

// WithDefaults fills unset string and numeric fields with conservative
// defaults. Boolean defaults are applied by UnmarshalJSON, where omitted values
// can be distinguished from explicit false values.
func (c PolicyConfig) WithDefaults() PolicyConfig {
	if c == (PolicyConfig{}) {
		return DefaultPolicyConfig()
	}
	def := DefaultPolicyConfig()
	if c.TrackerLabel == "" {
		c.TrackerLabel = def.TrackerLabel
	}
	if c.MaxAutoFixRounds == 0 {
		c.MaxAutoFixRounds = def.MaxAutoFixRounds
	}
	if c.ReviewStrategy == "" {
		c.ReviewStrategy = def.ReviewStrategy
	}
	if c.MaxReviseRounds == 0 {
		c.MaxReviseRounds = def.MaxReviseRounds
	}
	if c.VetoSecondAgent == "" {
		c.VetoSecondAgent = c.ReviewAgent
	}
	if c.MergeStrategy == "" {
		c.MergeStrategy = def.MergeStrategy
	}
	if c.MinPRAgeMinutes == 0 {
		c.MinPRAgeMinutes = def.MinPRAgeMinutes
	}
	return c
}

// Validate rejects unsafe or unsupported policy settings. Disabled policies do
// not participate in execution, so their incomplete values remain inert.
func (c PolicyConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	c = c.WithDefaults()
	if strings.TrimSpace(c.TrackerLabel) == "" {
		return fmt.Errorf("policy.tracker_label: is required when enabled")
	}
	if strings.TrimSpace(c.TrackerLabel) != c.TrackerLabel {
		return fmt.Errorf("policy.tracker_label: must not have leading or trailing whitespace")
	}
	if err := validatePolicyRounds("max_auto_fix_rounds", c.MaxAutoFixRounds); err != nil {
		return err
	}
	if err := validatePolicyRounds("max_revise_rounds", c.MaxReviseRounds); err != nil {
		return err
	}
	switch c.ReviewStrategy {
	case PolicyReviewSameAgent, PolicyReviewCrossAgent, PolicyReviewSecondOnly:
	default:
		return fmt.Errorf("policy.review_strategy: must be one of: %s, %s, %s", PolicyReviewSameAgent, PolicyReviewCrossAgent, PolicyReviewSecondOnly)
	}
	if err := validatePolicyAgent("review_agent", c.ReviewAgent); err != nil {
		return err
	}
	if c.ReviewStrategy == PolicyReviewCrossAgent && c.ReviewAgent == "" {
		return fmt.Errorf("policy.review_agent: is required when review_strategy is %q", PolicyReviewCrossAgent)
	}
	if c.HumanTimeoutHours < 0 {
		return fmt.Errorf("policy.human_timeout_hours: must be >= 0")
	}
	if err := validatePolicyAgent("veto_second_agent", c.VetoSecondAgent); err != nil {
		return err
	}
	switch c.MergeStrategy {
	case PolicyMergeSquash, PolicyMergeRebase, PolicyMergeMerge:
	default:
		return fmt.Errorf("policy.merge_strategy: must be one of: %s, %s, %s", PolicyMergeSquash, PolicyMergeRebase, PolicyMergeMerge)
	}
	if c.MinPRAgeMinutes < 0 {
		return fmt.Errorf("policy.min_pr_age_minutes: must be >= 0")
	}
	return nil
}

func validatePolicyRounds(field string, rounds int) error {
	if rounds < 0 || rounds > maxPolicyRounds {
		return fmt.Errorf("policy.%s: must be between 0 and %d", field, maxPolicyRounds)
	}
	return nil
}

func validatePolicyAgent(field, agent string) error {
	if strings.TrimSpace(agent) != agent {
		return fmt.Errorf("policy.%s: must not have leading or trailing whitespace", field)
	}
	return nil
}

// MarshalJSON explicitly defines the stable snake_case project-config shape.
func (c PolicyConfig) MarshalJSON() ([]byte, error) {
	type wire PolicyConfig
	return json.Marshal(wire(c))
}

// UnmarshalJSON applies conservative defaults before overlaying fields present
// in JSON. This preserves explicit false and zero values while keeping older
// project blobs safe when newly introduced fields are absent.
func (c *PolicyConfig) UnmarshalJSON(data []byte) error {
	type wire PolicyConfig
	cfg := wire(DefaultPolicyConfig())
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}
	*c = PolicyConfig(cfg).WithDefaults()
	return nil
}
