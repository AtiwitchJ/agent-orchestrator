package controllers

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/httpd/apierr"
	"github.com/modernagent/modern-agent/backend/internal/httpd/apispec"
	"github.com/modernagent/modern-agent/backend/internal/httpd/envelope"
	workboardsvc "github.com/modernagent/modern-agent/backend/internal/service/workboard"
)

// WorkboardService is the controller-facing work-card CRUD boundary.
type WorkboardService interface {
	Create(ctx context.Context, in workboardsvc.CreateInput) (domain.WorkCard, error)
	List(ctx context.Context, projectID, boardID string) ([]domain.WorkCard, error)
	Get(ctx context.Context, id string) (domain.WorkCard, error)
	Update(ctx context.Context, id string, in workboardsvc.UpdateInput) (domain.WorkCard, error)
	Move(ctx context.Context, id string, status domain.CardStatus, position int64) (domain.WorkCard, error)
}

// WorkboardController owns the project-scoped work-card routes.
type WorkboardController struct {
	Svc WorkboardService
}

// Register mounts the workboard routes on the supplied router.
func (c *WorkboardController) Register(r chi.Router) {
	r.Get("/projects/{projectId}/workboard/cards", c.list)
	r.Post("/projects/{projectId}/workboard/cards", c.create)
	r.Get("/workboard/cards/{cardId}", c.get)
	r.Patch("/workboard/cards/{cardId}", c.update)
	r.Post("/workboard/cards/{cardId}/move", c.move)
}

func (c *WorkboardController) list(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, http.MethodGet, "/api/v1/projects/{projectId}/workboard/cards")
		return
	}
	cards, err := c.Svc.List(r.Context(), chi.URLParam(r, "projectId"), "")
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, ListWorkCardsResponse{Cards: workCardResponses(cards)})
}

func (c *WorkboardController) create(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, http.MethodPost, "/api/v1/projects/{projectId}/workboard/cards")
		return
	}
	var req CreateWorkCardRequest
	if err := decodeJSONStrict(r, &req); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Invalid JSON body", nil)
		return
	}
	card, err := c.Svc.Create(r.Context(), req.toInput(chi.URLParam(r, "projectId")))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusCreated, newWorkCardResponse(card))
}

func (c *WorkboardController) get(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, http.MethodGet, "/api/v1/workboard/cards/{cardId}")
		return
	}
	card, err := c.Svc.Get(r.Context(), chi.URLParam(r, "cardId"))
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, newWorkCardResponse(card))
}

func (c *WorkboardController) update(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, http.MethodPatch, "/api/v1/workboard/cards/{cardId}")
		return
	}
	var req UpdateWorkCardRequest
	if err := decodeJSONStrict(r, &req); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Invalid JSON body", nil)
		return
	}
	card, err := c.Svc.Update(r.Context(), chi.URLParam(r, "cardId"), req.toInput())
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, newWorkCardResponse(card))
}

func (c *WorkboardController) move(w http.ResponseWriter, r *http.Request) {
	if c.Svc == nil {
		apispec.NotImplemented(w, r, http.MethodPost, "/api/v1/workboard/cards/{cardId}/move")
		return
	}
	var req MoveWorkCardRequest
	if err := decodeJSONStrict(r, &req); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Invalid JSON body", nil)
		return
	}
	if !req.positionSet {
		envelope.WriteError(w, r, apierr.Invalid("WORK_CARD_POSITION_REQUIRED", "Position is required", nil))
		return
	}
	card, err := c.Svc.Move(r.Context(), chi.URLParam(r, "cardId"), domain.CardStatus(req.Status), req.Position)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, newWorkCardResponse(card))
}
