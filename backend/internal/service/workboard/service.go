// Package workboard implements durable work-card CRUD for the project board.
package workboard

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/httpd/apierr"
)

const defaultBoardID = "default"

// Store is the narrow durable surface required by Service.
type Store interface {
	CreateWorkCard(ctx context.Context, card domain.WorkCard) error
	GetWorkCard(ctx context.Context, id string) (domain.WorkCard, bool, error)
	ListWorkCards(ctx context.Context, projectID, boardID string) ([]domain.WorkCard, error)
	UpdateWorkCard(ctx context.Context, card domain.WorkCard) error
	GetProject(ctx context.Context, id string) (domain.ProjectRecord, bool, error)
	ListWorkspaceRepos(ctx context.Context, projectID string) ([]domain.WorkspaceRepoRecord, error)
}

// CreateInput is the required content and placement of a new work card.
type CreateInput struct {
	ProjectID   string
	BoardID     string
	Title       string
	Notes       string
	Priority    domain.CardPriority
	Labels      []string
	Status      domain.CardStatus
	TargetPath  string
	Agent       string
	ScheduledAt *time.Time
}

// UpdateInput is a partial update. Nil fields are left unchanged.
type UpdateInput struct {
	Title       *string
	Notes       *string
	Priority    *domain.CardPriority
	Labels      *[]string
	Status      *domain.CardStatus
	ScheduledAt *time.Time
	TargetPath  *string
	Agent       *string
	Position    *int64
}

// Service owns work-card validation and orchestration-free CRUD.
type Service struct {
	store Store
	clock func() time.Time
	newID func() string
}

// Deps configures optional collaborators for Service.
type Deps struct {
	Store Store
	Clock func() time.Time
	NewID func() string
}

// New creates a workboard service backed by store.
func New(store Store) *Service {
	return NewWithDeps(Deps{Store: store})
}

// NewWithDeps creates a workboard service with testable time and id sources.
func NewWithDeps(d Deps) *Service {
	s := &Service{store: d.Store, clock: d.Clock, newID: d.NewID}
	if s.clock == nil {
		s.clock = time.Now
	}
	if s.newID == nil {
		s.newID = func() string { return "card_" + uuid.NewString() }
	}
	return s
}

// Create validates and persists a new work card.
func (s *Service) Create(ctx context.Context, in CreateInput) (domain.WorkCard, error) {
	projectID := strings.TrimSpace(in.ProjectID)
	if projectID == "" {
		return domain.WorkCard{}, apierr.Invalid("WORK_CARD_PROJECT_REQUIRED", "Project is required", nil)
	}
	title, notes, agent := strings.TrimSpace(in.Title), strings.TrimSpace(in.Notes), strings.TrimSpace(in.Agent)
	if title == "" {
		return domain.WorkCard{}, apierr.Invalid("WORK_CARD_TITLE_REQUIRED", "Title is required", nil)
	}
	if notes == "" {
		return domain.WorkCard{}, apierr.Invalid("WORK_CARD_NOTES_REQUIRED", "Notes are required", nil)
	}
	if _, err := domain.ParseCardPriority(string(in.Priority)); err != nil {
		return domain.WorkCard{}, apierr.Invalid("WORK_CARD_PRIORITY_INVALID", err.Error(), nil)
	}
	if len(in.Labels) == 0 {
		return domain.WorkCard{}, apierr.Invalid("WORK_CARD_LABELS_REQUIRED", "At least one label is required", nil)
	}
	if agent == "" {
		return domain.WorkCard{}, apierr.Invalid("WORK_CARD_AGENT_REQUIRED", "Agent is required", nil)
	}
	status := in.Status
	if status == "" {
		status = domain.CardStatusTriage
	}
	if err := domain.ValidateCardStatus(string(status)); err != nil {
		return domain.WorkCard{}, apierr.Invalid("WORK_CARD_STATUS_INVALID", err.Error(), nil)
	}
	boardID := strings.TrimSpace(in.BoardID)
	if boardID == "" {
		boardID = defaultBoardID
	}
	targetPath, err := s.validateTargetPath(ctx, projectID, in.TargetPath)
	if err != nil {
		return domain.WorkCard{}, err
	}

	now := s.clock().UTC()
	card := domain.WorkCard{
		ID:          s.newID(),
		ProjectID:   projectID,
		BoardID:     boardID,
		Title:       title,
		Notes:       notes,
		Priority:    in.Priority,
		Labels:      append([]string(nil), in.Labels...),
		Status:      status,
		ScheduledAt: cloneTime(in.ScheduledAt),
		TargetPath:  targetPath,
		Agent:       agent,
		GoalVersion: 1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if status == domain.CardStatusReady {
		card.ReadyAt = &now
	}
	if err := s.store.CreateWorkCard(ctx, card); err != nil {
		return domain.WorkCard{}, apierr.Internal("WORK_CARD_CREATE_FAILED", "Failed to create work card")
	}
	return card, nil
}

// List returns a project's cards on one board. The default board is selected
// when boardID is empty.
func (s *Service) List(ctx context.Context, projectID, boardID string) ([]domain.WorkCard, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, apierr.Invalid("WORK_CARD_PROJECT_REQUIRED", "Project is required", nil)
	}
	if strings.TrimSpace(boardID) == "" {
		boardID = defaultBoardID
	}
	cards, err := s.store.ListWorkCards(ctx, projectID, boardID)
	if err != nil {
		return nil, apierr.Internal("WORK_CARDS_LIST_FAILED", "Failed to load work cards")
	}
	return cards, nil
}

