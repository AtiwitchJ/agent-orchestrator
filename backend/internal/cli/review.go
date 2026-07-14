package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// reviewRun mirrors the daemon's domain.ReviewRun for the CLI client.
type reviewRun struct {
	ID             string     `json:"id"`
	SessionID      string     `json:"sessionId"`
	BatchID        string     `json:"batchId"`
	Harness        string     `json:"harness"`
	PRURL          string     `json:"prUrl"`
	TargetSHA      string     `json:"targetSha"`
	Status         string     `json:"status"`
	Verdict        string     `json:"verdict"`
	Body           string     `json:"body"`
	GithubReviewID string     `json:"githubReviewId"`
	CreatedAt      time.Time  `json:"createdAt"`
	DeliveredAt    *time.Time `json:"deliveredAt,omitempty"`
}

// reviewRunResponse mirrors controllers.ReviewRunResponse.
type reviewRunResponse struct {
	Review           reviewRun   `json:"review"`
	Reviews          []reviewRun `json:"reviews"`
	ReviewerHandleID string      `json:"reviewerHandleId"`
}

// submitReviewItem mirrors controllers.SubmitReviewItem.
type submitReviewItem struct {
	RunID          string `json:"runId"`
	Verdict        string `json:"verdict"`
	Body           string `json:"body,omitempty"`
	GithubReviewID string `json:"githubReviewId,omitempty"`
}

// submitReviewRequest mirrors controllers.SubmitReviewInput.
type submitReviewRequest struct {
	RunID          string             `json:"runId,omitempty"`
	Verdict        string             `json:"verdict,omitempty"`
	Body           string             `json:"body,omitempty"`
	GithubReviewID string             `json:"githubReviewId,omitempty"`
	Reviews        []submitReviewItem `json:"reviews,omitempty"`
}

type reviewSubmitOptions struct {
	session  string
	runID    string
	verdict  string
	body     string
	reviewID string
	reviews  string
}

// prReviewStateDTO mirrors internal/review.PRReviewState, the per-PR review
// status returned by list and trigger.
type prReviewStateDTO struct {
	PRURL     string     `json:"prUrl"`
	PRNumber  int        `json:"prNumber"`
	Title     string     `json:"title"`
	TargetSHA string     `json:"targetSha"`
	Status    string     `json:"status"`
	LatestRun *reviewRun `json:"latestRun,omitempty"`
}

// reviewsListResponse mirrors controllers.ListReviewsResponse /
// controllers.TriggerReviewResponse — the two share the same shape.
type reviewsListResponse struct {
	ReviewerHandleID string             `json:"reviewerHandleId"`
	Reviews          []prReviewStateDTO `json:"reviews"`
}

// reviewListEntry is one flattened review row: a session's prReviewStateDTO
// plus the owning session id, so `ao review list` can print a project-wide
// view even though reviews are attributed per-session on the wire.
type reviewListEntry struct {
	SessionID string `json:"sessionId"`
	prReviewStateDTO
}

func newReviewCommand(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "review",
		Short: "Manage AO code reviews of a worker's PR",
	}
	cmd.AddCommand(newReviewListCommand(ctx))
	cmd.AddCommand(newReviewExecuteCommand(ctx))
	cmd.AddCommand(newReviewSendCommand(ctx))
	cmd.AddCommand(newReviewSubmitCommand(ctx))
	return cmd
}

