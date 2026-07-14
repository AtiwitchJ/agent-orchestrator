package policy

import (
	"errors"
	"strings"
	"testing"
)

// TestDefaultPolicyConfig pins the conservative defaults from design doc §2
// and §4. These are the values a fresh project inherits; any change must be
// a deliberate, reviewed decision (and matched in the design doc).
func TestDefaultPolicyConfig(t *testing.T) {
	c := DefaultPolicyConfig()

	if c.Enabled {
		t.Errorf("default Enabled = true, want false (opt-in only per design doc §2 decision 5)")
	}
	if c.TrackerLabel != DefaultTrackerLabel {
		t.Errorf("default TrackerLabel = %q, want %q", c.TrackerLabel, DefaultTrackerLabel)
	}
	if !c.AutoFixOnCIFailure {
		t.Error("default AutoFixOnCIFailure = false, want true")
	}
	if c.MaxAutoFixRounds != 3 {
		t.Errorf("default MaxAutoFixRounds = %d, want 3", c.MaxAutoFixRounds)
	}
	if !c.RequireAgentReview {
		t.Error("default RequireAgentReview = false, want true")
	}
	if c.ReviewStrategy != ReviewStrategySameAgent {
		t.Errorf("default ReviewStrategy = %q, want %q", c.ReviewStrategy, ReviewStrategySameAgent)
	}
	if c.MaxReviseRounds != 3 {
		t.Errorf("default MaxReviseRounds = %d, want 3", c.MaxReviseRounds)
	}
	if !c.RequireHumanApproval {
		t.Error("default RequireHumanApproval = false, want true")
	}
	if c.HumanTimeoutHours != 0 {
		t.Errorf("default HumanTimeoutHours = %d, want 0 (no timeout)", c.HumanTimeoutHours)
	}
	if !c.AgentFinalPass {
		t.Error("default AgentFinalPass = false, want true")
	}
	if c.VetoSecondAgent != "" {
		t.Errorf("default VetoSecondAgent = %q, want empty (resolved to ReviewAgent at engine time)", c.VetoSecondAgent)
	}
	if c.MergeStrategy != MergeStrategySquash {
		t.Errorf("default MergeStrategy = %q, want %q", c.MergeStrategy, MergeStrategySquash)
	}
	if c.MinPRAgeMinutes != 5 {
		t.Errorf("default MinPRAgeMinutes = %d, want 5", c.MinPRAgeMinutes)
	}
	if !c.BlockOnDraft {
		t.Error("default BlockOnDraft = false, want true")
	}
}

// TestPolicyConfig_Validate_Defaults ensures the conservative default
// config round-trips through Validate cleanly. This guards against an
// accidental invariant being added to Validate that breaks the defaults.
func TestPolicyConfig_Validate_Defaults(t *testing.T) {
	if err := DefaultPolicyConfig().Validate(); err != nil {
		t.Fatalf("DefaultPolicyConfig().Validate() = %v, want nil", err)
	}
}

func TestPolicyConfig_Validate_Cases(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*PolicyConfig)
		wantErr bool
		wantSub string // expected substring of error message, when wantErr
	}{
		{
			name:    "ok_minimal",
			mutate:  func(c *PolicyConfig) { *c = PolicyConfig{} },
			wantErr: false,
		},
		{
			name: "ok_all_strategies",
			mutate: func(c *PolicyConfig) {
				*c = DefaultPolicyConfig()
				c.ReviewStrategy = ReviewStrategyCrossAgent
				c.ReviewAgent = "codex"
				c.MergeStrategy = MergeStrategyRebase
			},
			wantErr: false,
		},
		{
			name: "bad_review_strategy",
			mutate: func(c *PolicyConfig) {
				c.ReviewStrategy = "peer_review"
			},
			wantErr: true,
			wantSub: "review_strategy",
		},
		{
			name: "bad_merge_strategy",
			mutate: func(c *PolicyConfig) {
				c.MergeStrategy = "fast_forward"
			},
			wantErr: true,
			wantSub: "merge_strategy",
		},
		{
			name: "cross_agent_requires_review_agent",
			mutate: func(c *PolicyConfig) {
				c.ReviewStrategy = ReviewStrategyCrossAgent
				c.ReviewAgent = ""
			},
			wantErr: true,
			wantSub: "review_agent",
		},
		{
			name: "round_limit_too_high",
			mutate: func(c *PolicyConfig) {
				c.MaxAutoFixRounds = MaxRoundCeiling + 1
			},
			wantErr: true,
			wantSub: "max_auto_fix_rounds",
		},
		{
			name: "round_limit_negative",
			mutate: func(c *PolicyConfig) {
				c.MaxReviseRounds = -1
			},
			wantErr: true,
			wantSub: "max_revise_rounds",
		},
		{
			name: "negative_human_timeout",
			mutate: func(c *PolicyConfig) {
				c.HumanTimeoutHours = -2
			},
			wantErr: true,
			wantSub: "human_timeout_hours",
		},
		{
			name: "negative_pr_age",
			mutate: func(c *PolicyConfig) {
				c.MinPRAgeMinutes = -1
			},
			wantErr: true,
			wantSub: "min_pr_age_minutes",
		},
		{
			name: "bad_tracker_label_with_whitespace",
			mutate: func(c *PolicyConfig) {
				c.TrackerLabel = "agent ready"
			},
			wantErr: true,
			wantSub: "tracker_label",
		},
		{
			name: "round_limit_at_ceiling_ok",
			mutate: func(c *PolicyConfig) {
				c.MaxAutoFixRounds = MaxRoundCeiling
			},
			wantErr: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := DefaultPolicyConfig()
			tc.mutate(&c)
			err := c.Validate()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Validate() = nil, want error containing %q", tc.wantSub)
				}
				if tc.wantSub != "" && !strings.Contains(err.Error(), tc.wantSub) {
					t.Errorf("Validate() error = %q, want substring %q", err.Error(), tc.wantSub)
				}
				if !errors.Is(err, ErrInvalidConfig) {
					t.Errorf("Validate() error = %v, want errors.Is(err, ErrInvalidConfig)", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
}

// TestPolicyConfig_Validate_RoundCeiling documents the hard ceiling from
// design doc §7: "Hard ceiling at 5 rounds regardless of override; reject
// value with error if attempted". This is the contract that protects against
// runaway retry loops even when a user types a big number.
func TestPolicyConfig_Validate_RoundCeiling(t *testing.T) {
	for _, n := range []int{MaxRoundCeiling + 1, MaxRoundCeiling * 2, 1000} {
		c := DefaultPolicyConfig()
		c.MaxAutoFixRounds = n
		if err := c.Validate(); err == nil {
			t.Errorf("MaxAutoFixRounds=%d should be rejected (ceiling=%d)", n, MaxRoundCeiling)
		}
	}
}