// Package company implements the Company grouping use-cases for controllers:
// a company groups multiple git-repo Projects (e.g. an org with several
// product repos). Layout mirrors service/project: service.go (Manager +
// implementation), types.go (API-facing structs), store.go (the narrow Store
// interface this service needs).
package company

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/httpd/apierr"
)

// Manager is the controller-facing contract for the /api/v1/companies surface
// plus the project-company assignment endpoint.
type Manager interface {
	// List returns every registered company, ordered by name.
	List(ctx context.Context) ([]Company, error)

	// Create registers a new company. The id is derived from Name (slugified,
	// deduplicated with a numeric suffix on collision).
	Create(ctx context.Context, in CreateInput) (Company, error)

	// AssignProject sets (or, when CompanyID is "", clears) a project's company.
	// A non-empty CompanyID must name an existing company.
	AssignProject(ctx context.Context, projectID domain.ProjectID, in AssignProjectInput) error
}

// Service implements the company use-cases for controllers.
type Service struct {
	store Store
	clock func() time.Time
}

var _ Manager = (*Service)(nil)

// Deps captures optional collaborators for company use-cases.
type Deps struct {
	Store Store
	Clock func() time.Time
}

// New returns a company service backed by the given durable store.
func New(store Store) *Service {
	return NewWithDeps(Deps{Store: store})
}

// NewWithDeps returns a company service with optional collaborators.
func NewWithDeps(d Deps) *Service {
	s := &Service{store: d.Store, clock: d.Clock}
	if s.clock == nil {
		s.clock = time.Now
	}
	return s
}

// List returns every registered company.
func (m *Service) List(ctx context.Context) ([]Company, error) {
	rows, err := m.store.ListCompanies(ctx)
	if err != nil {
		return nil, apierr.Internal("COMPANIES_LIST_FAILED", "Failed to load companies")
	}
	out := make([]Company, 0, len(rows))
	for _, r := range rows {
		out = append(out, companyFromRow(r))
	}
	return out, nil
}

// Create registers a new company. Name is required; the id is a slugified
// derivation of Name, suffixed with -2, -3, ... on collision so repeated
// company names (e.g. two teams both named "Acme") never clash.
func (m *Service) Create(ctx context.Context, in CreateInput) (Company, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return Company{}, apierr.Invalid("NAME_REQUIRED", "Company name is required", nil)
	}

	id, err := m.uniqueID(ctx, slugify(name))
	if err != nil {
		return Company{}, err
	}

	rec := domain.CompanyRecord{ID: id, Name: name, CreatedAt: m.clock()}
	if err := m.store.InsertCompany(ctx, rec); err != nil {
		return Company{}, apierr.Internal("COMPANY_CREATE_FAILED", "Failed to create company")
	}
	return companyFromRow(rec), nil
}

// AssignProject sets or clears a project's company assignment. The company
// must exist when assigning (CompanyID != ""); an empty CompanyID unassigns
// without needing a lookup.
func (m *Service) AssignProject(ctx context.Context, projectID domain.ProjectID, in AssignProjectInput) error {
	companyID := strings.TrimSpace(in.CompanyID)
	if companyID != "" {
		if _, ok, err := m.store.GetCompany(ctx, companyID); err != nil {
			return apierr.Internal("COMPANY_LOAD_FAILED", "Failed to load company")
		} else if !ok {
			return apierr.NotFound("COMPANY_NOT_FOUND", "Unknown company")
		}
	}
	ok, err := m.store.SetProjectCompany(ctx, string(projectID), companyID)
	if err != nil {
		return apierr.Internal("PROJECT_COMPANY_ASSIGN_FAILED", "Failed to update project company")
	}
	if !ok {
		return apierr.NotFound("PROJECT_NOT_FOUND", "Unknown project")
	}
	return nil
}

// uniqueID returns base, or base-2, base-3, ... — the first id not already
// registered.
func (m *Service) uniqueID(ctx context.Context, base string) (string, error) {
	id := base
	for i := 2; ; i++ {
		_, ok, err := m.store.GetCompany(ctx, id)
		if err != nil {
			return "", apierr.Internal("COMPANY_LOAD_FAILED", "Failed to load company")
		}
		if !ok {
			return id, nil
		}
		id = fmt.Sprintf("%s-%d", base, i)
	}
}

func companyFromRow(r domain.CompanyRecord) Company {
	return Company{ID: r.ID, Name: r.Name, CreatedAt: r.CreatedAt}
}

// slugify derives a storage-safe id from a display name: lowercase
// alphanumerics separated by single hyphens (mirrors the project id shape —
// see service/project's validateProjectID pattern). An all-punctuation name
// falls back to "company" so Create never produces an empty id.
func slugify(name string) string {
	var b strings.Builder
	prevDash := true // suppresses a leading hyphen
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	slug := strings.TrimSuffix(b.String(), "-")
	if slug == "" {
		return "company"
	}
	return slug
}
