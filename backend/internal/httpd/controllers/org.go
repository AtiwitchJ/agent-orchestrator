package controllers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/modernagent/modern-agent/backend/internal/httpd/apispec"
	"github.com/modernagent/modern-agent/backend/internal/httpd/envelope"
	orgsvc "github.com/modernagent/modern-agent/backend/internal/service/org"
)

// OrgController owns the /org routes and the project-hq-role assignment
// route. The controller depends only on orgsvc.Manager; nil keeps routes
// registered but returns OpenAPI-backed 501s.
type OrgController struct {
	Mgr orgsvc.Manager
}

// Register mounts the org routes on the supplied router.
func (c *OrgController) Register(r chi.Router) {
	r.Get("/org/overview", c.overview)
	r.Get("/org/heartbeat", c.getHeartbeat)
	r.Put("/org/heartbeat", c.setHeartbeat)
	r.Put("/projects/{id}/hq", c.setHQRole)
	r.Post("/org/holding-hq", c.ensureHoldingHQ)
	r.Post("/org/companies/{companyId}/hq", c.ensureCompanyHQ)
}

func (c *OrgController) overview(w http.ResponseWriter, r *http.Request) {
	if c.Mgr == nil {
		apispec.NotImplemented(w, r, "GET", "/api/v1/org/overview")
		return
	}
	ov, err := c.Mgr.Overview(r.Context())
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, OrgOverviewResponse{Overview: ov})
}

func (c *OrgController) getHeartbeat(w http.ResponseWriter, r *http.Request) {
	if c.Mgr == nil {
		apispec.NotImplemented(w, r, "GET", "/api/v1/org/heartbeat")
		return
	}
	paused, err := c.Mgr.HeartbeatPaused(r.Context())
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, OrgHeartbeatResponse{Paused: paused})
}

func (c *OrgController) setHeartbeat(w http.ResponseWriter, r *http.Request) {
	if c.Mgr == nil {
		apispec.NotImplemented(w, r, "PUT", "/api/v1/org/heartbeat")
		return
	}
	var in orgsvc.SetHeartbeatPauseInput
	if err := decodeJSONStrict(r, &in); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Invalid JSON body", nil)
		return
	}
	if err := c.Mgr.SetHeartbeatPaused(r.Context(), in.Paused); err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, OrgHeartbeatResponse{Paused: in.Paused})
}

func (c *OrgController) setHQRole(w http.ResponseWriter, r *http.Request) {
	if c.Mgr == nil {
		apispec.NotImplemented(w, r, "PUT", "/api/v1/projects/{id}/hq")
		return
	}
	var in orgsvc.SetHQRoleInput
	if err := decodeJSONStrict(r, &in); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Invalid JSON body", nil)
		return
	}
	id := projectID(r)
	if err := c.Mgr.SetHQRole(r.Context(), id, in); err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, SetProjectHQRoleResponse{ProjectID: string(id), Role: in.Role})
}

func (c *OrgController) ensureHoldingHQ(w http.ResponseWriter, r *http.Request) {
	if c.Mgr == nil {
		apispec.NotImplemented(w, r, "POST", "/api/v1/org/holding-hq")
		return
	}
	id, err := c.Mgr.EnsureHoldingHQ(r.Context())
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, EnsureHQResponse{ProjectID: id})
}

func (c *OrgController) ensureCompanyHQ(w http.ResponseWriter, r *http.Request) {
	if c.Mgr == nil {
		apispec.NotImplemented(w, r, "POST", "/api/v1/org/companies/{companyId}/hq")
		return
	}
	companyID := chi.URLParam(r, "companyId")
	id, err := c.Mgr.EnsureCompanyHQ(r.Context(), companyID)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, EnsureHQResponse{ProjectID: id})
}
