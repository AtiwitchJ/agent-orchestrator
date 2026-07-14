package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/modernagent/modern-agent/backend/internal/policy"
)

// policyConfigDTO mirrors the daemon's persisted PolicyConfig payload so the
// CLI can read and write it without importing the controller package. The daemon
// fuses it with policy.DefaultPolicyConfig on PUT (see Task 9 controller),
// meaning any field omitted in the request keeps its default.
type policyConfigDTO struct {
	Enabled              bool   `json:"enabled,omitempty"`
	TrackerLabel         string `json:"trackerLabel,omitempty"`
	AutoFixOnCIFailure   bool   `json:"autoFixOnCiFailure,omitempty"`
	MaxAutoFixRounds     int    `json:"maxAutoFixRounds,omitempty"`
	RequireAgentReview   bool   `json:"requireAgentReview,omitempty"`
	ReviewStrategy       string `json:"reviewStrategy,omitempty"`
	ReviewAgent          string `json:"reviewAgent,omitempty"`
	MaxReviseRounds      int    `json:"maxReviseRounds,omitempty"`
	RequireHumanApproval bool   `json:"requireHumanApproval,omitempty"`
	HumanTimeoutHours    int    `json:"humanTimeoutHours,omitempty"`
	AgentFinalPass       bool   `json:"agentFinalPass,omitempty"`
	VetoSecondAgent      string `json:"vetoSecondAgent,omitempty"`
	MergeStrategy        string `json:"mergeStrategy,omitempty"`
	MinPRAgeMinutes      int    `json:"minPrAgeMinutes,omitempty"`
	BlockOnDraft         bool   `json:"blockOnDraft,omitempty"`
}

// policyConfigResponse mirrors controllers.PolicyConfigResponse.
type policyConfigResponse struct {
	ProjectID string          `json:"projectId"`
	Config    policyConfigDTO `json:"config"`
}

// policyGateResultDTO mirrors controllers.GateResultDTO.
type policyGateResultDTO struct {
	RunID         string `json:"runId"`
	GateID        string `json:"gateId"`
	Attempt       int    `json:"attempt"`
	Outcome       string `json:"outcome"`
	Reason        string `json:"reason,omitempty"`
	SecondVote    string `json:"secondVote,omitempty"`
	Justification string `json:"justification,omitempty"`
	DurationMS    int64  `json:"durationMs"`
}

// policyRunDTO mirrors controllers.PolicyRunDTO.
type policyRunDTO struct {
	ID          string                `json:"id"`
	ProjectID   string                `json:"projectId"`
	SessionID   string                `json:"sessionId"`
	PRID        string                `json:"prId"`
	Config      policyConfigDTO       `json:"config"`
	CurrentGate string                `json:"currentGate"`
	FinalState  string                `json:"finalState"`
	StartedAt   string                `json:"startedAt"`
	UpdatedAt   string                `json:"updatedAt"`
	History     []policyGateResultDTO `json:"history"`
}

// policyRunsResponse mirrors controllers.PolicyRunsResponse (paginated).
type policyRunsResponse struct {
	Runs       []policyRunDTO `json:"runs"`
	NextCursor string         `json:"nextCursor,omitempty"`
}

// policyDecideRequest mirrors controllers.PolicyDecideRequest.
type policyDecideRequest struct {
	Action        string `json:"action"`
	Justification string `json:"justification,omitempty"`
	Message       string `json:"message,omitempty"`
}

// policySetOptions captures every flag accepted by `ao policy set`. Flags not
// set are not included in the payload, which lets the controller merge over
// existing values rather than zero them out.
type policySetOptions struct {
	enabled                bool
	trackerLabel           string
	autoFix                bool
	noAutoFix              bool
	maxAutoFixRounds       int
	requireAgentReview     bool
	noRequireAgentReview   bool
	reviewStrategy         string
	reviewAgent            string
	maxReviseRounds        int
	requireHumanApproval   bool
	noRequireHumanApproval bool
	humanTimeoutHours      int
	agentFinalPass         bool
	noAgentFinalPass       bool
	vetoSecondAgent        string
	mergeStrategy          string
	minPRAgeMinutes        int
	blockOnDraft           bool
	noBlockOnDraft         bool
	clear                  bool
}

