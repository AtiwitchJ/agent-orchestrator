package cli

import (
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// prListSessionView mirrors the fields of controllers.SessionView this
// command reads: the session id and its attributed pull requests.
type prListSessionView struct {
	ID  string         `json:"id"`
	PRs []sessionPRDTO `json:"prs"`
}

// prListResponse mirrors controllers.ListSessionsResponse, scoped to the
// fields prListSessionView needs.
type prListResponse struct {
	Sessions []prListSessionView `json:"sessions"`
}

// prListEntry is one flattened PR row: a session's sessionPRDTO plus the
// owning session id, so `ao pr list` can print a project-wide view even
// though PRs are attributed per-session on the wire.
type prListEntry struct {
	SessionID string `json:"sessionId"`
	sessionPRDTO
}

type prListOptions struct {
	json bool
}

// mergePRResponse mirrors controllers.MergePRResponse.
type mergePRResponse struct {
	OK       bool   `json:"ok"`
	PRNumber int    `json:"prNumber"`
	Method   string `json:"method"`
}

// resolveCommentsRequest mirrors controllers.ResolveCommentsRequest.
type resolveCommentsRequest struct {
	CommentIDs []string `json:"commentIds,omitempty"`
}

// resolveCommentsResponse mirrors controllers.ResolveCommentsResponse.
type resolveCommentsResponse struct {
	OK       bool `json:"ok"`
	Resolved int  `json:"resolved"`
}

func newPRCommand(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pr",
		Short: "Inspect and act on pull requests opened by worker sessions",
	}
	cmd.AddCommand(newPRListCommand(ctx))
	cmd.AddCommand(newPRMergeCommand(ctx))
	cmd.AddCommand(newPRResolveCommentsCommand(ctx))
	return cmd
}

// newPRListCommand builds `ao pr list <project>`. There is no project-scoped
// PR listing endpoint; PRs are attributed per-session (SessionView.PRs), so
// this fans out over GET /sessions?project=<project>&active=true and
// flattens each session's PRs, mirroring `ao session ls`'s call pattern.
func newPRListCommand(ctx *commandContext) *cobra.Command {
	var opts prListOptions
	cmd := &cobra.Command{
		Use:   "list <project>",
		Short: "List pull requests opened by a project's active worker sessions",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(cmd, args); err != nil {
				return usageError{err}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return ctx.listProjectPRs(cmd, args[0], opts)
		},
	}
	cmd.Flags().BoolVar(&opts.json, "json", false, "Output as JSON")
	return cmd
}

func (c *commandContext) listProjectPRs(cmd *cobra.Command, project string, opts prListOptions) error {
	project = strings.TrimSpace(project)
	if project == "" {
		return usageError{errors.New("usage: project id is required")}
	}
	params := url.Values{"project": {project}, "active": {"true"}}
	var res prListResponse
	if err := c.getJSON(cmd.Context(), apiPath("sessions", params), &res); err != nil {
		return err
	}
	entries := make([]prListEntry, 0)
	for _, sess := range res.Sessions {
		for _, pr := range sess.PRs {
			entries = append(entries, prListEntry{SessionID: sess.ID, sessionPRDTO: pr})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].UpdatedAt.After(entries[j].UpdatedAt) })
	if opts.json {
		return writeJSON(cmd.OutOrStdout(), struct {
			PRs []prListEntry `json:"prs"`
		}{entries})
	}
	return writePRList(cmd, project, entries)
}

func writePRList(cmd *cobra.Command, project string, entries []prListEntry) error {
	out := cmd.OutOrStdout()
	if len(entries) == 0 {
		_, err := fmt.Fprintf(out, "(no open pull requests for %s)\n", project)
		return err
	}
	for _, e := range entries {
		if _, err := fmt.Fprintf(out, "%s  #%d  %s  ci=%s  review=%s  mergeability=%s  %s\n",
			e.SessionID, e.Number, e.State, e.CI, e.Review, e.Mergeability, e.URL); err != nil {
			return err
		}
	}
	return nil
}

// newPRMergeCommand builds `ao pr merge <prId>`, invoking the existing
// POST /api/v1/prs/{id}/merge endpoint.
func newPRMergeCommand(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "merge <prId>",
		Short: "Squash-merge a pull request",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(cmd, args); err != nil {
				return usageError{err}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return ctx.mergePR(cmd, args[0])
		},
	}
	return cmd
}

func (c *commandContext) mergePR(cmd *cobra.Command, prID string) error {
	prID = strings.TrimSpace(prID)
	if prID == "" {
		return usageError{errors.New("usage: prId is required")}
	}
	path := "prs/" + url.PathEscape(prID) + "/merge"
	var res mergePRResponse
	if err := c.postJSON(cmd.Context(), path, nil, &res); err != nil {
		return err
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "merged PR #%d (%s)\n", res.PRNumber, res.Method)
	return err
}

// newPRResolveCommentsCommand builds `ao pr resolve-comments <prId>`,
// invoking the existing POST /api/v1/prs/{id}/resolve-comments endpoint.
func newPRResolveCommentsCommand(ctx *commandContext) *cobra.Command {
	var commentIDs []string
	cmd := &cobra.Command{
		Use:   "resolve-comments <prId>",
		Short: "Resolve review comment threads on a pull request",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(cmd, args); err != nil {
				return usageError{err}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return ctx.resolvePRComments(cmd, args[0], commentIDs)
		},
	}
	cmd.Flags().StringSliceVar(&commentIDs, "comment-id", nil, "Resolve only this comment thread id (repeatable; omit to resolve all unresolved threads)")
	return cmd
}

func (c *commandContext) resolvePRComments(cmd *cobra.Command, prID string, commentIDs []string) error {
	prID = strings.TrimSpace(prID)
	if prID == "" {
		return usageError{errors.New("usage: prId is required")}
	}
	path := "prs/" + url.PathEscape(prID) + "/resolve-comments"
	var res resolveCommentsResponse
	if err := c.postJSON(cmd.Context(), path, resolveCommentsRequest{CommentIDs: commentIDs}, &res); err != nil {
		return err
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "resolved %d comment thread(s) on PR %s\n", res.Resolved, prID)
	return err
}
