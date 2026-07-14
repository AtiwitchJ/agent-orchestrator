package domain

import (
	"fmt"
	"strings"
)

// PermissionMode controls how much review an agent requires before acting. It
// lives in domain (not ports) so the typed AgentConfig can carry it; ports
// re-exports it as a type alias so agent adapters keep referring to
// ports.PermissionMode unchanged.
type PermissionMode string

// The permission modes adapters map onto their agent's native approval flags.
const (
	// PermissionModeDefault is special: adapters choose their own baseline
	// behavior for it. Most defer to the agent's own config; some managed
	// adapters may map it to a safer non-interactive default.
	PermissionModeDefault           PermissionMode = "default"
	PermissionModeAcceptEdits       PermissionMode = "accept-edits"
	PermissionModeAuto              PermissionMode = "auto"
	PermissionModeBypassPermissions PermissionMode = "bypass-permissions"
)

// AgentConfig is the typed per-project agent configuration. It replaces the
// former free-form map so the fields are validated and the API/UI render a
// real form rather than arbitrary JSON. An empty value (IsZero) means unset.
type AgentConfig struct {
	// Model overrides the agent's default model (e.g. claude-opus-4-5).
	Model string `json:"model,omitempty"`
	// Permissions sets the agent's starting permission mode. Empty is treated
	// like the adapter's default mode.
	Permissions PermissionMode `json:"permissions,omitempty"`
	// Command is the argv for the command harness (HarnessCommand). The first
	// element must be an executable on PATH or an absolute path. Prompt and
	// system instructions are exported as AO_PROMPT and AO_SYSTEM_PROMPT.
	Command []string `json:"command,omitempty"`
}

// IsZero reports whether the config carries no settings, so storage can persist
// SQL NULL and resolution can skip an empty config.
func (c AgentConfig) IsZero() bool {
	return c.Model == "" && c.Permissions == "" && len(c.Command) == 0
}

// Validate rejects values outside the typed vocabulary so a bad config is
// refused when it is set (CLI/API) rather than silently dropped at spawn.
func (c AgentConfig) Validate() error {
	for i, arg := range c.Command {
		if strings.TrimSpace(arg) == "" {
			return fmt.Errorf("command[%d]: must not be empty", i)
		}
	}
	switch c.Permissions {
	case "", PermissionModeDefault, PermissionModeAcceptEdits, PermissionModeAuto, PermissionModeBypassPermissions:
		return nil
	default:
		return fmt.Errorf("invalid permissions %q: want one of default, accept-edits, auto, bypass-permissions", c.Permissions)
	}
}
