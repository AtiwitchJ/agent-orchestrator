package message

import (
	"context"
	"errors"

	"github.com/modernagent/modern-agent/backend/internal/domain"
)

const (
	// DefaultListLimit is the project message page size used when none is requested.
	DefaultListLimit = 100
	// MaxListLimit caps project message API responses.
	MaxListLimit = 500
)

// Manager reads stored session messages for REST controllers.
type Manager struct {
	store Store
}

// Deps configures a Manager.
type Deps struct {
	Store Store
}

// New constructs a read-only message Manager.
func New(d Deps) *Manager {
	return &Manager{store: d.Store}
}

// ListProjectMessages returns the most recent session messages targeting a
// session in project, newest first.
func (m *Manager) ListProjectMessages(ctx context.Context, filter ListFilter) ([]domain.SessionMessageRecord, error) {
	if m == nil || m.store == nil {
		return nil, errors.New("message: store is required")
	}
	return m.store.ListProjectSessionMessages(ctx, filter.ProjectID, normalizeLimit(filter.Limit))
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return DefaultListLimit
	}
	if limit > MaxListLimit {
		return MaxListLimit
	}
	return limit
}
