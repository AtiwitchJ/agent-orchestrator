package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/storage/sqlite/gen"
)

// CreateWorkCard persists a new work card. Identity and audit fields are
// assigned by the service; this store only maps the domain record to sqlc.
func (s *Store) CreateWorkCard(ctx context.Context, card domain.WorkCard) error {
	params, err := workCardInsertParams(card)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.qw.InsertWorkCard(ctx, params); err != nil {
		return fmt.Errorf("insert work card %s: %w", card.ID, err)
	}
	return nil
}

// GetWorkCard returns a work card by id, or ok=false when it is absent.
func (s *Store) GetWorkCard(ctx context.Context, id string) (domain.WorkCard, bool, error) {
	row, err := s.qr.GetWorkCard(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WorkCard{}, false, nil
	}
	if err != nil {
		return domain.WorkCard{}, false, fmt.Errorf("get work card %s: %w", id, err)
	}
	card, err := workCardFromRow(row)
	if err != nil {
		return domain.WorkCard{}, false, fmt.Errorf("decode work card %s: %w", id, err)
	}
	return card, true, nil
}

// ListWorkCards returns a board's cards in the query's stable board order.
func (s *Store) ListWorkCards(ctx context.Context, projectID, boardID string) ([]domain.WorkCard, error) {
	rows, err := s.qr.ListWorkCardsByProject(ctx, gen.ListWorkCardsByProjectParams{
		ProjectID: projectID,
		BoardID:   boardID,
	})
	if err != nil {
		return nil, fmt.Errorf("list work cards for project %s board %s: %w", projectID, boardID, err)
	}
	cards := make([]domain.WorkCard, 0, len(rows))
	for _, row := range rows {
		card, err := workCardFromRow(row)
		if err != nil {
			return nil, fmt.Errorf("decode work card %s: %w", row.ID, err)
		}
		cards = append(cards, card)
	}
	return cards, nil
}

// UpdateWorkCard writes the mutable state of an existing card. Its identity,
// project, board, and creation time remain untouched by the SQL query.
func (s *Store) UpdateWorkCard(ctx context.Context, card domain.WorkCard) error {
	params, err := workCardUpdateParams(card)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.qw.UpdateWorkCard(ctx, params); err != nil {
		return fmt.Errorf("update work card %s: %w", card.ID, err)
	}
	return nil
}

// ClaimReadyWorkCard atomically transitions one ready card to running when it
// is still eligible and the project's durable running-card count is below the
// supplied WIP limit. The returned flag reports whether this dispatcher won
// the claim; callers must not start a worker when it is false.
func (s *Store) ClaimReadyWorkCard(ctx context.Context, cardID, projectID string, wipLimit int, at time.Time) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	n, err := s.qw.ClaimReadyWorkCard(ctx, gen.ClaimReadyWorkCardParams{
		UpdatedAt: at.UnixMilli(),
		CardID:    cardID,
		ProjectID: projectID,
		WipLimit:  int64(wipLimit),
	})
	if err != nil {
		return false, fmt.Errorf("claim ready work card %s: %w", cardID, err)
	}
	return n > 0, nil
}

// AppendWorkCardEvent records an immutable work-card audit event.
func (s *Store) AppendWorkCardEvent(ctx context.Context, event domain.WorkCardEvent) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.qw.InsertWorkCardEvent(ctx, gen.InsertWorkCardEventParams{
		ID:        event.ID,
		CardID:    event.CardID,
		ProjectID: event.ProjectID,
		Kind:      event.Kind,
		Payload:   event.Payload,
		CreatedAt: event.CreatedAt.UnixMilli(),
	}); err != nil {
		return fmt.Errorf("insert work card event %s: %w", event.ID, err)
	}
	return nil
}

// PrepareHermesAnswerAttempt records a Hermes send attempt and, when requested,
// consumes the non-sticky autonomous override in the same transaction. A
// one-shot override is therefore never durably spent without an attempt that
// owns the decision in its payload.
func (s *Store) PrepareHermesAnswerAttempt(ctx context.Context, project domain.ProjectRecord, event domain.WorkCardEvent, consumeOneShot bool) error {
	config, err := marshalProjectConfig(project.Config)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.inTx(ctx, "prepare Hermes answer attempt", func(q *gen.Queries) error {
		if consumeOneShot {
			if err := upsertProject(ctx, q, project, config); err != nil {
				return err
			}
		}
		return q.InsertWorkCardEvent(ctx, gen.InsertWorkCardEventParams{
			ID:        event.ID,
			CardID:    event.CardID,
			ProjectID: event.ProjectID,
			Kind:      event.Kind,
			Payload:   event.Payload,
			CreatedAt: event.CreatedAt.UnixMilli(),
		})
	})
}

