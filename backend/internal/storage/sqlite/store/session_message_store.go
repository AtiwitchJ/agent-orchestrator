package store

import (
	"context"
	"fmt"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// InsertSessionMessage persists one durable agent-to-agent (or human-to-agent)
// send as a fact. The insert fires the session_messages_cdc_insert trigger
// (migration 0023), which fans a session_message_created event out to
// change_log for the target session's project.
func (s *Store) InsertSessionMessage(ctx context.Context, r domain.SessionMessageRecord) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var sender *domain.SessionID
	if r.SenderSessionID != "" {
		sender = &r.SenderSessionID
	}
	if err := s.qw.InsertSessionMessage(ctx, gen.InsertSessionMessageParams{
		ID:              r.ID,
		SenderSessionID: sender,
		TargetSessionID: r.TargetSessionID,
		Content:         r.Content,
		CreatedAt:       r.CreatedAt,
	}); err != nil {
		return fmt.Errorf("insert session message %s: %w", r.ID, err)
	}
	return nil
}

// ListProjectSessionMessages returns the most recent session messages whose
// target session belongs to project, newest first, capped at limit.
func (s *Store) ListProjectSessionMessages(ctx context.Context, project domain.ProjectID, limit int) ([]domain.SessionMessageRecord, error) {
	rows, err := s.qr.ListProjectSessionMessages(ctx, gen.ListProjectSessionMessagesParams{
		ProjectID: project,
		Limit:     int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list session messages for project %s: %w", project, err)
	}
	out := make([]domain.SessionMessageRecord, 0, len(rows))
	for _, r := range rows {
		out = append(out, sessionMessageFromGen(r))
	}
	return out, nil
}

func sessionMessageFromGen(r gen.SessionMessage) domain.SessionMessageRecord {
	rec := domain.SessionMessageRecord{
		ID:              r.ID,
		TargetSessionID: r.TargetSessionID,
		Content:         r.Content,
		CreatedAt:       r.CreatedAt,
	}
	if r.SenderSessionID != nil {
		rec.SenderSessionID = *r.SenderSessionID
	}
	return rec
}
