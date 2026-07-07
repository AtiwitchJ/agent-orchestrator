package openclaw

import (
	"context"

	"github.com/modernagent/modern-agent/backend/internal/ports"
)

// GetAgentHooks is a no-op for openclaw since it doesn't have full hooks support
// like Claude Code and Codex. openclaw doesn't have a native hook configuration system
// that AO can integrate with for session metadata tracking.
//
// TODO(openclaw): Implement hook installation once openclaw has native hook support.
// Until then, session metadata tracking is not available.
func (p *Plugin) GetAgentHooks(ctx context.Context, cfg ports.WorkspaceHookConfig) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// No-op for now since openclaw doesn't have full hooks support
	return nil
}

// UninstallHooks is a no-op for openclaw.
func (p *Plugin) UninstallHooks(ctx context.Context, workspacePath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// No-op for now since openclaw doesn't have full hooks support
	return nil
}

// AreHooksInstalled is a no-op for openclaw.
func (p *Plugin) AreHooksInstalled(ctx context.Context, workspacePath string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	// No-op for now since openclaw doesn't have full hooks support
	return false, nil
}
