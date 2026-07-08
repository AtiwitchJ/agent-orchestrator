package domain

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

// ProjectConfig is the typed per-project configuration — the SQLite twin of the
// legacy modern-agent.yaml `projects.<id>` block. It is persisted as one
// JSON blob per project and resolved at spawn. Each field is typed and
// validated; there is no free-form map.
//
// Only fields with a live consumer are modeled: DefaultBranch, Env, Symlinks,
// PostCreate, AgentConfig, and the role overrides are consumed at spawn;
// SessionPrefix feeds the display prefix. TrackerIntake feeds the background
// issue-intake loop. Deliverable configures the artifact a docs-repo session
// produces, consumed by the deliverable watcher.
type ProjectConfig struct {
	// DefaultBranch is the base branch new session worktrees are created from.
	DefaultBranch string `json:"defaultBranch,omitempty"`
	// SessionPrefix overrides the displayed session-id prefix.
	SessionPrefix string `json:"sessionPrefix,omitempty"`

	// Env are extra environment variables forwarded into worker session
	// runtimes. AO-internal vars (AO_SESSION, AO_PROJECT_ID, …) always win.
	Env map[string]string `json:"env,omitempty"`
	// Symlinks are repo-relative paths symlinked into each session workspace.
	Symlinks []string `json:"symlinks,omitempty"`
	// PostCreate are shell commands run in the workspace after it is created.
	PostCreate []string `json:"postCreate,omitempty"`

	// AgentConfig is the default agent config for the project.
	AgentConfig AgentConfig `json:"agentConfig,omitempty"`
	// Worker and Orchestrator are role-specific harness/agent-config overrides.
	Worker       RoleOverride `json:"worker,omitempty"`
	Orchestrator RoleOverride `json:"orchestrator,omitempty"`

	// Reviewers names the agent(s) that review a worker's PR when a review is
	// triggered. It is configured independently of the Worker override; an empty
	// list falls back to claude-code (see ResolveReviewerHarness).
	Reviewers []ReviewerConfig `json:"reviewers,omitempty"`

	// TrackerIntake controls issue-driven worker spawning. It is opt-in and
	// read-only toward the tracker in v1: matching issues spawn sessions, but the
	// tracker is not commented on or transitioned.
	TrackerIntake TrackerIntakeConfig `json:"trackerIntake,omitempty"`

	// Deliverable describes the artifact a docs-repo session produces. It is
	// consumed by the deliverable watcher: the session is marked StatusReportReady
	// when the agent exits AND the watcher confirms the artifact exists.
	Deliverable *DeliverableConfig `json:"deliverable,omitempty"`

	// Heartbeat controls the periodic wake-up nudge sent to this project's
	// orchestrator when the project is an HQ (see HQRole). It is a no-op on
	// ordinary projects.
	Heartbeat HeartbeatConfig `json:"heartbeat,omitempty"`
}

// DefaultHeartbeatInterval is the wake-up cadence an HQ project gets when it
// enables the heartbeat without naming an interval.
const DefaultHeartbeatInterval = "30m"

// MinHeartbeatInterval is the shortest interval a heartbeat can be configured
// with, to keep the wake-up loop from spamming an HQ orchestrator.
const MinHeartbeatInterval = "1m"

// HeartbeatConfig controls the periodic wake-up nudge for an HQ project's
// PM/CEO orchestrator.
type HeartbeatConfig struct {
	Enabled bool `json:"enabled,omitempty"`
	// Interval is a Go duration string (e.g. "30m"). Defaults to
	// DefaultHeartbeatInterval when Enabled and left unset.
	Interval string `json:"interval,omitempty"`
}

// WithDefaults fills Interval only when the heartbeat is enabled. Disabled
// heartbeats leave the zero value untouched so empty project configs still
// store as NULL.
func (c HeartbeatConfig) WithDefaults() HeartbeatConfig {
	if c.Enabled && c.Interval == "" {
		c.Interval = DefaultHeartbeatInterval
	}
	return c
}