// Get returns a work card by id.
func (s *Service) Get(ctx context.Context, id string) (domain.WorkCard, error) {
	card, ok, err := s.store.GetWorkCard(ctx, id)
	if err != nil {
		return domain.WorkCard{}, apierr.Internal("WORK_CARD_LOAD_FAILED", "Failed to load work card")
	}
	if !ok {
		return domain.WorkCard{}, apierr.NotFound("WORK_CARD_NOT_FOUND", "Unknown work card")
	}
	return card, nil
}

// Move updates a card's board position and status. Ready cards record the
// moment they entered the ready queue.
func (s *Service) Move(ctx context.Context, id string, status domain.CardStatus, position int64) (domain.WorkCard, error) {
	if err := domain.ValidateCardStatus(string(status)); err != nil {
		return domain.WorkCard{}, apierr.Invalid("WORK_CARD_STATUS_INVALID", err.Error(), nil)
	}
	card, err := s.Get(ctx, id)
	if err != nil {
		return domain.WorkCard{}, err
	}
	card.Status = status
	card.Position = position
	card.UpdatedAt = s.clock().UTC()
	if status == domain.CardStatusReady {
		readyAt := card.UpdatedAt
		card.ReadyAt = &readyAt
	}
	if err := s.store.UpdateWorkCard(ctx, card); err != nil {
		return domain.WorkCard{}, apierr.Internal("WORK_CARD_UPDATE_FAILED", "Failed to update work card")
	}
	return card, nil
}

// Update applies only the supplied mutable fields to a work card.
func (s *Service) Update(ctx context.Context, id string, in UpdateInput) (domain.WorkCard, error) {
	card, err := s.Get(ctx, id)
	if err != nil {
		return domain.WorkCard{}, err
	}
	if in.Title != nil {
		card.Title = strings.TrimSpace(*in.Title)
		if card.Title == "" {
			return domain.WorkCard{}, apierr.Invalid("WORK_CARD_TITLE_REQUIRED", "Title is required", nil)
		}
	}
	if in.Notes != nil {
		card.Notes = strings.TrimSpace(*in.Notes)
		if card.Notes == "" {
			return domain.WorkCard{}, apierr.Invalid("WORK_CARD_NOTES_REQUIRED", "Notes are required", nil)
		}
	}
	if in.Priority != nil {
		if _, err := domain.ParseCardPriority(string(*in.Priority)); err != nil {
			return domain.WorkCard{}, apierr.Invalid("WORK_CARD_PRIORITY_INVALID", err.Error(), nil)
		}
		card.Priority = *in.Priority
	}
	if in.Labels != nil {
		if len(*in.Labels) == 0 {
			return domain.WorkCard{}, apierr.Invalid("WORK_CARD_LABELS_REQUIRED", "At least one label is required", nil)
		}
		card.Labels = append([]string(nil), (*in.Labels)...)
	}
	if in.Status != nil {
		if err := domain.ValidateCardStatus(string(*in.Status)); err != nil {
			return domain.WorkCard{}, apierr.Invalid("WORK_CARD_STATUS_INVALID", err.Error(), nil)
		}
		card.Status = *in.Status
		if card.Status == domain.CardStatusReady {
			readyAt := s.clock().UTC()
			card.ReadyAt = &readyAt
		}
	}
	if in.ScheduledAt != nil {
		card.ScheduledAt = cloneTime(in.ScheduledAt)
	}
	if in.TargetPath != nil {
		targetPath, err := s.validateTargetPath(ctx, card.ProjectID, *in.TargetPath)
		if err != nil {
			return domain.WorkCard{}, err
		}
		card.TargetPath = targetPath
	}
	if in.Agent != nil {
		card.Agent = strings.TrimSpace(*in.Agent)
		if card.Agent == "" {
			return domain.WorkCard{}, apierr.Invalid("WORK_CARD_AGENT_REQUIRED", "Agent is required", nil)
		}
	}
	if in.Position != nil {
		card.Position = *in.Position
	}
	card.UpdatedAt = s.clock().UTC()
	if err := s.store.UpdateWorkCard(ctx, card); err != nil {
		return domain.WorkCard{}, apierr.Internal("WORK_CARD_UPDATE_FAILED", "Failed to update work card")
	}
	return card, nil
}

func (s *Service) validateTargetPath(ctx context.Context, projectID, targetPath string) (string, error) {
	path := filepath.Clean(strings.TrimSpace(targetPath))
	if targetPath == "" || !filepath.IsAbs(path) {
		return "", apierr.Invalid("WORK_CARD_TARGET_PATH_INVALID", "Target path must be an absolute path", nil)
	}
	roots, err := s.repoRoots(ctx, projectID)
	if err != nil {
		return "", err
	}
	if err := domain.ValidateTargetPathUnderRepos(path, roots); err != nil {
		return "", apierr.Invalid("WORK_CARD_TARGET_PATH_INVALID", err.Error(), nil)
	}
	return path, nil
}

func (s *Service) repoRoots(ctx context.Context, projectID string) ([]string, error) {
	project, ok, err := s.store.GetProject(ctx, projectID)
	if err != nil {
		return nil, apierr.Internal("PROJECT_LOAD_FAILED", "Failed to load project")
	}
	if !ok || !project.ArchivedAt.IsZero() {
		return nil, apierr.NotFound("PROJECT_NOT_FOUND", "Unknown project")
	}
	roots := []string{project.Path}
	if project.Kind.WithDefault() != domain.ProjectKindWorkspace {
		return roots, nil
	}
	repos, err := s.store.ListWorkspaceRepos(ctx, projectID)
	if err != nil {
		return nil, apierr.Internal("PROJECT_LOAD_FAILED", "Failed to load workspace repositories")
	}
	for _, repo := range repos {
		roots = append(roots, filepath.Join(project.Path, repo.RelativePath))
	}
	return roots, nil
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := value.UTC()
	return &cloned
}