// policyDecideOptions captures the mutually exclusive decision flags for
// `ao policy decide`. Exactly one of approve/requestChanges/overrideWith
// must be non-empty.
type policyDecideOptions struct {
	approve        bool
	requestChanges string
	overrideWith   string
}

func newPolicyCommand(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "Manage hybrid approval-gate policy for projects and worker runs",
	}
	cmd.AddCommand(newPolicyShowCommand(ctx))
	cmd.AddCommand(newPolicySetCommand(ctx))
	cmd.AddCommand(newPolicyRunsCommand(ctx))
	cmd.AddCommand(newPolicyDecideCommand(ctx))
	return cmd
}

// newPolicyShowCommand builds `ao policy show <project>`. It mirrors the
// daemon's GET /api/v1/projects/{id}/policy endpoint and prints the merged
// policy config as JSON by default (the show verb is intentionally
// machine-friendly since the same payload backs downstream tooling).
func newPolicyShowCommand(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <project>",
		Short: "Show the merged policy configuration for a project",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(cmd, args); err != nil {
				return usageError{err}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return ctx.showPolicy(cmd, args[0])
		},
	}
	return cmd
}

func (c *commandContext) showPolicy(cmd *cobra.Command, projectID string) error {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return usageError{errors.New("usage: project id is required")}
	}
	path := "projects/" + url.PathEscape(projectID) + "/policy"
	var res policyConfigResponse
	if err := c.getJSON(cmd.Context(), path, &res); err != nil {
		return err
	}
	// Hard-coded JSON output: this command is the read surface for the typed
	// upstream pipeline (`ao` -> JSON -> script), and the project policy doc
	// explicitly chose structured output over tabular.
	return writeJSON(cmd.OutOrStdout(), res.Config)
}

// newPolicySetCommand builds `ao policy set <project> ...`. It maps the
// engine's PolicyConfig fields to explicit flags and emits a PUT to
// /api/v1/projects/{id}/policy. The daemon validates and merges over the
// current config rather than zeroing unset fields.
func newPolicySetCommand(ctx *commandContext) *cobra.Command {
	var opts policySetOptions
	cmd := &cobra.Command{
		Use:   "set <project>",
		Short: "Update the policy configuration for a project",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(cmd, args); err != nil {
				return usageError{err}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return ctx.setPolicy(cmd, args[0], opts)
		},
	}
	// Reviewer agents routinely spell flags with underscores rather than
	// hyphens; normalize so both forms resolve to the same flag, mirroring
	// the convention from `review submit`.
	cmd.Flags().SetNormalizeFunc(func(_ *pflag.FlagSet, name string) pflag.NormalizedName {
		return pflag.NormalizedName(strings.ReplaceAll(name, "_", "-"))
	})

	cmd.Flags().BoolVar(&opts.enabled, "enabled", false, "Enable policy for this project")
	cmd.Flags().StringVar(&opts.trackerLabel, "tracker-label", "", "Override the tracker label (e.g. agent-ready)")
	cmd.Flags().BoolVar(&opts.autoFix, "auto-fix", false, "Auto-fix on CI failure (Gate 1)")
	cmd.Flags().BoolVar(&opts.noAutoFix, "no-auto-fix", false, "Disable auto-fix on CI failure")
	cmd.Flags().IntVar(&opts.maxAutoFixRounds, "max-auto-fix-rounds", 0, "Maximum CI auto-fix rounds (0 = keep current; -1 = unset)")
	cmd.Flags().BoolVar(&opts.requireAgentReview, "require-agent-review", false, "Require agent self-review (Gate 2)")
	cmd.Flags().BoolVar(&opts.noRequireAgentReview, "no-require-agent-review", false, "Disable agent self-review")
	cmd.Flags().StringVar(&opts.reviewStrategy, "review-strategy", "", "Review strategy: same_agent|cross_agent|second_only")
	cmd.Flags().StringVar(&opts.reviewAgent, "review-agent", "", "Agent to use for cross_agent reviews (required when review-strategy=cross_agent)")
	cmd.Flags().IntVar(&opts.maxReviseRounds, "max-revise-rounds", 0, "Maximum revision rounds for Gate 2")
	cmd.Flags().BoolVar(&opts.requireHumanApproval, "require-human-approval", false, "Require explicit human approval (Gate 3)")
	cmd.Flags().BoolVar(&opts.noRequireHumanApproval, "no-require-human-approval", false, "Disable human approval")
	cmd.Flags().IntVar(&opts.humanTimeoutHours, "human-timeout-hours", 0, "Hours before a pending human approval is auto-escalated (0 = none)")
	cmd.Flags().BoolVar(&opts.agentFinalPass, "agent-final-pass", false, "Run a final-pass agent scan (Gate 4)")
	cmd.Flags().BoolVar(&opts.noAgentFinalPass, "no-agent-final-pass", false, "Disable final-pass agent")
	cmd.Flags().StringVar(&opts.vetoSecondAgent, "veto-second-agent", "", "Override the second-opinion agent for hybrid veto (default = review-agent)")
	cmd.Flags().StringVar(&opts.mergeStrategy, "merge-strategy", "", "Merge strategy: squash|rebase|merge")
	cmd.Flags().IntVar(&opts.minPRAgeMinutes, "min-pr-age-minutes", 0, "Minimum PR age in minutes before merge")
	cmd.Flags().BoolVar(&opts.blockOnDraft, "block-on-draft", false, "Block merge on draft PRs")
	cmd.Flags().BoolVar(&opts.noBlockOnDraft, "no-block-on-draft", false, "Allow merge on draft PRs")
	cmd.Flags().BoolVar(&opts.clear, "clear", false, "Reset every override to the default policy config")
	return cmd
}

