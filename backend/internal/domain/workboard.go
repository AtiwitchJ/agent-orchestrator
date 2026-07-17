package domain

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type CardStatus string

const (
	CardStatusTriage    CardStatus = "triage"
	CardStatusBacklog   CardStatus = "backlog"
	CardStatusTodo      CardStatus = "todo"
	CardStatusScheduled CardStatus = "scheduled"
	CardStatusReady     CardStatus = "ready"
	CardStatusRunning   CardStatus = "running"
	CardStatusReview    CardStatus = "review"
	CardStatusBlocked   CardStatus = "blocked"
	CardStatusDone      CardStatus = "done"
)

func ParseCardStatus(s string) (CardStatus, error) {
	switch CardStatus(s) {
	case CardStatusTriage, CardStatusBacklog, CardStatusTodo, CardStatusScheduled,
		CardStatusReady, CardStatusRunning, CardStatusReview, CardStatusBlocked, CardStatusDone:
		return CardStatus(s), nil
	default:
		return "", fmt.Errorf("invalid card status %q", s)
	}
}

// ValidateCardStatus reports whether s is a known card status.
func ValidateCardStatus(s string) error {
	_, err := ParseCardStatus(s)
	return err
}

type CardPriority string

const (
	CardPriorityLow    CardPriority = "low"
	CardPriorityNormal CardPriority = "normal"
	CardPriorityHigh   CardPriority = "high"
	CardPriorityUrgent CardPriority = "urgent"
)

func ParseCardPriority(s string) (CardPriority, error) {
	switch CardPriority(s) {
	case CardPriorityLow, CardPriorityNormal, CardPriorityHigh, CardPriorityUrgent:
		return CardPriority(s), nil
	default:
		return "", fmt.Errorf("invalid card priority %q", s)
	}
}

// PriorityRank higher = claimed sooner.
func (p CardPriority) Rank() int {
	switch p {
	case CardPriorityUrgent:
		return 4
	case CardPriorityHigh:
		return 3
	case CardPriorityNormal:
		return 2
	case CardPriorityLow:
		return 1
	default:
		return 0
	}
}

type WorkCard struct {
	ID                 string
	ProjectID          string
	BoardID            string
	Title              string
	Notes              string
	Priority           CardPriority
	Labels             []string
	Status             CardStatus
	ScheduledAt        *time.Time
	ReadyAt            *time.Time
	Position           int64
	TargetPath         string
	RepoName           string
	Agent              string
	SessionID          string
	WaitingForInput    bool
	PausedRetarget     bool
	GoalVersion        int
	SupersededByCardID string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// WorkCardEvent is an append-only audit fact associated with a work card.
// Payload is JSON owned by the event producer.
type WorkCardEvent struct {
	ID        string
	CardID    string
	ProjectID string
	Kind      string
	Payload   string
	CreatedAt time.Time
}

type WorkboardAutonomousConfig struct {
	Enabled             bool   `json:"enabled,omitempty"`
	Mode                string `json:"mode,omitempty"` // skip_timeout | short_timeout
	ShortTimeoutMinutes int    `json:"shortTimeoutMinutes,omitempty"`
	Sticky              bool   `json:"sticky,omitempty"`
}

type WorkboardConfig struct {
	WIPLimit             int                       `json:"wipLimit,omitempty"`
	FallbackAgents       []string                  `json:"fallbackAgents,omitempty"`
	LimitCooldownMinutes int                       `json:"limitCooldownMinutes,omitempty"`
	AnswerTimeoutMinutes int                       `json:"answerTimeoutMinutes,omitempty"`
	Autonomous           WorkboardAutonomousConfig `json:"autonomous,omitempty"`
	AnswerDenylist       []string                  `json:"answerDenylist,omitempty"`
}

func DefaultWorkboardConfig() WorkboardConfig {
	return WorkboardConfig{
		WIPLimit:             3,
		LimitCooldownMinutes: 60,
		AnswerTimeoutMinutes: 10,
		Autonomous: WorkboardAutonomousConfig{
			Mode:                "skip_timeout",
			ShortTimeoutMinutes: 2,
			Sticky:              true,
		},
		AnswerDenylist: []string{"force_push", "delete_repo", "exfil_secret"},
	}
}

// TargetPathAllowed reports whether absPath is under one of the registered repo roots.
func TargetPathAllowed(absPath string, repoRoots []string) bool {
	clean := filepath.Clean(absPath)
	for _, root := range repoRoots {
		r := filepath.Clean(root)
		if clean == r || strings.HasPrefix(clean, r+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// ValidateTargetPathUnderRepos reports an error when absPath is outside all registered repo roots.
func ValidateTargetPathUnderRepos(absPath string, repoRoots []string) error {
	if TargetPathAllowed(absPath, repoRoots) {
		return nil
	}
	return fmt.Errorf("target path %q is not under a registered repository", absPath)
}
