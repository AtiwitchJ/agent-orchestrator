package company

import (
	"context"

	"github.com/modernagent/modern-agent/backend/internal/domain"
)

// Store is the durable company persistence surface required by Service.
type Store interface {
	InsertCompany(ctx context.Context, r domain.CompanyRecord) error
	ListCompanies(ctx context.Context) ([]domain.CompanyRecord, error)
	GetCompany(ctx context.Context, id string) (domain.CompanyRecord, bool, error)
	// SetProjectCompany assigns (companyID != "") or unassigns (companyID == "")
	// a project's company, reporting whether the project id was known.
	SetProjectCompany(ctx context.Context, projectID string, companyID string) (bool, error)
}