// setPolicyDiff is the partial-update payload submitted to the daemon. The
// controller accepts a sparse DTO and merges missing fields against defaults,
// which is the behavior we want — `ao policy set --enabled` must not unset
// MaxAutoFixRounds back to zero.
type setPolicyDiff struct {
	Enabled              *bool   `json:"enabled,omitempty"`
	TrackerLabel         *string `json:"trackerLabel,omitempty"`
	AutoFixOnCIFailure   *bool   `json:"autoFixOnCiFailure,omitempty"`
	MaxAutoFixRounds     *int    `json:"maxAutoFixRounds,omitempty"`
	RequireAgentReview   *bool   `json:"requireAgentReview,omitempty"`
	ReviewStrategy       *string `json:"reviewStrategy,omitempty"`
	ReviewAgent          *string `json:"reviewAgent,omitempty"`
	MaxReviseRounds      *int    `json:"maxReviseRounds,omitempty"`
	RequireHumanApproval *bool   `json:"requireHumanApproval,omitempty"`
	HumanTimeoutHours    *int    `json:"humanTimeoutHours,omitempty"`
	AgentFinalPass       *bool   `json:"agentFinalPass,omitempty"`
	VetoSecondAgent      *string `json:"vetoSecondAgent,omitempty"`
	MergeStrategy        *string `json:"mergeStrategy,omitempty"`
	MinPRAgeMinutes      *int    `json:"minPrAgeMinutes,omitempty"`
	BlockOnDraft         *bool   `json:"blockOnDraft,omitempty"`
}

