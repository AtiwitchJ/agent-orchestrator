package message

import (
	"context"

	"github.com/modernagent/modern-agent/backend/internal/domain"
)

// Store is the message service's read persistence surface.
type Store interface {
	ListProjectSessionMessages(ctx context.Context, project domain.ProjectID, limit int) ([]domain.SessionMessageRecord, error)
}
