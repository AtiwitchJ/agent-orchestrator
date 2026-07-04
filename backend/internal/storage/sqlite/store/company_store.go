package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// InsertCompany inserts a new company registry row.
func (s *Store) InsertCompany(ctx context.Context, r domain.CompanyRecord) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.qw.InsertCompany(ctx, gen.InsertCompanyParams{
		ID:        r.ID,
		Name:      r.Name,
		CreatedAt: r.CreatedAt,
	})
}

// GetCompany returns a company by id.
func (s *Store) GetCompany(ctx context.Context, id string) (domain.CompanyRecord, bool, error) {
	c, err := s.qr.GetCompany(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.CompanyRecord{}, false, nil
	}
	if err != nil {
		return domain.CompanyRecord{}, false, fmt.Errorf("get company %s: %w", id, err)
	}
	return companyRowFromGen(c), true, nil
}

// ListCompanies returns every registered company ordered by name.
func (s *Store) ListCompanies(ctx context.Context) ([]domain.CompanyRecord, error) {
	rows, err := s.qr.ListCompanies(ctx)
	if err != nil {
		return nil, fmt.Errorf("list companies: %w", err)
	}
	out := make([]domain.CompanyRecord, 0, len(rows))
	for _, c := range rows {
		out = append(out, companyRowFromGen(c))
	}
	return out, nil
}

// SetProjectCompany assigns a project to a company, or unassigns it when
// companyID is "". Reports whether a project row was affected (false means
// the project id is unknown).
func (s *Store) SetProjectCompany(ctx context.Context, projectID string, companyID string) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.SetProjectCompany(ctx, gen.SetProjectCompanyParams{
		CompanyID: nullString(companyID),
		ID:        domain.ProjectID(projectID),
	})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func companyRowFromGen(c gen.Company) domain.CompanyRecord {
	return domain.CompanyRecord{
		ID:        c.ID,
		Name:      c.Name,
		CreatedAt: c.CreatedAt,
	}
}

// nullString encodes an empty string as SQL NULL — used for nullable text
// columns (e.g. projects.company_id) where "" durably means "unset" rather
// than a literal empty string in storage.
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// stringFromNull decodes a nullable text column back to a plain string, where
// SQL NULL becomes "".
func stringFromNull(ns sql.NullString) string {
	if !ns.Valid {
		return ""
	}
	return ns.String
}