// newReviewListCommand builds `ao review list <project>`. There is no
// project-scoped review listing endpoint; reviews are attributed per-session
// (GET /sessions/{sessionId}/reviews), so this fans out over
// GET /sessions?project=<project>&active=true first, mirroring `ao pr list`.
func newReviewListCommand(ctx *commandContext) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list <project>",
		Short: "List code reviews for a project's active worker sessions",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(cmd, args); err != nil {
				return usageError{err}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return ctx.listProjectReviews(cmd, args[0], asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func (c *commandContext) listProjectReviews(cmd *cobra.Command, project string, asJSON bool) error {
	project = strings.TrimSpace(project)
	if project == "" {
		return usageError{errors.New("usage: project id is required")}
	}
	params := url.Values{"project": {project}, "active": {"true"}}
	var sessions prListResponse
	if err := c.getJSON(cmd.Context(), apiPath("sessions", params), &sessions); err != nil {
		return err
	}
	entries := make([]reviewListEntry, 0)
	for _, sess := range sessions.Sessions {
		var res reviewsListResponse
		path := "sessions/" + url.PathEscape(sess.ID) + "/reviews"
		if err := c.getJSON(cmd.Context(), path, &res); err != nil {
			return err
		}
		for _, r := range res.Reviews {
			entries = append(entries, reviewListEntry{SessionID: sess.ID, prReviewStateDTO: r})
		}
	}
	if asJSON {
		return writeJSON(cmd.OutOrStdout(), struct {
			Reviews []reviewListEntry `json:"reviews"`
		}{entries})
	}
	return writeReviewList(cmd, project, entries)
}

func writeReviewList(cmd *cobra.Command, project string, entries []reviewListEntry) error {
	out := cmd.OutOrStdout()
	if len(entries) == 0 {
		_, err := fmt.Fprintf(out, "(no reviews for %s)\n", project)
		return err
	}
	for _, e := range entries {
		verdict := ""
		if e.LatestRun != nil {
			verdict = e.LatestRun.Verdict
		}
		if _, err := fmt.Fprintf(out, "%s  #%d  %s  status=%s  verdict=%s  %s\n",
			e.SessionID, e.PRNumber, e.Title, e.Status, verdict, e.PRURL); err != nil {
			return err
		}
	}
	return nil
}

// newReviewExecuteCommand builds `ao review execute <session>`, invoking the
// existing POST /sessions/{sessionId}/reviews/trigger endpoint. The plan
// framed this as project-scoped, but a review trigger evaluates one worker
// session's current PR — there is no HTTP surface to trigger "a project"
// (which may have zero, one, or many active sessions) — so this takes the
// same session-id argument as `review submit`.
func newReviewExecuteCommand(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "execute <session>",
		Short: "Trigger a code review of a worker session's pull request",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(cmd, args); err != nil {
				return usageError{err}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return ctx.executeReview(cmd, args[0])
		},
	}
	return cmd
}

func (c *commandContext) executeReview(cmd *cobra.Command, session string) error {
	session = strings.TrimSpace(session)
	if session == "" {
		return usageError{errors.New("usage: session id is required")}
	}
	path := "sessions/" + url.PathEscape(session) + "/reviews/trigger"
	var res reviewsListResponse
	if err := c.postJSON(cmd.Context(), path, nil, &res); err != nil {
		return err
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "triggered review for %s (%d PR(s))\n", session, len(res.Reviews))
	return err
}

type reviewSendOptions struct {
	verdict  string
	body     string
	reviewID string
}

// newReviewSendCommand builds `ao review send <session> <reviewId>`, a
// positional-argument shorthand for `review submit`'s single-review path —
// submitting a verdict requires a session (the run is session-scoped) and the
// verdict/body only the reviewer has after doing the review, so those stay
// flags; only session and reviewId move from flags to positional args.
func newReviewSendCommand(ctx *commandContext) *cobra.Command {
	var opts reviewSendOptions
	cmd := &cobra.Command{
		Use:   "send <session> <reviewId>",
		Short: "Record and deliver a reviewer's verdict for a worker's PR",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(2)(cmd, args); err != nil {
				return usageError{err}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return ctx.sendReview(cmd, args[0], args[1], opts)
		},
	}
	cmd.Flags().SetNormalizeFunc(func(_ *pflag.FlagSet, name string) pflag.NormalizedName {
		return pflag.NormalizedName(strings.ReplaceAll(name, "_", "-"))
	})
	cmd.Flags().StringVar(&opts.verdict, "verdict", "", "Review verdict: approved or changes_requested (required)")
	cmd.Flags().StringVar(&opts.body, "body", "", "Review body: a path to a Markdown file, or - to read from stdin (so nothing is written into the worktree)")
	cmd.Flags().StringVar(&opts.reviewID, "review-id", "", "Id of the GitHub PR review just posted (the .id from the gh api POST that created the review)")
	return cmd
}

func (c *commandContext) sendReview(cmd *cobra.Command, session, reviewID string, opts reviewSendOptions) error {
	session = strings.TrimSpace(session)
	if session == "" {
		return usageError{errors.New("usage: session id is required")}
	}
	reviewID = strings.TrimSpace(reviewID)
	if reviewID == "" {
		return usageError{errors.New("usage: reviewId is required")}
	}
	verdict := strings.TrimSpace(opts.verdict)
	if verdict == "" {
		return usageError{errors.New("usage: --verdict is required (approved or changes_requested)")}
	}
	body, err := readReviewBodyArg(cmd, opts.body)
	if err != nil {
		return err
	}
	path := "sessions/" + url.PathEscape(session) + "/reviews/submit"
	var res reviewRunResponse
	req := submitReviewRequest{RunID: reviewID, Verdict: verdict, Body: body, GithubReviewID: strings.TrimSpace(opts.reviewID)}
	if err := c.postJSON(cmd.Context(), path, req, &res); err != nil {
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "sent %s review for %s\n", res.Review.Verdict, session)
	return err
}

func newReviewSubmitCommand(ctx *commandContext) *cobra.Command {
	var opts reviewSubmitOptions
	cmd := &cobra.Command{
		Use:   "submit [worker-session-id]",
		Short: "Record a reviewer's result for a worker's PR",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return ctx.submitReview(cmd, args, opts)
		},
	}
	// Reviewer agents routinely spell flags with underscores (--review_id) rather
	// than hyphens (--review-id); normalize so both resolve to the same flag.
	cmd.Flags().SetNormalizeFunc(func(_ *pflag.FlagSet, name string) pflag.NormalizedName {
		return pflag.NormalizedName(strings.ReplaceAll(name, "_", "-"))
	})
	cmd.Flags().StringVar(&opts.session, "session", "", "Worker session id (or pass it as the positional argument)")
	cmd.Flags().StringVar(&opts.runID, "run", "", "Review run id (required)")
	cmd.Flags().StringVar(&opts.verdict, "verdict", "", "Review verdict: approved or changes_requested (required)")
	cmd.Flags().StringVar(&opts.body, "body", "", "Review body: a path to a Markdown file, or - to read from stdin (so nothing is written into the worktree)")
	cmd.Flags().StringVar(&opts.reviewID, "review-id", "", "Id of the GitHub PR review just posted (the .id from the gh api POST that created the review)")
	cmd.Flags().StringVar(&opts.reviews, "reviews", "", "JSON review results array or object: a path, or - to read from stdin")
	return cmd
}

