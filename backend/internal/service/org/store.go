package org

import (
	"context"

	"github.com/modernagent/modern-agent/backend/internal/domain"
)

// Store is the durable persistence surface required by Service.
type Store interface {
	GetProject(ctx context.Context, id string) (domain.ProjectRecord, bool, error)
	ListProjects(ctx context.Context) ([]domain.ProjectRecord, error)
	ListCompanies(ctx context.Context) ([]domain.CompanyRecord, error)
	ListAllSessions(ctx context.Context) ([]domain.SessionRecord, error)
	// SetProjectHQRole sets (or, with role == "", clears) a project's HQ role,
	// reporting whether the project id was known.
	SetProjectHQRole(ctx context.Context, projectID string, role domain.HQRole) (bool, error)
	// SetProjectCompany assigns (companyID != "") or unassigns (companyID == "")
	// a project's company, reporting whether the project id was known. Used by
	// EnsureCompanyHQ to assign a freshly auto-provisioned PM HQ to its company.
	SetProjectCompany(ctx context.Context, projectID string, companyID string) (bool, error)
	GetOrgSetting(ctx context.Context, key string) (string, bool, error)
	SetOrgSetting(ctx context.Context, key, value string) error
}
