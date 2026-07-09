package org

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/httpd/apierr"
	aoprocess "github.com/modernagent/modern-agent/backend/internal/process"
	projectsvc "github.com/modernagent/modern-agent/backend/internal/service/project"
)

// orgHQRoot is the subdirectory (under the daemon's data dir) that holds
// every auto-provisioned HQ repo. Keeping these under AO_DATA_DIR — not an
// arbitrary user-chosen folder — is what lets EnsureHoldingHQ/EnsureCompanyHQ
// provision an HQ without ever asking a human to pick a directory: the
// holding CEO and each company's PM are structural parts of the org, not
// ordinary delivery projects a human sets up by hand.
const orgHQRoot = "org-hq"

// holdingHQProjectID is the fixed project id for the auto-provisioned holding
// HQ. There is at most one holding HQ (see the HQRoleHolding partial unique
// index), so a fixed id is safe.
const holdingHQProjectID = "holding-hq"

// ProjectCreator is the narrow project-registration surface
// EnsureHoldingHQ/EnsureCompanyHQ need to register an auto-provisioned HQ
// repo as a project.
type ProjectCreator interface {
	Add(ctx context.Context, in projectsvc.AddInput) (projectsvc.Project, error)
}

// EnsureHoldingHQ returns the holding's auto-provisioned HQ project id,
// creating and registering it — a local git repo under the daemon's data
// dir, no folder picker involved — on first call. Idempotent: once created,
// every call returns the same project id.
func (m *Service) EnsureHoldingHQ(ctx context.Context) (string, error) {
	projects, err := m.store.ListProjects(ctx)
	if err != nil {
		return "", apierr.Internal("PROJECTS_LIST_FAILED", "Failed to load projects")
	}
	for _, p := range projects {
		if p.HQRole == domain.HQRoleHolding {
			return p.ID, nil
		}
	}
	if m.projects == nil || m.dataDir == "" {
		return "", apierr.Internal("HQ_PROVISION_UNAVAILABLE", "HQ auto-provisioning is not configured")
	}

	path := filepath.Join(m.dataDir, orgHQRoot, "holding")
	id, err := m.provisionRepo(ctx, path, holdingHQProjectID, "Holding Headquarters")
	if err != nil {
		return "", err
	}
	if err := m.SetHQRole(ctx, domain.ProjectID(id), SetHQRoleInput{Role: string(domain.HQRoleHolding)}); err != nil {
		return "", err
	}
	return id, nil
}

// EnsureCompanyHQ returns companyID's auto-provisioned PM HQ project id,
// creating and registering it — a local git repo under the daemon's data
// dir, no folder picker involved — on first call. Idempotent.
func (m *Service) EnsureCompanyHQ(ctx context.Context, companyID string) (string, error) {
	companyID = strings.TrimSpace(companyID)
	if companyID == "" {
		return "", apierr.Invalid("COMPANY_ID_REQUIRED", "companyId is required", nil)
	}
	projects, err := m.store.ListProjects(ctx)
	if err != nil {
		return "", apierr.Internal("PROJECTS_LIST_FAILED", "Failed to load projects")
	}
	for _, p := range projects {
		if p.HQRole == domain.HQRoleCompany && p.CompanyID == companyID {
			return p.ID, nil
		}
	}
	if m.projects == nil || m.dataDir == "" {
		return "", apierr.Internal("HQ_PROVISION_UNAVAILABLE", "HQ auto-provisioning is not configured")
	}

	path := filepath.Join(m.dataDir, orgHQRoot, "companies", companyID)
	id, err := m.provisionRepo(ctx, path, companyID+"-hq", "Company Headquarters")
	if err != nil {
		return "", err
	}
	if ok, err := m.store.SetProjectCompany(ctx, id, companyID); err != nil {
		return "", apierr.Internal("HQ_COMPANY_ASSIGN_FAILED", "Failed to assign hq project to company")
	} else if !ok {
		return "", apierr.Internal("HQ_COMPANY_ASSIGN_FAILED", "Failed to assign hq project to company")
	}
	if err := m.SetHQRole(ctx, domain.ProjectID(id), SetHQRoleInput{Role: string(domain.HQRoleCompany)}); err != nil {
		return "", err
	}
	return id, nil
}

// provisionRepo creates (idempotently) a local git repository at path with
// one initial commit, then registers it as a docs-repo project under id.
func (m *Service) provisionRepo(ctx context.Context, path, id, title string) (string, error) {
	if err := initLocalGitRepo(path, title); err != nil {
		return "", apierr.Internal("HQ_REPO_INIT_FAILED", "Failed to initialize hq repository: "+err.Error())
	}
	projectID := id
	proj, err := m.projects.Add(ctx, projectsvc.AddInput{
		Path:       path,
		ProjectID:  &projectID,
		AsDocsRepo: true,
	})
	if err != nil {
		return "", err
	}
	return string(proj.ID), nil
}

// initLocalGitRepo creates dir (if needed), git-inits it on branch "main",
// and commits a placeholder README so the repo has a real HEAD — sessions
// spawn worktrees off a branch, which requires at least one commit to exist.
// A no-op when dir is already a git repository.
func initLocalGitRepo(dir, title string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create hq directory: %w", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return nil // already provisioned
	}
	if err := aoprocess.Command("git", "init", "-b", "main", dir).Run(); err != nil {
		return fmt.Errorf("git init: %w", err)
	}
	readme := filepath.Join(dir, "README.md")
	content := "# " + title + "\n\nAuto-provisioned by AO to run the " + strings.ToLower(title) + " orchestrator.\n"
	if err := os.WriteFile(readme, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write README: %w", err)
	}
	if err := aoprocess.Command("git", "-C", dir, "add", "README.md").Run(); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	commit := aoprocess.Command("git", "-C", dir, "commit", "-m", "Initial commit")
	commit.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=AO", "GIT_AUTHOR_EMAIL=ao@localhost",
		"GIT_COMMITTER_NAME=AO", "GIT_COMMITTER_EMAIL=ao@localhost",
	)
	if err := commit.Run(); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	return nil
}
