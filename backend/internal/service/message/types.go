// Package message exposes the read-only project-scoped session-message
// listing use case for REST controllers: it is the read side of the durable
// session_messages fact persisted by service/session's Send.
package message

import "github.com/modernagent/modern-agent/backend/internal/domain"

// ListFilter controls project-scoped message listing.
type ListFilter struct {
	ProjectID domain.ProjectID
	Limit     int
}
