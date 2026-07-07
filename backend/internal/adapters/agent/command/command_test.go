package command

import (
	"context"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestGetLaunchCommandRequiresArgv(t *testing.T) {
	t.Parallel()
	_, err := (&Plugin{}).GetLaunchCommand(context.Background(), ports.LaunchConfig{})
	if err == nil {
		t.Fatal("expected error when command argv is unset")
	}
}

func TestGetLaunchCommandReturnsConfiguredArgv(t *testing.T) {
	t.Parallel()
	argv := []string{"python", "agents/bank-auditor.py"}
	cmd, err := (&Plugin{}).GetLaunchCommand(context.Background(), ports.LaunchConfig{
		Config: ports.AgentConfig{Command: argv},
	})
	if err != nil {
		t.Fatalf("GetLaunchCommand: %v", err)
	}
	if len(cmd) != 2 || cmd[0] != "python" || cmd[1] != "agents/bank-auditor.py" {
		t.Fatalf("cmd = %#v, want %#v", cmd, argv)
	}
}

func TestGetRestoreCommandNotSupported(t *testing.T) {
	t.Parallel()
	_, ok, err := (&Plugin{}).GetRestoreCommand(context.Background(), ports.RestoreConfig{})
	if err != nil {
		t.Fatalf("GetRestoreCommand: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for command harness restore")
	}
}

func TestAuthStatusAuthorized(t *testing.T) {
	t.Parallel()
	status, err := (&Plugin{}).AuthStatus(context.Background())
	if err != nil {
		t.Fatalf("AuthStatus: %v", err)
	}
	if status != ports.AgentAuthStatusAuthorized {
		t.Fatalf("status = %q, want authorized", status)
	}
}
