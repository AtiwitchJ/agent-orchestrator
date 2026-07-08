package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/storage/sqlite/gen"
)

// SetProjectHQRole sets or clears (role == "") a project's HQ role. Reports
// whether a project row was affected (false means the project id is unknown).
func (s *Store) SetProjectHQRole(ctx context.Context, projectID string, role domain.HQRole) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.SetProjectHQRole(ctx, gen.SetProjectHQRoleParams{
		HqRole: nullString(string(role)),
		ID:     domain.ProjectID(projectID),
	})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetOrgSetting returns the value stored for key, or ok=false when unset.
func (s *Store) GetOrgSetting(ctx context.Context, key string) (string, bool, error) {
	v, err := s.qr.GetOrgSetting(ctx, key)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get org setting %s: %w", key, err)
	}
	return v, true, nil
}

// SetOrgSetting upserts a key/value pair in org_settings.
func (s *Store) SetOrgSetting(ctx context.Context, key, value string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.qw.SetOrgSetting(ctx, gen.SetOrgSettingParams{Key: key, Value: value})
}
