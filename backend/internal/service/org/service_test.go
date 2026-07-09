package org_test

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/httpd/apierr"
	companysvc "github.com/modernagent/modern-agent/backend/internal/service/company"
	"github.com/modernagent/modern-agent/backend/internal/service/org"
	projectsvc "github.com/modernagent/modern-agent/backend/internal/service/project"
	"github.com/modernagent/modern-agent/backend/internal/storage/sqlite"
)

// gitRepo creates a real git repository in a fresh temp dir — project.Add
// requires a real repo (mirrors service/company's own test helper).
func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-b", "main", dir).CombinedOutput(); err != nil {
		t.Fatalf("git unavailable: %v (%s)", err, out)
	}
	return dir
}

func ptr(s string) *string { return &s }

// managers builds an org.Manager alongside company.Manager and project.Manager
// over the same throwaway sqlite store, so tests can register projects, group
// them under companies, and then exercise HQ assignment.
func managers(t *testing.T) (org.Manager, companysvc.Manager, projectsvc.Manager, *sqlite.Store) {
	t.Helper()
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return org.New(store), companysvc.New(store), projectsvc.New(store), store
}

// managersWithProvisioning is like managers but wires org.Deps.Projects and
// org.Deps.DataDir, so EnsureHoldingHQ/EnsureCompanyHQ can actually
// auto-provision a repo (a fresh directory under a throwaway data dir).
func managersWithProvisioning(t *testing.T) (org.Manager, companysvc.Manager, projectsvc.Manager, *sqlite.Store) {
	t.Helper()
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	pm := projectsvc.New(store)
	om := org.NewWithDeps(org.Deps{Store: store, Projects: pm, DataDir: t.TempDir()})
	return om, companysvc.New(store), pm, store
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

func TestSetHQRole_CompanyRequiresCompanyAssignment(t *testing.T) {
	ctx := context.Background()
	om, _, pm, _ := managers(t)

	proj, err := pm.Add(ctx, projectsvc.AddInput{Path: gitRepo(t), ProjectID: ptr("acme-hq")})
	if err != nil {
		t.Fatalf("add project: %v", err)
	}
	err = om.SetHQRole(ctx, proj.ID, org.SetHQRoleInput{Role: "company"})
	if err == nil {
		t.Fatal("SetHQRole company on unassigned project = nil error, want HQ_REQUIRES_COMPANY")
	}
	wantCode(t, err, "HQ_REQUIRES_COMPANY")
}

func TestSetHQRole_CompanyRoundTripAndUniqueness(t *testing.T) {
	ctx := context.Background()
	om, cm, pm, _ := managers(t)

	c, err := cm.Create(ctx, companysvc.CreateInput{Name: "Acme"})
	if err != nil {
		t.Fatalf("create company: %v", err)
	}
	hq, err := pm.Add(ctx, projectsvc.AddInput{Path: gitRepo(t), ProjectID: ptr("acme-hq")})
	if err != nil {
		t.Fatalf("add hq project: %v", err)
	}
	other, err := pm.Add(ctx, projectsvc.AddInput{Path: gitRepo(t), ProjectID: ptr("acme-hq-2")})
	if err != nil {
		t.Fatalf("add second project: %v", err)
	}
	if err := cm.AssignProject(ctx, hq.ID, companysvc.AssignProjectInput{CompanyID: c.ID}); err != nil {
		t.Fatalf("assign hq to company: %v", err)
	}
	if err := cm.AssignProject(ctx, other.ID, companysvc.AssignProjectInput{CompanyID: c.ID}); err != nil {
		t.Fatalf("assign second project to company: %v", err)
	}

	if err := om.SetHQRole(ctx, hq.ID, org.SetHQRoleInput{Role: "company"}); err != nil {
		t.Fatalf("SetHQRole: %v", err)
	}

	// A second company HQ for the same company is rejected before it ever
	// reaches the DB's partial unique index.
	err = om.SetHQRole(ctx, other.ID, org.SetHQRoleInput{Role: "company"})
	if err == nil {
		t.Fatal("SetHQRole second company HQ = nil error, want COMPANY_HQ_EXISTS")
	}
	wantCode(t, err, "COMPANY_HQ_EXISTS")

	// Clearing (role == "") frees the slot for a different project.
	if err := om.SetHQRole(ctx, hq.ID, org.SetHQRoleInput{Role: ""}); err != nil {
		t.Fatalf("clear hq role: %v", err)
	}
	if err := om.SetHQRole(ctx, other.ID, org.SetHQRoleInput{Role: "company"}); err != nil {
		t.Fatalf("SetHQRole after clear: %v", err)
	}
}

func TestSetHQRole_HoldingRequiresNoCompany(t *testing.T) {
	ctx := context.Background()
	om, cm, pm, _ := managers(t)

	c, err := cm.Create(ctx, companysvc.CreateInput{Name: "Acme"})
	if err != nil {
		t.Fatalf("create company: %v", err)
	}
	proj, err := pm.Add(ctx, projectsvc.AddInput{Path: gitRepo(t), ProjectID: ptr("holding-hq")})
	if err != nil {
		t.Fatalf("add project: %v", err)
	}
	if err := cm.AssignProject(ctx, proj.ID, companysvc.AssignProjectInput{CompanyID: c.ID}); err != nil {
		t.Fatalf("assign project to company: %v", err)
	}

	err = om.SetHQRole(ctx, proj.ID, org.SetHQRoleInput{Role: "holding"})
	if err == nil {
		t.Fatal("SetHQRole holding on a company-assigned project = nil error, want HOLDING_HQ_REQUIRES_NO_COMPANY")
	}
	wantCode(t, err, "HOLDING_HQ_REQUIRES_NO_COMPANY")
}

func TestSetHQRole_HoldingUniqueness(t *testing.T) {
	ctx := context.Background()
	om, _, pm, _ := managers(t)

	first, err := pm.Add(ctx, projectsvc.AddInput{Path: gitRepo(t), ProjectID: ptr("holding-hq")})
	if err != nil {
		t.Fatalf("add first project: %v", err)
	}
	second, err := pm.Add(ctx, projectsvc.AddInput{Path: gitRepo(t), ProjectID: ptr("holding-hq-2")})
	if err != nil {
		t.Fatalf("add second project: %v", err)
	}

	if err := om.SetHQRole(ctx, first.ID, org.SetHQRoleInput{Role: "holding"}); err != nil {
		t.Fatalf("SetHQRole: %v", err)
	}
	err = om.SetHQRole(ctx, second.ID, org.SetHQRoleInput{Role: "holding"})
	if err == nil {
		t.Fatal("SetHQRole second holding HQ = nil error, want HOLDING_HQ_EXISTS")
	}
	wantCode(t, err, "HOLDING_HQ_EXISTS")
}

func TestSetHQRole_RejectsUnknownProjectAndRole(t *testing.T) {
	ctx := context.Background()
	om, _, pm, _ := managers(t)

	err := om.SetHQRole(ctx, domain.ProjectID("no-such-project"), org.SetHQRoleInput{Role: "company"})
	if err == nil {
		t.Fatal("SetHQRole on unknown project = nil error, want PROJECT_NOT_FOUND")
	}
	wantCode(t, err, "PROJECT_NOT_FOUND")

	proj, err := pm.Add(ctx, projectsvc.AddInput{Path: gitRepo(t), ProjectID: ptr("p1")})
	if err != nil {
		t.Fatalf("add project: %v", err)
	}
	err = om.SetHQRole(ctx, proj.ID, org.SetHQRoleInput{Role: "empire"})
	if err == nil {
		t.Fatal("SetHQRole with unknown role = nil error, want HQ_ROLE_INVALID")
	}
	wantCode(t, err, "HQ_ROLE_INVALID")
}

func TestHeartbeatPauseDefaultsFalseAndRoundTrips(t *testing.T) {
	ctx := context.Background()
	om, _, _, _ := managers(t)

	paused, err := om.HeartbeatPaused(ctx)
	if err != nil || paused {
		t.Fatalf("HeartbeatPaused on fresh store = %v, %v; want false, nil", paused, err)
	}

	if err := om.SetHeartbeatPaused(ctx, true); err != nil {
		t.Fatalf("SetHeartbeatPaused(true): %v", err)
	}
	paused, err = om.HeartbeatPaused(ctx)
	if err != nil || !paused {
		t.Fatalf("HeartbeatPaused after pause = %v, %v; want true, nil", paused, err)
	}

	if err := om.SetHeartbeatPaused(ctx, false); err != nil {
		t.Fatalf("SetHeartbeatPaused(false): %v", err)
	}
	paused, err = om.HeartbeatPaused(ctx)
	if err != nil || paused {
		t.Fatalf("HeartbeatPaused after resume = %v, %v; want false, nil", paused, err)
	}
}

// TestOverview_HoldingCompaniesAndProjects covers the full tree assembly: a
// holding HQ, a company with its own HQ plus one ordinary delivery project
// (with a running orchestrator and one active worker), and confirms the HQ
// projects are never double-listed as ordinary projects.
func TestOverview_HoldingCompaniesAndProjects(t *testing.T) {
	ctx := context.Background()
	om, cm, pm, store := managers(t)

	holdingHQ, err := pm.Add(ctx, projectsvc.AddInput{Path: gitRepo(t), ProjectID: ptr("uppu-hq")})
	if err != nil {
		t.Fatalf("add holding hq: %v", err)
	}
	if err := om.SetHQRole(ctx, holdingHQ.ID, org.SetHQRoleInput{Role: "holding"}); err != nil {
		t.Fatalf("set holding hq role: %v", err)
	}

	c, err := cm.Create(ctx, companysvc.CreateInput{Name: "Acme"})
	if err != nil {
		t.Fatalf("create company: %v", err)
	}
	acmeHQ, err := pm.Add(ctx, projectsvc.AddInput{Path: gitRepo(t), ProjectID: ptr("acme-hq")})
	if err != nil {
		t.Fatalf("add acme hq: %v", err)
	}
	if err := cm.AssignProject(ctx, acmeHQ.ID, companysvc.AssignProjectInput{CompanyID: c.ID}); err != nil {
		t.Fatalf("assign acme hq to company: %v", err)
	}
	if err := om.SetHQRole(ctx, acmeHQ.ID, org.SetHQRoleInput{Role: "company"}); err != nil {
		t.Fatalf("set acme hq role: %v", err)
	}

	delivery, err := pm.Add(ctx, projectsvc.AddInput{Path: gitRepo(t), ProjectID: ptr("acme-api")})
	if err != nil {
		t.Fatalf("add delivery project: %v", err)
	}
	if err := cm.AssignProject(ctx, delivery.ID, companysvc.AssignProjectInput{CompanyID: c.ID}); err != nil {
		t.Fatalf("assign delivery project to company: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	if _, err := store.CreateSession(ctx, domain.SessionRecord{
		ProjectID: delivery.ID, Kind: domain.KindOrchestrator, Harness: domain.HarnessClaudeCode,
		Activity: domain.Activity{State: domain.ActivityIdle, LastActivityAt: now}, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create orchestrator session: %v", err)
	}
	if _, err := store.CreateSession(ctx, domain.SessionRecord{
		ProjectID: delivery.ID, Kind: domain.KindWorker, Harness: domain.HarnessClaudeCode,
		Activity: domain.Activity{State: domain.ActivityActive, LastActivityAt: now}, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create worker session: %v", err)
	}

	ov, err := om.Overview(ctx)
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if ov.Paused {
		t.Fatal("Overview.Paused = true on a fresh store")
	}
	if ov.HoldingHQ == nil || ov.HoldingHQ.ProjectID != "uppu-hq" {
		t.Fatalf("Overview.HoldingHQ = %#v, want project uppu-hq", ov.HoldingHQ)
	}
	if len(ov.Companies) != 1 {
		t.Fatalf("Overview.Companies = %#v, want 1 company", ov.Companies)
	}
	co := ov.Companies[0]
	if co.ID != c.ID || co.HQ == nil || co.HQ.ProjectID != "acme-hq" {
		t.Fatalf("company overview = %#v, want hq acme-hq", co)
	}
	if len(co.Projects) != 1 || co.Projects[0].ID != "acme-api" {
		t.Fatalf("company projects = %#v, want [acme-api]", co.Projects)
	}
	ps := co.Projects[0]
	if ps.OrchestratorSessionID == "" || ps.OrchestratorActivity != "idle" {
		t.Fatalf("delivery project orchestrator status = %#v, want idle orchestrator", ps)
	}
	if ps.TotalSessions != 2 || ps.ActiveSessions != 1 {
		t.Fatalf("delivery project session counts = %#v, want total=2 active=1", ps)
	}
}

// TestEnsureHoldingHQ_ProvisionsAndIsIdempotent covers the whole point of
// auto-provisioning: no caller ever passes a path — the holding CEO HQ is a
// singleton that "just exists" the first time anything asks for it, and every
// later call returns the exact same project id without creating a second one.
func TestEnsureHoldingHQ_ProvisionsAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	om, _, pm, _ := managersWithProvisioning(t)

	id, err := om.EnsureHoldingHQ(ctx)
	if err != nil {
		t.Fatalf("EnsureHoldingHQ: %v", err)
	}
	if id == "" {
		t.Fatal("EnsureHoldingHQ returned empty project id")
	}
	proj, err := pm.Get(ctx, domain.ProjectID(id))
	if err != nil || proj.Project == nil {
		t.Fatalf("Get provisioned project: %v, %#v", err, proj)
	}

	again, err := om.EnsureHoldingHQ(ctx)
	if err != nil {
		t.Fatalf("EnsureHoldingHQ (second call): %v", err)
	}
	if again != id {
		t.Fatalf("EnsureHoldingHQ second call = %q, want the same id %q", again, id)
	}

	all, err := pm.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	holdingHQCount := 0
	for _, p := range all {
		if p.HQRole == "holding" {
			holdingHQCount++
		}
	}
	if holdingHQCount != 1 {
		t.Fatalf("holding HQ project count = %d, want exactly 1 (idempotent provisioning)", holdingHQCount)
	}
}

// TestEnsureCompanyHQ_ProvisionsAssignsCompanyAndIsIdempotent covers the same
// idempotent-provisioning contract for a company PM HQ, plus that the
// auto-provisioned project ends up assigned to the right company.
func TestEnsureCompanyHQ_ProvisionsAssignsCompanyAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	om, cm, pm, _ := managersWithProvisioning(t)

	c, err := cm.Create(ctx, companysvc.CreateInput{Name: "Acme"})
	if err != nil {
		t.Fatalf("create company: %v", err)
	}

	id, err := om.EnsureCompanyHQ(ctx, c.ID)
	if err != nil {
		t.Fatalf("EnsureCompanyHQ: %v", err)
	}
	list, err := pm.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var found *projectsvc.Summary
	for i := range list {
		if string(list[i].ID) == id {
			found = &list[i]
		}
	}
	if found == nil {
		t.Fatalf("provisioned project %q not found in List(): %#v", id, list)
	}
	if found.CompanyID != c.ID || found.HQRole != "company" {
		t.Fatalf("provisioned project = %#v, want companyId=%q hqRole=company", found, c.ID)
	}

	again, err := om.EnsureCompanyHQ(ctx, c.ID)
	if err != nil {
		t.Fatalf("EnsureCompanyHQ (second call): %v", err)
	}
	if again != id {
		t.Fatalf("EnsureCompanyHQ second call = %q, want the same id %q", again, id)
	}
}

func TestEnsureCompanyHQ_RejectsEmptyCompanyID(t *testing.T) {
	ctx := context.Background()
	om, _, _, _ := managersWithProvisioning(t)

	_, err := om.EnsureCompanyHQ(ctx, "  ")
	if err == nil {
		t.Fatal("EnsureCompanyHQ with blank companyId = nil error, want COMPANY_ID_REQUIRED")
	}
	wantCode(t, err, "COMPANY_ID_REQUIRED")
}

// TestEnsureHoldingHQ_UnavailableWithoutProvisioningDeps covers the degrade
// path: a Service built via org.New (no Projects/DataDir, e.g. most other
// tests in this file) reports a clear error instead of panicking when asked
// to provision — but still resolves an already-existing holding HQ without
// needing those deps at all.
func TestEnsureHoldingHQ_UnavailableWithoutProvisioningDeps(t *testing.T) {
	ctx := context.Background()
	om, _, pm, _ := managers(t)

	_, err := om.EnsureHoldingHQ(ctx)
	if err == nil {
		t.Fatal("EnsureHoldingHQ without provisioning deps = nil error, want HQ_PROVISION_UNAVAILABLE")
	}
	wantCode(t, err, "HQ_PROVISION_UNAVAILABLE")

	// An already-registered holding HQ resolves fine even without provisioning
	// deps configured — only the create path needs them.
	proj, err := pm.Add(ctx, projectsvc.AddInput{Path: gitRepo(t), ProjectID: ptr("existing-hq")})
	if err != nil {
		t.Fatalf("add project: %v", err)
	}
	if err := om.SetHQRole(ctx, proj.ID, org.SetHQRoleInput{Role: "holding"}); err != nil {
		t.Fatalf("set hq role: %v", err)
	}
	id, err := om.EnsureHoldingHQ(ctx)
	if err != nil {
		t.Fatalf("EnsureHoldingHQ with existing holding hq: %v", err)
	}
	if id != "existing-hq" {
		t.Fatalf("EnsureHoldingHQ = %q, want existing-hq", id)
	}
}
