package controllers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/modernagent/modern-agent/backend/internal/httpd/apispec"
	"github.com/modernagent/modern-agent/backend/internal/httpd/envelope"
	companysvc "github.com/modernagent/modern-agent/backend/internal/service/company"
)

// CompaniesController owns the /companies routes and the project-company
// assignment route. The controller depends only on companysvc.Manager; nil
// keeps routes registered but returns OpenAPI-backed 501s.
type CompaniesController struct {
	Mgr companysvc.Manager
}

// Register mounts the company routes on the supplied router.
func (c *CompaniesController) Register(r chi.Router) {
	r.Get("/companies", c.list)
	r.Post("/companies", c.create)
	r.Put("/projects/{id}/company", c.assignProject)
}

func (c *CompaniesController) list(w http.ResponseWriter, r *http.Request) {
	if c.Mgr == nil {
		apispec.NotImplemented(w, r, "GET", "/api/v1/companies")
		return
	}
	companies, err := c.Mgr.List(r.Context())
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	if companies == nil {
		companies = []companysvc.Company{}
	}
	envelope.WriteJSON(w, http.StatusOK, ListCompaniesResponse{Companies: companies})
}

func (c *CompaniesController) create(w http.ResponseWriter, r *http.Request) {
	if c.Mgr == nil {
		apispec.NotImplemented(w, r, "POST", "/api/v1/companies")
		return
	}
	var in companysvc.CreateInput
	if err := decodeJSONStrict(r, &in); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Invalid JSON body", nil)
		return
	}
	company, err := c.Mgr.Create(r.Context(), in)
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusCreated, CompanyResponse{Company: company})
}

func (c *CompaniesController) assignProject(w http.ResponseWriter, r *http.Request) {
	if c.Mgr == nil {
		apispec.NotImplemented(w, r, "PUT", "/api/v1/projects/{id}/company")
		return
	}
	var in companysvc.AssignProjectInput
	if err := decodeJSONStrict(r, &in); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Invalid JSON body", nil)
		return
	}
	id := projectID(r)
	if err := c.Mgr.AssignProject(r.Context(), id, in); err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, AssignProjectCompanyResponse{ProjectID: string(id), CompanyID: in.CompanyID})
}
