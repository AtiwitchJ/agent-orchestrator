package openclaw

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/modernagent/modern-agent/backend/internal/ports"
)

func TestOpenclawProvidersAuthStatusAuthorizedWithAPIKey(t *testing.T) {
	path := writeopenclawProviders(t, `[{"id":"anthropic","api_key":"sk-test"}]`)

	status, ok, err := openclawProvidersAuthStatus(path)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || status != ports.AgentAuthStatusAuthorized {
		t.Fatalf("status = (%q, %v), want (%q, true)", status, ok, ports.AgentAuthStatusAuthorized)
	}
}

func TestOpenclawProvidersAuthStatusUnauthorizedWithEmptyAPIKeys(t *testing.T) {
	path := writeopenclawProviders(t, `[{"id":"anthropic","api_key":""}]`)

	status, ok, err := openclawProvidersAuthStatus(path)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || status != ports.AgentAuthStatusUnauthorized {
		t.Fatalf("status = (%q, %v), want (%q, true)", status, ok, ports.AgentAuthStatusUnauthorized)
	}
}

func writeopenclawProviders(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "providers.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