func (o policySetOptions) buildDiff() (setPolicyDiff, error) {
	d := setPolicyDiff{}
	// Resolve mutually-exclusive bool flags into a tri-state. A pair like
	// --no-agent-final-pass + --agent-final-pass is a usage error so we
	// surface the contradiction immediately rather than silently picking the
	// last flag (the actual zero value is never carried forward).
	if o.enabled && (false) {
		return d, usageError{errors.New("usage: --enabled requires removing --no-enabled")}
	}
	if o.noAutoFix && o.autoFix {
		return d, usageError{errors.New("usage: --auto-fix and --no-auto-fix are mutually exclusive")}
	}
	if o.noRequireAgentReview && o.requireAgentReview {
		return d, usageError{errors.New("usage: --require-agent-review and --no-require-agent-review are mutually exclusive")}
	}
	if o.noRequireHumanApproval && o.requireHumanApproval {
		return d, usageError{errors.New("usage: --require-human-approval and --no-require-human-approval are mutually exclusive")}
	}
	if o.noAgentFinalPass && o.agentFinalPass {
		return d, usageError{errors.New("usage: --agent-final-pass and --no-agent-final-pass are mutually exclusive")}
	}
	if o.noBlockOnDraft && o.blockOnDraft {
		return d, usageError{errors.New("usage: --block-on-draft and --no-block-on-draft are mutually exclusive")}
	}

	// --clear is exclusive with any other flag: the user is asking to reset
	// the overrides back to the default config wholesale, so we refuse to
	// guess what they meant when flags are mixed in. (Flag presence checks
	// happen in the wrap-up step below, in setPolicy.)

	// Resolve the bool tri-state and emit only non-default entries as omitempty pointers.
	b := func(v, neg bool) *bool {
		if v {
			t := true
			return &t
		}
		if neg {
			f := false
			return &f
		}
		return nil
	}
	d.Enabled = ptrTrue(o.enabled)
	d.AutoFixOnCIFailure = b(o.autoFix, o.noAutoFix)
	d.RequireAgentReview = b(o.requireAgentReview, o.noRequireAgentReview)
	d.RequireHumanApproval = b(o.requireHumanApproval, o.noRequireHumanApproval)
	d.AgentFinalPass = b(o.agentFinalPass, o.noAgentFinalPass)
	d.BlockOnDraft = b(o.blockOnDraft, o.noBlockOnDraft)

	if s := strings.TrimSpace(o.trackerLabel); s != "" {
		v := s
		d.TrackerLabel = &v
	}
	if s := strings.TrimSpace(o.reviewStrategy); s != "" {
		v := s
		d.ReviewStrategy = &v
	}
	if s := strings.TrimSpace(o.reviewAgent); s != "" {
		v := s
		d.ReviewAgent = &v
	}
	if s := strings.TrimSpace(o.vetoSecondAgent); s != "" {
		v := s
		d.VetoSecondAgent = &v
	}
	if s := strings.TrimSpace(o.mergeStrategy); s != "" {
		v := s
		d.MergeStrategy = &v
	}
	// Round fields accept 0 as "keep current"; we use -1 to explicitly unset
	// back to the default by passing the pointer-nil and the controller's
	// merge logic decides whether -1 means "drop to zero" or "use default".
	if o.maxAutoFixRounds != 0 {
		v := o.maxAutoFixRounds
		d.MaxAutoFixRounds = &v
	}
	if o.maxReviseRounds != 0 {
		v := o.maxReviseRounds
		d.MaxReviseRounds = &v
	}
	if o.humanTimeoutHours != 0 {
		v := o.humanTimeoutHours
		d.HumanTimeoutHours = &v
	}
	if o.minPRAgeMinutes != 0 {
		v := o.minPRAgeMinutes
		d.MinPRAgeMinutes = &v
	}
	return d, nil
}

func ptrTrue(v bool) *bool {
	if !v {
		return nil
	}
	t := true
	return &t
}

func (c *commandContext) setPolicy(cmd *cobra.Command, projectID string, opts policySetOptions) error {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return usageError{errors.New("usage: project id is required")}
	}
	diff, err := opts.buildDiff()
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(diff)
	if err != nil {
		return err
	}
	// --clear is the only shortcut that touches every field; if it's set the
	// JSON should be empty and the controller resets to defaults. We surface
	// the contradiction between --clear and any other toggle as a usage
	// error to avoid surprising downstream pipelines.
	if opts.clear {
		var anyFlag bool
		// Re-introspect via reflection would couple us to the CLI internal
		// structure; instead, encode a second pass with all flags as if
		// --clear was used. The simpler approach: refuse any co-flags.
		// Re-encode opts to detect any other signals via a sentinel map of
		// values we just observed.
		_ = anyFlag
		if string(encoded) != "{}" {
			return usageError{errors.New("usage: --clear cannot be combined with other policy flags")}
		}
	}
	path := "projects/" + url.PathEscape(projectID) + "/policy"
	var res policyConfigResponse
	if err := c.putJSON(cmd.Context(), path, diff, &res); err != nil {
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "policy updated for %s\n", projectID)
	return err
}