func (c *commandContext) submitReview(cmd *cobra.Command, args []string, opts reviewSubmitOptions) error {
	session := strings.TrimSpace(opts.session)
	if len(args) == 1 {
		session = strings.TrimSpace(args[0])
	}
	if session == "" {
		return usageError{errors.New("usage: worker session id is required (positional or --session)")}
	}
	if strings.TrimSpace(opts.reviews) != "" {
		return c.submitReviewBatch(cmd, session, opts)
	}
	runID := strings.TrimSpace(opts.runID)
	if runID == "" {
		return usageError{errors.New("usage: --run is required")}
	}
	verdict := strings.TrimSpace(opts.verdict)
	if verdict == "" {
		return usageError{errors.New("usage: --verdict is required (approved or changes_requested)")}
	}
	body, err := readReviewBodyArg(cmd, opts.body)
	if err != nil {
		return err
	}
	reviewID := strings.TrimSpace(opts.reviewID)
	path := "sessions/" + url.PathEscape(session) + "/reviews/submit"
	var res reviewRunResponse
	if err := c.postJSON(cmd.Context(), path, submitReviewRequest{RunID: runID, Verdict: verdict, Body: body, GithubReviewID: reviewID}, &res); err != nil {
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "recorded %s review for %s\n", res.Review.Verdict, session)
	return err
}

func (c *commandContext) submitReviewBatch(cmd *cobra.Command, session string, opts reviewSubmitOptions) error {
	if strings.TrimSpace(opts.runID) != "" || strings.TrimSpace(opts.verdict) != "" || strings.TrimSpace(opts.body) != "" || strings.TrimSpace(opts.reviewID) != "" {
		return usageError{errors.New("usage: --reviews cannot be combined with --run, --verdict, --body, or --review-id")}
	}
	reviews, err := readReviewItems(cmd, strings.TrimSpace(opts.reviews))
	if err != nil {
		return err
	}
	path := "sessions/" + url.PathEscape(session) + "/reviews/submit"
	var res reviewRunResponse
	if err := c.postJSON(cmd.Context(), path, submitReviewRequest{Reviews: reviews}, &res); err != nil {
		return err
	}
	count := len(res.Reviews)
	if count == 0 {
		count = len(reviews)
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "recorded %d review(s) for %s\n", count, session)
	return err
}

// readReviewBodyArg reads a review body from a file path, or from stdin when
// path is "-", so the reviewer never has to write a file into its checkout
// (where it could be committed onto the worker branch). An empty path is a
// no-op (some verdicts, e.g. approved, carry no body).
func readReviewBodyArg(cmd *cobra.Command, path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
	var raw []byte
	var err error
	if path == "-" {
		raw, err = io.ReadAll(cmd.InOrStdin())
	} else {
		raw, err = os.ReadFile(path)
	}
	if err != nil {
		return "", usageError{fmt.Errorf("read review body: %w", err)}
	}
	return string(raw), nil
}

func readReviewItems(cmd *cobra.Command, path string) ([]submitReviewItem, error) {
	var raw []byte
	var err error
	if path == "-" {
		raw, err = io.ReadAll(cmd.InOrStdin())
	} else {
		raw, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, usageError{fmt.Errorf("read review results: %w", err)}
	}
	var req submitReviewRequest
	if err := json.Unmarshal(raw, &req); err == nil && len(req.Reviews) > 0 {
		return req.Reviews, nil
	}
	var reviews []submitReviewItem
	if err := json.Unmarshal(raw, &reviews); err != nil {
		return nil, usageError{fmt.Errorf("decode review results JSON: %w", err)}
	}
	if len(reviews) == 0 {
		return nil, usageError{errors.New("usage: --reviews requires at least one review result")}
	}
	return reviews, nil
}
