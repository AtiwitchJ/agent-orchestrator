package controllers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apispec"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	messagesvc "github.com/aoagents/agent-orchestrator/backend/internal/service/message"
)

// MessageService is the controller-facing session-message read contract.
type MessageService interface {
	ListProjectMessages(ctx context.Context, filter messagesvc.ListFilter) ([]domain.SessionMessageRecord, error)
}

// MessagesController owns the read-only project-scoped /messages route: the
// durable read side of agent-to-agent `ao send` facts persisted by
// SessionsController's send handler.
type MessagesController struct {
	Svc MessageService
}

// Register mounts the messages route on the supplied router.
func (c *MessagesController) Register(r chi.Router) {
	r.Get("/projects/{id}/messages", c.list)
}

func (c *MessagesController) list(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, "GET", "/api/v1/projects/{id}/messages")
		return
	}
	limit := messagesvc.DefaultListLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_LIMIT", "limit must be a positive integer", nil)
			return
		}
		limit = parsed
	}
	if limit > messagesvc.MaxListLimit {
		limit = messagesvc.MaxListLimit
	}
	rows, err := c.Svc.ListProjectMessages(r.Context(), messagesvc.ListFilter{ProjectID: projectID(r), Limit: limit})
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, ListProjectMessagesResponse{Messages: sessionMessageResponses(rows)})
}

func sessionMessageResponses(rows []domain.SessionMessageRecord) []SessionMessage {
	out := make([]SessionMessage, 0, len(rows))
	for _, r := range rows {
		out = append(out, SessionMessage{
			ID:              r.ID,
			SenderSessionID: string(r.SenderSessionID),
			TargetSessionID: string(r.TargetSessionID),
			Content:         r.Content,
			CreatedAt:       r.CreatedAt,
		})
	}
	return out
}