// newPolicyRunsCommand builds `ao policy runs <project> [flags]`. It lists
// recent policy_runs for the supplied project (or session when --session is
// passed) so operators can audit the gate history without running the desktop.
func newPolicyRunsCommand(ctx *commandContext) *cobra.Command {
	var session string
	var limit int
	cmd := &cobra.Command{
		Use:   "runs <session>",
		Short: "List recent policy runs for a worker session",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(cmd, args); err != nil {
				return usageError{err}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return ctx.listPolicyRuns(cmd, args[0], session, limit)
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "Worker session id (overrides the positional argument)")
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum number of runs to return (most recent first)")
	return cmd
}

func (c *commandContext) listPolicyRuns(cmd *cobra.Command, sessionArg, sessionFlag string, limit int) error {
	session := strings.TrimSpace(sessionFlag)
	if session == "" {
		session = strings.TrimSpace(sessionArg)
	}
	if session == "" {
		return usageError{errors.New("usage: session id is required (positional or --session)")}
	}
	if limit <= 0 || limit > 200 {
		// The HTTP layer caps at 200 anyway, but a sane local cap keeps the
		// output readable from the terminal.
		limit = 50
	}
	q := url.Values{"limit": {fmt.Sprintf("%d", limit)}}
	path := "sessions/" + url.PathEscape(session) + "/policy/runs?" + q.Encode()
	var res policyRunsResponse
	if err := c.getJSON(cmd.Context(), path, &res); err != nil {
		return err
	}
	return writeJSON(cmd.OutOrStdout(), res)
}

// newPolicyDecideCommand builds `ao policy decide <runId>`. The verb is the
// public entry into the hybrid-veto override path (design doc §3.1) and the
// Gate 3 human-approve path. Exactly one action flag must be supplied.
func newPolicyDecideCommand(ctx *commandContext) *cobra.Command {
	var opts policyDecideOptions
	cmd := &cobra.Command{
		Use:   "decide <runId>",
		Short: "Record a human decision against a running policy run (Gate 3 approve / request_changes / Gate 4 override)",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(cmd, args); err != nil {
				return usageError{err}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return ctx.policyDecide(cmd, args[0], opts)
		},
	}
	cmd.Flags().BoolVar(&opts.approve, "approve", false, "Approve this gate (advance the run)")
	cmd.Flags().StringVar(&opts.requestChanges, "request-changes", "", "Send the work back to Gate 2 with the supplied reason")
	cmd.Flags().StringVar(&opts.overrideWith, "override-with", "", "Override a Gate 4 veto with the supplied justification")
	return cmd
}

func (c *commandContext) policyDecide(cmd *cobra.Command, runID string, opts policyDecideOptions) error {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return usageError{errors.New("usage: runId is required")}
	}
	selected := 0
	if opts.approve {
		selected++
	}
	if strings.TrimSpace(opts.requestChanges) != "" {
		selected++
	}
	if strings.TrimSpace(opts.overrideWith) != "" {
		selected++
	}
	if selected != 1 {
		return usageError{errors.New("usage: exactly one of --approve, --request-changes, or --override-with is required")}
	}
	var req policyDecideRequest
	switch {
	case opts.approve:
		req.Action = policy.DecisionApprove
	case opts.requestChanges != "":
		req.Action = policy.DecisionRequestChanges
		req.Message = strings.TrimSpace(opts.requestChanges)
	case opts.overrideWith != "":
		req.Action = policy.DecisionOverride
		req.Justification = strings.TrimSpace(opts.overrideWith)
	}
	if err := req.toDecision().Validate(); err != nil {
		// The daemon also validates, but surfacing the offense locally gives
		// the operator a 2-exit usage error before any network roundtrip.
		return usageError{err}
	}
	path := "policy/runs/" + url.PathEscape(runID) + "/decide"
	var res policyRunDTO
	if err := c.postJSON(cmd.Context(), path, req, &res); err != nil {
		return err
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "decision %s recorded for run %s\n", req.Action, runID)
	return err
}

// toDecision converts a CLI-shaped request into the canonical engine Decision
// so the local Validate() call can mirror the daemon-side guardrails.
func (r policyDecideRequest) toDecision() policy.Decision {
	return policy.Decision{
		Action:        r.Action,
		Justification: r.Justification,
	}
}
