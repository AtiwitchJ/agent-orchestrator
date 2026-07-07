// Package command implements the generic command harness: it launches a
// project-configured argv for scripts, CLIs, and other workers that are not
// AI coding agents. Prompts are passed through AO_PROMPT / AO_SYSTEM_PROMPT;
// scripts report activity via `ao hooks command <event>`.
package command

import (
	"context"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

const adapterID = "command"

// Plugin is the command harness adapter.
type Plugin struct{}

// New returns a ready-to-register command adapter.
func New() *Plugin {
	return &Plugin{}
}

var _ adapters.Adapter = (*Plugin)(nil)
var _ ports.Agent = (*Plugin)(nil)
var _ ports.AgentAuthChecker = (*Plugin)(nil)

// Manifest returns the adapter's static self-description.
func (p *Plugin) Manifest() adapters.Manifest {
	return adapters.Manifest{
		ID:          adapterID,
		Name:        "Command",
		Description: "Run a project-configured command (scripts, CLIs, or other non-coding workers).",
		Version:     "0.0.1",
		Capabilities: []adapters.Capability{
			adapters.CapabilityAgent,
		},
	}
}

// GetConfigSpec reports the command argv field exposed in project config.
func (p *Plugin) GetConfigSpec(ctx context.Context) (ports.ConfigSpec, error) {
	if err := ctx.Err(); err != nil {
		return ports.ConfigSpec{}, err
	}
	return ports.ConfigSpec{
		Fields: []ports.ConfigField{
			{
				Key:         "command",
				Type:        ports.ConfigFieldStringList,
				Description: "Argv to execute (first element must be on PATH or an absolute path).",
				Required:    true,
			},
		},
	}, nil
}

// GetLaunchCommand returns the project-configured argv unchanged.
func (p *Plugin) GetLaunchCommand(ctx context.Context, cfg ports.LaunchConfig) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cmd := cfg.Config.Command
	if len(cmd) == 0 {
		return nil, fmt.Errorf("command harness requires agentConfig.command in project config")
	}
	argv := make([]string, len(cmd))
	copy(argv, cmd)
	return argv, nil
}

// GetPromptDeliveryStrategy reports that prompts are passed through env vars.
func (p *Plugin) GetPromptDeliveryStrategy(ctx context.Context, _ ports.LaunchConfig) (ports.PromptDeliveryStrategy, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return ports.PromptDeliveryAfterStart, nil
}

// GetAgentHooks is a no-op: command workers opt into activity via `ao hooks command`.
func (p *Plugin) GetAgentHooks(context.Context, ports.WorkspaceHookConfig) error {
	return nil
}

// GetRestoreCommand reports that command sessions cannot be natively resumed.
func (p *Plugin) GetRestoreCommand(ctx context.Context, _ ports.RestoreConfig) ([]string, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	return nil, false, nil
}

// SessionInfo reports no agent-owned metadata.
func (p *Plugin) SessionInfo(ctx context.Context, _ ports.SessionRef) (ports.SessionInfo, bool, error) {
	if err := ctx.Err(); err != nil {
		return ports.SessionInfo{}, false, err
	}
	return ports.SessionInfo{}, false, nil
}

// AuthStatus always reports authorized: readiness is argv resolution at spawn.
func (p *Plugin) AuthStatus(ctx context.Context) (ports.AgentAuthStatus, error) {
	if err := ctx.Err(); err != nil {
		return ports.AgentAuthStatusUnknown, err
	}
	return ports.AgentAuthStatusAuthorized, nil
}