// Validate rejects an interval that does not parse as a Go duration or is
// shorter than MinHeartbeatInterval.
func (c HeartbeatConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	c = c.WithDefaults()
	d, err := time.ParseDuration(c.Interval)
	if err != nil {
		return fmt.Errorf("heartbeat.interval: %w", err)
	}
	minInterval, _ := time.ParseDuration(MinHeartbeatInterval)
	if d < minInterval {
		return fmt.Errorf("heartbeat.interval: must be at least %s", MinHeartbeatInterval)
	}
	return nil
}

// DeliverableType discriminates the deliverable watcher implementation.
type DeliverableType string

const (
	// DeliverableTypeFilesystem watches for a file matching a glob pattern.
	DeliverableTypeFilesystem DeliverableType = "filesystem"
	// DeliverableTypeDatabase polls a database query for a completion condition.
	DeliverableTypeDatabase DeliverableType = "database"
	// DeliverableTypeWebhook polls an HTTP endpoint for a success signal.
	DeliverableTypeWebhook DeliverableType = "webhook"
)

// DeliverableConfig describes the artifact a docs-repo session produces and how
// the watcher confirms it exists. Exactly one Spec field is non-nil based on Type.
type DeliverableConfig struct {
	Type        DeliverableType  `json:"type"`
	Filesystem  *FilesystemSpec  `json:"filesystem,omitempty"`
	Database    *DatabaseSpec    `json:"database,omitempty"`
	Webhook     *WebhookSpec    `json:"webhook,omitempty"`
}

// FilesystemSpec describes a file-system watcher for a docs-repo deliverable.
type FilesystemSpec struct {
	// Glob is a glob pattern relative to the worktree root, e.g. "**/*.md"
	// or "reports/*.pdf". The watcher uses fsnotify to detect creation.
	Glob string `json:"glob"`
}

// DatabaseSpec describes a database-query watcher for a docs-repo deliverable.
type DatabaseSpec struct {
	// Query is a SELECT statement that returns a single row. The watcher runs
	// it on PollInterval and checks Condition against the result.
	Query string `json:"query"`
	// Condition specifies what constitutes "found":
	//   - "exists" (row exists)
	//   - "count_gt_0" (row count > 0)
	//   - "value_equals" (first column == Expected)
	//   - "value_neq" (first column != Expected)
	//   - "value_gt" (first column > Expected)
	//   - "value_gte" (first column >= Expected)
	//   - "value_lt" (first column < Expected)
	//   - "value_lte" (first column <= Expected)
	//   - "row_count_eq" (total rows == Expected)
	//   - "row_count_neq" (total rows != Expected)
	//   - "row_count_gt" (total rows > Expected)
	//   - "row_count_gte" (total rows >= Expected)
	//   - "row_count_lt" (total rows < Expected)
	//   - "row_count_lte" (total rows <= Expected)
	Condition string `json:"condition"`
	// Expected is the value to compare against.
	Expected any `json:"expected,omitempty"`
}

// WebhookSpec describes an HTTP endpoint watcher for a docs-repo deliverable.
type WebhookSpec struct {
	// URL is the endpoint to poll. The watcher uses GET requests.
	URL string `json:"url"`
	// Condition specifies what constitutes "found": "received" (any 2xx response),
	// "status_2xx" (2xx status code).
	Condition string `json:"condition"`
	// PollInterval is the polling cadence, e.g. "30s". Defaults to "30s".
	PollInterval string `json:"pollInterval,omitempty"`
	// AuthHeader is the name of an HTTP header for authentication, e.g. "Authorization".
	AuthHeader string `json:"authHeader,omitempty"`
	// AuthHeaderValue is the full value for the authentication header.
	// For Bearer tokens, use "Bearer <token>" format directly.
	AuthHeaderValue string `json:"authHeaderValue,omitempty"`
	// BearerToken is a shorthand for setting Authorization: Bearer <token>.
	BearerToken string `json:"bearerToken,omitempty"`
}

