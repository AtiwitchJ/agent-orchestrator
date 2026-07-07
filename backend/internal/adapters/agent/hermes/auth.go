package hermes

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/modernagent/modern-agent/backend/internal/adapters/agent/authprobe"
	"github.com/modernagent/modern-agent/backend/internal/ports"
)

var _ ports.AgentAuthChecker = (*Plugin)(nil)

// AuthStatus returns the plugin's local authentication status.
func (p *Plugin) AuthStatus(ctx context.Context) (ports.AgentAuthStatus, error) {
	binary, err := p.ResolveBinary(ctx)
	if err != nil {
		return ports.AgentAuthStatusUnknown, err
	}
	if status, ok, err := hermesLocalAuthStatus(ctx); err != nil {
		return ports.AgentAuthStatusUnknown, err
	} else if ok {
		return status, nil
	}
	return authprobe.CLIStatus(ctx, binary, nil)
}

func hermesLocalAuthStatus(ctx context.Context) (ports.AgentAuthStatus, bool, error) {
	if err := ctx.Err(); err != nil {
		return ports.AgentAuthStatusUnknown, false, err
	}
	dataDir, ok := hermesDataDir()
	if !ok {
		return ports.AgentAuthStatusUnknown, false, nil
	}
	return hermesProvidersAuthStatus(filepath.Join(dataDir, "providers.json"))
}

func hermesDataDir() (string, bool) {
	if dataDir := strings.TrimSpace(os.Getenv("hermes_DATA_DIR")); dataDir != "" {
		return dataDir, true
	}
	if dataHome := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); dataHome != "" {
		return filepath.Join(dataHome, "hermes"), true
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", false
	}
	return filepath.Join(home, ".local", "share", "hermes"), true
}

type hermesProviderAuth struct {
	APIKey string `json:"api_key"`
}

func hermesProvidersAuthStatus(path string) (ports.AgentAuthStatus, bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ports.AgentAuthStatusUnknown, false, nil
	}
	if err != nil {
		return ports.AgentAuthStatusUnknown, false, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return ports.AgentAuthStatusUnknown, false, nil
	}

	var providers []hermesProviderAuth
	if err := json.Unmarshal(data, &providers); err != nil {
		return ports.AgentAuthStatusUnknown, false, err
	}
	if len(providers) == 0 {
		return ports.AgentAuthStatusUnknown, false, nil
	}
	for _, provider := range providers {
		if strings.TrimSpace(provider.APIKey) != "" {
			return ports.AgentAuthStatusAuthorized, true, nil
		}
	}
	return ports.AgentAuthStatusUnauthorized, true, nil
}
