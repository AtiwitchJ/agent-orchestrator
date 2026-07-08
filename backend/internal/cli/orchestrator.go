package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type orchestratorListOptions struct {
	json bool
}

type orchestratorListOutput struct {
	Data []sessionListEntry `json:"data"`
}

func newOrchestratorCommand(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "orchestrator",
		Short: "Manage orchestrator sessions",
	}
	cmd.AddCommand(newOrchestratorListCommand(ctx))
	cmd.AddCommand(newOrchestratorSpawnCommand(ctx))
	return cmd
}

type orchestratorSpawnOptions struct {
	project string
	clean   bool
}

// spawnOrchestratorRequest mirrors the daemon's SpawnOrchestratorRequest body
// for POST /api/v1/orchestrators. The CLI keeps its own copy so it need not
// import httpd.
type spawnOrchestratorRequest struct {
	ProjectID string `json:"projectId"`
	Clean     bool   `json:"clean,omitempty"`
}

type spawnOrchestratorResult struct {
	Orchestrator struct {
		ID        string `json:"id"`
		ProjectID string `json:"projectId"`
	} `json:"orchestrator"`
}

func newOrchestratorSpawnCommand(ctx *commandContext) *cobra.Command {
	var opts orchestratorSpawnOptions
	cmd := &cobra.Command{
		Use:   "spawn",
		Short: "Spawn (or replace) a project's orchestrator session",
		Long: "Spawn a project's orchestrator session. A project has at most one active\n" +
			"orchestrator: without --clean, an existing active orchestrator is returned\n" +
			"unchanged; with --clean, it is retired and a fresh one is spawned.",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(opts.project) == "" {
				return usageError{fmt.Errorf("--project is required")}
			}
			var res spawnOrchestratorResult
			req := spawnOrchestratorRequest{ProjectID: opts.project, Clean: opts.clean}
			if err := ctx.postJSON(cmd.Context(), "orchestrators", req, &res); err != nil {
				return err
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "spawned orchestrator %s for project %s\n", res.Orchestrator.ID, res.Orchestrator.ProjectID)
			return err
		},
	}
	cmd.Flags().StringVar(&opts.project, "project", "", "Project id to spawn the orchestrator in (required)")
	cmd.Flags().BoolVar(&opts.clean, "clean", false, "Retire any existing active orchestrator and spawn a fresh one")
	return cmd
}

func newOrchestratorListCommand(ctx *commandContext) *cobra.Command {
	var opts orchestratorListOptions
	cmd := &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List orchestrator sessions",
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return ctx.listOrchestrators(cmd.Context(), cmd, opts)
		},
	}
	cmd.Flags().BoolVar(&opts.json, "json", false, "Output as JSON")
	return cmd
}

func (c *commandContext) listOrchestrators(ctx context.Context, cmd *cobra.Command, opts orchestratorListOptions) error {
	var res sessionListResponse
	if err := c.getJSON(ctx, "orchestrators", &res); err != nil {
		return err
	}
	orchestrators := filterAndSortOrchestrators(res.Sessions)
	if opts.json {
		return writeJSON(cmd.OutOrStdout(), orchestratorListOutput{Data: sessionListEntries(orchestrators)})
	}
	return writeOrchestratorList(cmd, orchestrators)
}

func filterAndSortOrchestrators(sessions []sessionDTO) []sessionDTO {
	out := make([]sessionDTO, 0, len(sessions))
	for _, sess := range sessions {
		if sess.Kind != "orchestrator" {
			continue
		}
		out = append(out, sess)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ProjectID != out[j].ProjectID {
			return out[i].ProjectID < out[j].ProjectID
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func writeOrchestratorList(cmd *cobra.Command, sessions []sessionDTO) error {
	out := cmd.OutOrStdout()
	if len(sessions) == 0 {
		_, err := fmt.Fprintln(out, "(no orchestrators)")
		return err
	}
	currentProject := ""
	for _, sess := range sessions {
		if sess.ProjectID != currentProject {
			if currentProject != "" {
				if _, err := fmt.Fprintln(out); err != nil {
					return err
				}
			}
			currentProject = sess.ProjectID
			if _, err := fmt.Fprintf(out, "%s:\n", currentProject); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(out, "  %s", sess.ID); err != nil {
			return err
		}
		parts := orchestratorLineParts(sess)
		if len(parts) > 0 {
			if _, err := fmt.Fprintf(out, "  %s", strings.Join(parts, "  ")); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(out); err != nil {
			return err
		}
	}
	return nil
}

func orchestratorLineParts(sess sessionDTO) []string {
	parts := []string{}
	if !sess.Activity.LastActivityAt.IsZero() {
		parts = append(parts, "("+formatSessionAge(time.Since(sess.Activity.LastActivityAt))+")")
	}
	if sess.Status != "" {
		parts = append(parts, "["+sess.Status+"]")
	}
	if sess.IsTerminated {
		parts = append(parts, "terminated")
	}
	return parts
}