// ReviewerConfig names one reviewer agent by harness. The harness is drawn from
// the reviewer vocabulary (ReviewerHarness), which is distinct from the worker
// AgentHarness set.
type ReviewerConfig struct {
	Harness ReviewerHarness `json:"harness"`
}

// FallbackReviewerHarness is the reviewer used when a project configures none
// and the worker's harness is not itself a supported reviewer.
const FallbackReviewerHarness = ReviewerClaudeCode

// ResolveReviewerHarness picks the reviewer harness for a worker. A configured
// reviewer wins; otherwise claude-code is used.
func (c ProjectConfig) ResolveReviewerHarness(_ AgentHarness) ReviewerHarness {
	if len(c.Reviewers) > 0 {
		return c.Reviewers[0].Harness
	}
	return FallbackReviewerHarness
}

// RoleOverride overrides the harness and/or agent config for a session role.
type RoleOverride struct {
	Harness     AgentHarness `json:"agent,omitempty"`
	AgentConfig AgentConfig  `json:"agentConfig,omitempty"`
}

// DefaultBranchName is the base branch used when a project configures none.
const DefaultBranchName = "main"

// DefaultProjectConfig returns the config a project has when it sets nothing:
// branch "main". Every other field defaults to its zero value (no
// env/symlinks/post-create, agent + role defaults).
func DefaultProjectConfig() ProjectConfig {
	return ProjectConfig{
		DefaultBranch: DefaultBranchName,
	}
}

// WithDefaults overlays DefaultProjectConfig onto c, filling only fields the
// project left unset. A set field is always preserved.
func (c ProjectConfig) WithDefaults() ProjectConfig {
	def := DefaultProjectConfig()
	if c.DefaultBranch == "" {
		c.DefaultBranch = def.DefaultBranch
	}
	c.TrackerIntake = c.TrackerIntake.WithDefaults()
	c.Heartbeat = c.Heartbeat.WithDefaults()
	return c
}

// IsZero reports whether the config carries no settings, so storage can persist
// SQL NULL and resolution can skip an empty config.
func (c ProjectConfig) IsZero() bool {
	return reflect.DeepEqual(c, ProjectConfig{})
}

// Validate rejects values outside the typed vocabulary so a bad config is
// refused when it is set (CLI/API) rather than surfacing at spawn.
func (c ProjectConfig) Validate() error {
	if err := c.AgentConfig.Validate(); err != nil {
		return err
	}
	if err := validateNameComponent("sessionPrefix", c.SessionPrefix); err != nil {
		return err
	}
	for role, ro := range map[string]RoleOverride{"worker": c.Worker, "orchestrator": c.Orchestrator} {
		if ro.Harness != "" && !ro.Harness.IsKnown() {
			return fmt.Errorf("%s.agent: unknown harness %q", role, ro.Harness)
		}
		if err := ro.AgentConfig.Validate(); err != nil {
			return fmt.Errorf("%s.%w", role, err)
		}
	}
	for _, s := range c.Symlinks {
		if err := validateRepoRelative(s); err != nil {
			return fmt.Errorf("symlink %q: %w", s, err)
		}
	}
	for i, rv := range c.Reviewers {
		if !rv.Harness.IsKnown() {
			return fmt.Errorf("reviewers[%d].harness: unknown harness %q", i, rv.Harness)
		}
	}
	if err := c.TrackerIntake.Validate(); err != nil {
		return err
	}
	if err := c.Deliverable.Validate(); err != nil {
		return fmt.Errorf("deliverable: %w", err)
	}
	if err := c.Heartbeat.Validate(); err != nil {
		return err
	}
	return nil
}

