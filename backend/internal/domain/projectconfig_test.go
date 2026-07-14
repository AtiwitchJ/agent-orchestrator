package domain

import (
	"encoding/json"
	"strings"
	"testing"

	internalconfig "github.com/modernagent/modern-agent/backend/internal/config"
)

func TestProjectConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     ProjectConfig
		wantErr bool
	}{
		{"empty ok", ProjectConfig{}, false},
		{"good agent config", ProjectConfig{AgentConfig: AgentConfig{Model: "m", Permissions: PermissionModeAuto}}, false},
		{"bad permission", ProjectConfig{AgentConfig: AgentConfig{Permissions: "yolo"}}, true},
		{"good session prefix", ProjectConfig{SessionPrefix: "ao"}, false},
		{"session prefix with slash", ProjectConfig{SessionPrefix: "ao/project"}, true},
		{"session prefix with backslash", ProjectConfig{SessionPrefix: `ao\project`}, true},
		{"session prefix traversal component", ProjectConfig{SessionPrefix: ".."}, true},
		{"good role override", ProjectConfig{Worker: RoleOverride{Harness: HarnessCodex}}, false},
		{"unknown role harness", ProjectConfig{Orchestrator: RoleOverride{Harness: "nope"}}, true},
		{"bad role agent config", ProjectConfig{Worker: RoleOverride{AgentConfig: AgentConfig{Permissions: "nope"}}}, true},
		{"good symlinks", ProjectConfig{Symlinks: []string{".env", "configs/dev.toml"}}, false},
		{"symlink absolute path", ProjectConfig{Symlinks: []string{"/etc/passwd"}}, true},
		{"symlink parent escape", ProjectConfig{Symlinks: []string{"../escape"}}, true},
		{"symlink embedded parent", ProjectConfig{Symlinks: []string{"a/../../b"}}, true},
		{"symlink bare ..", ProjectConfig{Symlinks: []string{".."}}, true},
		{"good reviewers", ProjectConfig{Reviewers: []ReviewerConfig{{Harness: ReviewerClaudeCode}}}, false},
		{"good codex reviewer", ProjectConfig{Reviewers: []ReviewerConfig{{Harness: ReviewerCodex}}}, false},
		{"good opencode reviewer", ProjectConfig{Reviewers: []ReviewerConfig{{Harness: ReviewerOpenCode}}}, false},
		{"unknown reviewer harness", ProjectConfig{Reviewers: []ReviewerConfig{{Harness: "nope"}}}, true},
		{"worker-only harness is not auto a reviewer", ProjectConfig{Reviewers: []ReviewerConfig{{Harness: ReviewerHarness(HarnessAider)}}}, true},
		{"empty reviewer harness", ProjectConfig{Reviewers: []ReviewerConfig{{Harness: ""}}}, true},
		{"tracker intake assignee rule", ProjectConfig{TrackerIntake: TrackerIntakeConfig{Enabled: true, Assignee: "alice"}}, false},
		{"tracker intake explicit github", ProjectConfig{TrackerIntake: TrackerIntakeConfig{Enabled: true, Provider: TrackerProviderGitHub, Assignee: "alice"}}, false},
		{"tracker intake no rule", ProjectConfig{TrackerIntake: TrackerIntakeConfig{Enabled: true}}, true},
		{"tracker intake unknown provider", ProjectConfig{TrackerIntake: TrackerIntakeConfig{Enabled: true, Provider: "linear", Assignee: "alice"}}, true},
		{"tracker intake repo with whitespace", ProjectConfig{TrackerIntake: TrackerIntakeConfig{Enabled: true, Repo: " acme/demo", Assignee: "alice"}}, true},
		{"tracker intake assignee with whitespace", ProjectConfig{TrackerIntake: TrackerIntakeConfig{Enabled: true, Assignee: " alice"}}, true},
		{"policy disabled ignores invalid values", ProjectConfig{Policy: internalconfig.PolicyConfig{MaxAutoFixRounds: -1}}, false},
		{"policy enabled valid", ProjectConfig{Policy: enabledPolicyConfig()}, false},
		{"policy enabled invalid", ProjectConfig{Policy: internalconfig.PolicyConfig{Enabled: true, ReviewStrategy: "unknown"}}, true},
		{"heartbeat disabled ok", ProjectConfig{Heartbeat: HeartbeatConfig{}}, false},
		{"heartbeat enabled default interval", ProjectConfig{Heartbeat: HeartbeatConfig{Enabled: true}}, false},
		{"heartbeat enabled explicit interval", ProjectConfig{Heartbeat: HeartbeatConfig{Enabled: true, Interval: "15m"}}, false},
		{"heartbeat interval not a duration", ProjectConfig{Heartbeat: HeartbeatConfig{Enabled: true, Interval: "soon"}}, true},
		{"heartbeat interval below minimum", ProjectConfig{Heartbeat: HeartbeatConfig{Enabled: true, Interval: "30s"}}, true},
		{"heartbeat interval at minimum", ProjectConfig{Heartbeat: HeartbeatConfig{Enabled: true, Interval: "1m"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.cfg.Validate(); (err != nil) != tt.wantErr {
				t.Fatalf("Validate() err = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestDefaultProjectConfig(t *testing.T) {
	def := DefaultProjectConfig()

	// The one documented non-empty default.
	if def.DefaultBranch != "main" {
		t.Fatalf("default DefaultBranch = %q, want main", def.DefaultBranch)
	}
	if def.Policy != internalconfig.DefaultPolicyConfig() {
		t.Fatalf("default Policy = %#v, want %#v", def.Policy, internalconfig.DefaultPolicyConfig())
	}

	// Every other field defaults to its zero value: clearing the documented
	// defaults must leave the config completely empty.
	def.DefaultBranch = ""
	def.Policy = internalconfig.PolicyConfig{}
	if !def.IsZero() {
		t.Fatalf("default config has unexpected non-zero fields: %#v", def)
	}
}

func TestProjectConfigWithDefaults(t *testing.T) {
	// An unset config gets the documented defaults.
	got := (ProjectConfig{}).WithDefaults()
	if got.DefaultBranch != DefaultBranchName {
		t.Fatalf("WithDefaults = %#v, want branch=main", got)
	}
	if got.Policy != internalconfig.DefaultPolicyConfig() {
		t.Fatalf("WithDefaults Policy = %#v, want %#v", got.Policy, internalconfig.DefaultPolicyConfig())
	}

	// Set fields are preserved, not overwritten.
	got = (ProjectConfig{
		DefaultBranch: "develop",
		AgentConfig:   AgentConfig{Model: "m"},
	}).WithDefaults()
	if got.DefaultBranch != "develop" {
		t.Fatalf("WithDefaults overwrote set fields: %#v", got)
	}
	if got.AgentConfig.Model != "m" {
		t.Fatalf("WithDefaults dropped a set field: %#v", got.AgentConfig)
	}

	got = (ProjectConfig{TrackerIntake: TrackerIntakeConfig{Enabled: true, Assignee: "alice"}}).WithDefaults()
	if got.TrackerIntake.Provider != TrackerProviderGitHub {
		t.Fatalf("TrackerIntake.Provider = %q, want %q", got.TrackerIntake.Provider, TrackerProviderGitHub)
	}

	got = (ProjectConfig{}).WithDefaults()
	if got.TrackerIntake.Provider != "" {
		t.Fatalf("disabled TrackerIntake.Provider = %q, want empty", got.TrackerIntake.Provider)
	}
}

func TestProjectConfigPolicyJSONRoundTrip(t *testing.T) {
	withoutPolicy, err := json.Marshal(ProjectConfig{DefaultBranch: "develop"})
	if err != nil {
		t.Fatalf("Marshal without policy: %v", err)
	}
	if strings.Contains(string(withoutPolicy), `"policy"`) {
		t.Fatalf("zero policy should be omitted, got %s", withoutPolicy)
	}

	input := []byte(`{"defaultBranch":"develop","policy":{"enabled":true,"require_human_approval":false}}`)
	var got ProjectConfig
	if err := json.Unmarshal(input, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.DefaultBranch != "develop" || !got.Policy.Enabled || got.Policy.RequireHumanApproval {
		t.Fatalf("Unmarshal = %#v", got)
	}
	if got.Policy.MaxAutoFixRounds != 3 || got.Policy.ReviewStrategy != internalconfig.PolicyReviewSameAgent {
		t.Fatalf("Unmarshal did not apply policy defaults: %#v", got.Policy)
	}

	data, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var roundTrip ProjectConfig
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("round-trip Unmarshal: %v", err)
	}
	if roundTrip.DefaultBranch != got.DefaultBranch || roundTrip.Policy != got.Policy {
		t.Fatalf("round trip = %#v, want %#v", roundTrip, got)
	}
}

func enabledPolicyConfig() internalconfig.PolicyConfig {
	cfg := internalconfig.DefaultPolicyConfig()
	cfg.Enabled = true
	return cfg
}

func TestResolveReviewerHarness(t *testing.T) {
	// A configured reviewer always wins, regardless of the worker harness.
	cfg := ProjectConfig{Reviewers: []ReviewerConfig{{Harness: ReviewerClaudeCode}}}
	if got := cfg.ResolveReviewerHarness(HarnessAider); got != ReviewerClaudeCode {
		t.Fatalf("configured reviewer = %q, want claude-code", got)
	}

	// No reviewer configured: always use claude-code, regardless of the worker
	// harness (see #2241).
	if got := (ProjectConfig{}).ResolveReviewerHarness(HarnessClaudeCode); got != ReviewerClaudeCode {
		t.Fatalf("default = %q, want reviewer claude-code", got)
	}
	if got := (ProjectConfig{}).ResolveReviewerHarness(HarnessCodex); got != ReviewerClaudeCode {
		t.Fatalf("default = %q, want reviewer claude-code", got)
	}
	if got := (ProjectConfig{}).ResolveReviewerHarness(HarnessOpenCode); got != ReviewerClaudeCode {
		t.Fatalf("default = %q, want reviewer claude-code", got)
	}

	// A worker harness that is not claude-code also falls back to claude-code.
	if got := (ProjectConfig{}).ResolveReviewerHarness(HarnessAider); got != FallbackReviewerHarness {
		t.Fatalf("fallback = %q, want %q", got, FallbackReviewerHarness)
	}
}

func TestProjectConfigIsZero(t *testing.T) {
	if !(ProjectConfig{}).IsZero() {
		t.Fatal("empty config should be zero")
	}
	if (ProjectConfig{DefaultBranch: "main"}).IsZero() {
		t.Fatal("populated config should not be zero")
	}
	if (ProjectConfig{Env: map[string]string{"A": "b"}}).IsZero() {
		t.Fatal("config with env should not be zero")
	}
}