// ListWorkCardEvents returns a card's immutable audit facts in creation order.
func (s *Store) ListWorkCardEvents(ctx context.Context, cardID string) ([]domain.WorkCardEvent, error) {
	rows, err := s.qr.ListWorkCardEventsByCard(ctx, cardID)
	if err != nil {
		return nil, fmt.Errorf("list work card events for %s: %w", cardID, err)
	}
	events := make([]domain.WorkCardEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, domain.WorkCardEvent{
			ID:        row.ID,
			CardID:    row.CardID,
			ProjectID: row.ProjectID,
			Kind:      row.Kind,
			Payload:   row.Payload,
			CreatedAt: time.UnixMilli(row.CreatedAt).UTC(),
		})
	}
	return events, nil
}

func workCardFromRow(row gen.WorkCard) (domain.WorkCard, error) {
	var labels []string
	if err := json.Unmarshal([]byte(row.LabelsJson), &labels); err != nil {
		return domain.WorkCard{}, fmt.Errorf("unmarshal labels: %w", err)
	}
	return domain.WorkCard{
		ID:                 row.ID,
		ProjectID:          row.ProjectID,
		BoardID:            row.BoardID,
		Title:              row.Title,
		Notes:              row.Notes,
		Priority:           domain.CardPriority(row.Priority),
		Labels:             labels,
		Status:             domain.CardStatus(row.Status),
		ScheduledAt:        timeFromMillis(row.ScheduledAt),
		ReadyAt:            timeFromMillis(row.ReadyAt),
		Position:           row.Position,
		TargetPath:         row.TargetPath,
		RepoName:           row.RepoName,
		Agent:              row.Agent,
		SessionID:          row.SessionID,
		WaitingForInput:    row.WaitingForInput != 0,
		PausedRetarget:     row.PausedRetarget != 0,
		GoalVersion:        int(row.GoalVersion),
		SupersededByCardID: row.SupersededByCardID,
		CreatedAt:          time.UnixMilli(row.CreatedAt).UTC(),
		UpdatedAt:          time.UnixMilli(row.UpdatedAt).UTC(),
	}, nil
}

func workCardInsertParams(card domain.WorkCard) (gen.InsertWorkCardParams, error) {
	labels, err := json.Marshal(card.Labels)
	if err != nil {
		return gen.InsertWorkCardParams{}, fmt.Errorf("marshal work card labels: %w", err)
	}
	return gen.InsertWorkCardParams{
		ID:                 card.ID,
		ProjectID:          card.ProjectID,
		BoardID:            card.BoardID,
		Title:              card.Title,
		Notes:              card.Notes,
		Priority:           string(card.Priority),
		LabelsJson:         string(labels),
		Status:             string(card.Status),
		ScheduledAt:        millisFromTime(card.ScheduledAt),
		ReadyAt:            millisFromTime(card.ReadyAt),
		Position:           card.Position,
		TargetPath:         card.TargetPath,
		RepoName:           card.RepoName,
		Agent:              card.Agent,
		SessionID:          card.SessionID,
		WaitingForInput:    boolToInt64(card.WaitingForInput),
		PausedRetarget:     boolToInt64(card.PausedRetarget),
		GoalVersion:        int64(card.GoalVersion),
		SupersededByCardID: card.SupersededByCardID,
		CreatedAt:          card.CreatedAt.UnixMilli(),
		UpdatedAt:          card.UpdatedAt.UnixMilli(),
	}, nil
}

func workCardUpdateParams(card domain.WorkCard) (gen.UpdateWorkCardParams, error) {
	labels, err := json.Marshal(card.Labels)
	if err != nil {
		return gen.UpdateWorkCardParams{}, fmt.Errorf("marshal work card labels: %w", err)
	}
	return gen.UpdateWorkCardParams{
		ID:                 card.ID,
		Title:              card.Title,
		Notes:              card.Notes,
		Priority:           string(card.Priority),
		LabelsJson:         string(labels),
		Status:             string(card.Status),
		ScheduledAt:        millisFromTime(card.ScheduledAt),
		ReadyAt:            millisFromTime(card.ReadyAt),
		Position:           card.Position,
		TargetPath:         card.TargetPath,
		RepoName:           card.RepoName,
		Agent:              card.Agent,
		SessionID:          card.SessionID,
		WaitingForInput:    boolToInt64(card.WaitingForInput),
		PausedRetarget:     boolToInt64(card.PausedRetarget),
		GoalVersion:        int64(card.GoalVersion),
		SupersededByCardID: card.SupersededByCardID,
		UpdatedAt:          card.UpdatedAt.UnixMilli(),
	}, nil
}

func millisFromTime(t *time.Time) sql.NullInt64 {
	if t == nil || t.IsZero() {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: t.UnixMilli(), Valid: true}
}

func timeFromMillis(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	t := time.UnixMilli(value.Int64).UTC()
	return &t
}

func boolToInt64(value bool) int64 {
	if value {
		return 1
	}
	return 0
}