// Validate checks that exactly one Spec is non-nil for the given Type and that
// each spec's fields are valid. Nil Deliverable is valid (no deliverable configured).
func (d *DeliverableConfig) Validate() error {
	if d == nil {
		return nil
	}
	switch d.Type {
	case DeliverableTypeFilesystem:
		if d.Filesystem == nil {
			return fmt.Errorf("type is %q but filesystem spec is nil", d.Type)
		}
		if d.Filesystem.Glob == "" {
			return fmt.Errorf("filesystem.glob: is empty")
		}
	case DeliverableTypeDatabase:
		if d.Database == nil {
			return fmt.Errorf("type is %q but database spec is nil", d.Type)
		}
		if d.Database.Query == "" {
			return fmt.Errorf("database.query: is empty")
		}
		validConditions := map[string]bool{
			"exists":       true,
			"count_gt_0":   true,
			"value_equals": true,
			"value_neq":    true,
			"value_gt":     true,
			"value_gte":    true,
			"value_lt":     true,
			"value_lte":    true,
			"row_count_eq":  true,
			"row_count_neq": true,
			"row_count_gt":  true,
			"row_count_gte": true,
			"row_count_lt":  true,
			"row_count_lte": true,
		}
		if !validConditions[d.Database.Condition] {
			return fmt.Errorf("database.condition: must be one of: exists, count_gt_0, value_equals, value_neq, value_gt, value_gte, value_lt, value_lte, row_count_eq, row_count_neq, row_count_gt, row_count_gte, row_count_lt, row_count_lte")
		}
	case DeliverableTypeWebhook:
		if d.Webhook == nil {
			return fmt.Errorf("type is %q but webhook spec is nil", d.Type)
		}
		if d.Webhook.URL == "" {
			return fmt.Errorf("webhook.url: is empty")
		}
		if d.Webhook.Condition != "received" && d.Webhook.Condition != "status_2xx" {
			return fmt.Errorf("webhook.condition: must be one of received, status_2xx")
		}
		if d.Webhook.AuthHeader != "" && d.Webhook.AuthHeaderValue == "" {
			return fmt.Errorf("webhook.authHeader: is set but authHeaderValue is empty")
		}
		if d.Webhook.AuthHeader == "" && d.Webhook.AuthHeaderValue != "" {
			return fmt.Errorf("webhook.authHeaderValue: is set but authHeader is empty")
		}
		if d.Webhook.BearerToken != "" && (d.Webhook.AuthHeader != "" || d.Webhook.AuthHeaderValue != "") {
			return fmt.Errorf("webhook.bearerToken: cannot be used with authHeader or authHeaderValue")
		}
	default:
		return fmt.Errorf("unknown deliverable type %q", d.Type)
	}
	return nil
}

func validateNoWhitespaceField(name, value string) error {
	if value == "" {
		return nil
	}
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%s: must not have leading or trailing whitespace", name)
	}
	return nil
}

func validateNameComponent(name, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	if strings.ContainsAny(trimmed, `/\`) || trimmed == "." || trimmed == ".." {
		return fmt.Errorf("%s: must not contain path separators or traversal components", name)
	}
	return nil
}

// validateRepoRelative refuses paths that would let a project config escape
// its repo root: absolute paths and any ".." segment (before or after Clean).
// The same guard runs at spawn time as defense-in-depth, but enforcing it here
// rejects bad config when it is set rather than at every later spawn.
func validateRepoRelative(p string) error {
	trimmed := strings.TrimSpace(p)
	if trimmed == "" {
		return nil
	}
	if filepath.IsAbs(trimmed) || strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, `\`) {
		return fmt.Errorf("path must be repo-relative and must not escape the project root")
	}
	clean := filepath.Clean(trimmed)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path must be repo-relative and must not escape the project root")
	}
	for _, seg := range strings.Split(filepath.ToSlash(clean), "/") {
		if seg == ".." {
			return fmt.Errorf("path must be repo-relative and must not escape the project root")
		}
	}
	return nil
}
