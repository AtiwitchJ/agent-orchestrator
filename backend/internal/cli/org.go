package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// orgOverviewDTO mirrors the daemon's org.Overview (via
// controllers.OrgOverviewResponse) for GET /api/v1/org/overview. The CLI
// keeps its own copy so it need not import the service/org package.
type orgOverviewDTO struct {
	Overview orgOverview `json:"overview"`
}

type orgOverview struct {
	HoldingHQ *orgHQInfoDTO        `json:"holdingHq,omitempty"`
	Companies []orgCompanyOverview `json:"companies"`
	Paused    bool                 `json:"paused"`
}

type orgCompanyOverview struct {
	ID       string             `json:"id"`
	Name     string             `json:"name"`
	HQ       *orgHQInfoDTO      `json:"hq,omitempty"`
	Projects []orgProjectStatus `json:"projects"`
}

type orgHQInfoDTO struct {
	ProjectID             string `json:"projectId"`
	OrchestratorSessionID string `json:"orchestratorSessionId,omitempty"`
	Activity              string `json:"activity,omitempty"`
	HeartbeatEnabled      bool   `json:"heartbeatEnabled"`
	HeartbeatInterval     string `json:"heartbeatInterval,omitempty"`
}

type orgProjectStatus struct {
	ID                    string `json:"id"`
	Name                  string `json:"name"`
	Kind                  string `json:"kind"`
	OrchestratorSessionID string `json:"orchestratorSessionId,omitempty"`
	OrchestratorActivity  string `json:"orchestratorActivity,omitempty"`
	ActiveSessions        int    `json:"activeSessions"`
	TotalSessions         int    `json:"totalSessions"`
}

type orgHeartbeatDTO struct {
	Paused bool `json:"paused"`
}

type orgStatusOptions struct {
	company string
	json    bool
}

func newOrgCommand(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "org",
		Short: "Inspect and control the holding/company org hierarchy",
	}
	cmd.AddCommand(newOrgStatusCommand(ctx))
	cmd.AddCommand(newOrgPauseCommand(ctx))
	cmd.AddCommand(newOrgResumeCommand(ctx))
	return cmd
}

func newOrgStatusCommand(ctx *commandContext) *cobra.Command {
	var opts orgStatusOptions
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the holding tree: holding HQ, companies, their HQ and delivery projects",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var res orgOverviewDTO
			if err := ctx.getJSON(cmd.Context(), "org/overview", &res); err != nil {
				return err
			}
			ov := res.Overview
			company := strings.TrimSpace(opts.company)
			if company != "" {
				found := false
				for _, c := range ov.Companies {
					if c.ID == company {
						ov.Companies = []orgCompanyOverview{c}
						found = true
						break
					}
				}
				if !found {
					return fmt.Errorf("company %q not found", company)
				}
				ov.HoldingHQ = nil
			}
			if opts.json {
				return writeJSON(cmd.OutOrStdout(), ov)
			}
			return writeOrgStatus(cmd, ov)
		},
	}
	cmd.Flags().StringVar(&opts.company, "company", "", "Limit the report to one company id")
	cmd.Flags().BoolVar(&opts.json, "json", false, "Output as JSON")
	return cmd
}

func newOrgPauseCommand(ctx *commandContext) *cobra.Command {
	return &cobra.Command{
		Use:   "pause",
		Short: "Pause the global heartbeat kill switch (stop nudging PM/CEO orchestrators)",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return ctx.setOrgHeartbeatPause(cmd, true)
		},
	}
}

func newOrgResumeCommand(ctx *commandContext) *cobra.Command {
	return &cobra.Command{
		Use:   "resume",
		Short: "Resume the global heartbeat kill switch",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return ctx.setOrgHeartbeatPause(cmd, false)
		},
	}
}

func (c *commandContext) setOrgHeartbeatPause(cmd *cobra.Command, paused bool) error {
	var res orgHeartbeatDTO
	if err := c.putJSON(cmd.Context(), "org/heartbeat", orgHeartbeatDTO{Paused: paused}, &res); err != nil {
		return err
	}
	state := "resumed"
	if res.Paused {
		state = "paused"
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "heartbeat %s\n", state)
	return err
}

func writeOrgStatus(cmd *cobra.Command, ov orgOverview) error {
	out := cmd.OutOrStdout()
	pauseLabel := "running"
	if ov.Paused {
		pauseLabel = "paused"
	}
	if _, err := fmt.Fprintf(out, "heartbeat: %s\n", pauseLabel); err != nil {
		return err
	}
	if ov.HoldingHQ != nil {
		if _, err := fmt.Fprintf(out, "holding hq: %s%s\n", ov.HoldingHQ.ProjectID, hqSuffix(*ov.HoldingHQ)); err != nil {
			return err
		}
	}
	if len(ov.Companies) == 0 {
		_, err := fmt.Fprintln(out, "(no companies)")
		return err
	}
	for _, c := range ov.Companies {
		if _, err := fmt.Fprintf(out, "\n%s (%s):\n", c.ID, c.Name); err != nil {
			return err
		}
		if c.HQ != nil {
			if _, err := fmt.Fprintf(out, "  hq: %s%s\n", c.HQ.ProjectID, hqSuffix(*c.HQ)); err != nil {
				return err
			}
		} else {
			if _, err := fmt.Fprintln(out, "  hq: (none)"); err != nil {
				return err
			}
		}
		if len(c.Projects) == 0 {
			if _, err := fmt.Fprintln(out, "  (no projects)"); err != nil {
				return err
			}
			continue
		}
		for _, p := range c.Projects {
			orch := "no orchestrator running"
			if p.OrchestratorSessionID != "" {
				orch = fmt.Sprintf("orchestrator %s (%s)", p.OrchestratorSessionID, p.OrchestratorActivity)
			}
			if _, err := fmt.Fprintf(out, "  %s (%s) — %s, sessions %d/%d\n", p.ID, p.Name, orch, p.ActiveSessions, p.TotalSessions); err != nil {
				return err
			}
		}
	}
	return nil
}

func hqSuffix(hq orgHQInfoDTO) string {
	if hq.OrchestratorSessionID == "" {
		return " — no orchestrator running"
	}
	heartbeat := "heartbeat off"
	if hq.HeartbeatEnabled {
		heartbeat = "heartbeat every " + hq.HeartbeatInterval
	}
	return fmt.Sprintf(" — orchestrator %s (%s), %s", hq.OrchestratorSessionID, hq.Activity, heartbeat)
}
