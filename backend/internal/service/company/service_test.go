package company_test

import (
	"context"
	"errors"
	"os/exec"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/company"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/project"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

// gitRepo creates a real git repository in a fresh temp dir and returns its
// path — project.Add requires a real repo, mirroring service/project's own
// test helper (unexported there, so duplicated here for this package).
func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-b", "main", dir).CombinedOutput(); err != nil {
		t.Fatalf("git unavailable: %v (%s)", err, out)
	}
	return dir
}

func ptr(s string) *string { return &s }

// newManagers builds a company.Manager and a project.Manager over the same
// throwaway sqlite store, so tests can register a project and then assign it
// to a company.
func newManagers(t *testing.T) (company.Manager, project.Manager) {
	t.Helper()
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return company.New(store), project.New(store)
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var e *apierr.Error
	if !errors.As(err, &e) {
		t.Fatalf("error = %v, want *apierr.Error", err)
	}
	if e.Code != code {
		t.Fatalf("code = %q, want %q", e.Code, code)
	}
}

func TestManager_CreateListRoundTrip(t *testing.T) {
	ctx := context.Background()
	m, _ := newManagers(t)

	if got, err := m.List(ctx); err != nil || len(got) != 0 {
		t.Fatalf("List() = %v, %v; want empty", got, err)
	}

	c, err := m.Create(ctx, company.CreateInput{Name: "Acme Corp"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if c.ID != "acme-corp" || c.Name != "Acme Corp" || c.CreatedAt.IsZero() {
		t.Fatalf("Create returned %#v", c)
	}

	list, err := m.List(ctx)
	if err != nil || len(list) != 1 || list[0].ID != "acme-corp" {
		t.Fatalf("List() = %v, %v; want [acme-corp]", list, err)
	}
}

func TestManager_CreateRejectsEmptyName(t *testing.T) {
	ctx := context.Background()
	m, _ := newManagers(t)

	_, err := m.Create(ctx, company.CreateInput{Name: "   "})
	if err == nil {
		t.Fatal("Create with blank name = nil error, want NAME_REQUIRED")
	}
	wantCode(t, err, "NAME_REQUIRED")
}

// TestManager_CreateDedupesSlugCollisions locks the collision-avoidance
// contract: two companies with the same (or same-slugifying) name get
// distinct, deterministic ids rather than clobbering each other.
func TestManager_CreateDedupesSlugCollisions(t *testing.T) {
	ctx := context.Background()
	m, _ := newManagers(t)

	first, err := m.Create(ctx, company.CreateInput{Name: "Acme Corp"})
	if err != nil {
		t.Fatalf("Create first: %v", err)
	}
	second, err := m.Create(ctx, company.CreateInput{Name: "Acme Corp"})
	if err != nil {
		t.Fatalf("Create second: %v", err)
	}
	if first.ID != "acme-corp" || second.ID != "acme-corp-2" {
		t.Fatalf("ids = %q, %q; want acme-corp, acme-corp-2", first.ID, second.ID)
	}
}

func TestManager_AssignProjectRoundTrip(t *testing.T) {
	ctx := context.Background()
	cm, pm := newManagers(t)

	proj, err := pm.Add(ctx, project.AddInput{Path: gitRepo(t), ProjectID: ptr("ao")})
	if err != nil {
		t.Fatalf("Add project: %v", err)
	}
	c, err := cm.Create(ctx, company.CreateInput{Name: "Acme"})
	if err != nil {
		t.Fatalf("Create company: %v", err)
	}

	if err := cm.AssignProject(ctx, proj.ID, company.AssignProjectInput{CompanyID: c.ID}); err != nil {
		t.Fatalf("AssignProject: %v", err)
	}
	got, err := pm.Get(ctx, proj.ID)
	if err != nil || got.Project == nil || got.Project.ID != proj.ID {
		t.Fatalf("Get project: %v, %#v", err, got)
	}
	if list, _ := pm.List(ctx); len(list) != 1 || list[0].CompanyID != c.ID {
		t.Fatalf("project summary company id = %#v, want %q", list, c.ID)
	}

	// Unassign.
	if err := cm.AssignProject(ctx, proj.ID, company.AssignProjectInput{CompanyID: ""}); err != nil {
		t.Fatalf("unassign: %v", err)
	}
	if list, _ := pm.List(ctx); len(list) != 1 || list[0].CompanyID != "" {
		t.Fatalf("project summary company id after unassign = %#v, want empty", list)
	}
}

func TestManager_AssignProjectRejectsUnknownCompany(t *testing.T) {
	ctx := context.Background()
	cm, pm := newManagers(t)

	proj, err := pm.Add(ctx, project.AddInput{Path: gitRepo(t), ProjectID: ptr("ao")})
	if err != nil {
		t.Fatalf("Add project: %v", err)
	}
	err = cm.AssignProject(ctx, proj.ID, company.AssignProjectInput{CompanyID: "no-such-company"})
	if err == nil {
		t.Fatal("AssignProject with unknown company = nil error, want COMPANY_NOT_FOUND")
	}
	wantCode(t, err, "COMPANY_NOT_FOUND")
}

func TestManager_AssignProjectRejectsUnknownProject(t *testing.T) {
	ctx := context.Background()
	cm, _ := newManagers(t)

	c, err := cm.Create(ctx, company.CreateInput{Name: "Acme"})
	if err != nil {
		t.Fatalf("Create company: %v", err)
	}
	err = cm.AssignProject(ctx, domain.ProjectID("no-such-project"), company.AssignProjectInput{CompanyID: c.ID})
	if err == nil {
		t.Fatal("AssignProject on unknown project = nil error, want PROJECT_NOT_FOUND")
	}
	wantCode(t, err, "PROJECT_NOT_FOUND")
}
