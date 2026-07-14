package controllers

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	internalconfig "github.com/modernagent/modern-agent/backend/internal/config"
	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/httpd/apispec"
	"github.com/modernagent/modern-agent/backend/internal/httpd/envelope"
	"github.com/modernagent/modern-agent/backend/internal/policy"
	projectsvc "github.com/modernagent/modern-agent/backend/internal/service/project"
)

// PolicyController owns the policy engine's HTTP surface: the per-project
// config sub-resource (backed by projectsvc.Manager, same as the rest of a
// project's config) and the policy-run read/decide routes (backed by
// policy.Engine). Either dependency may be nil independently — each route
// 501s only when its own backing dependency is unwired.
type PolicyController struct {
	Projects projectsvc.Manager
	Engine   policy.Engine
}

// Register mounts the policy routes on the supplied router.
func (c *PolicyController) Register(r chi.Router) {
	r.Get("/projects/{id}/policy", c.getConfig)
	r.Put("/projects/{id}/policy", c.setConfig)
	r.Get("/policy/runs/{runId}", c.getRun)
	r.Get("/policy/runs/{runId}/gates", c.getGates)
	r.Post("/policy/runs/{runId}/decide", c.decide)
}

func (c *PolicyController) getConfig(w http.ResponseWriter, r *http.Request) {
	if c.Projects == nil {
		apispec.NotImplemented(w, r, "GET", "/api/v1/projects/{id}/policy")
		return
	}
	id := projectID(r)
	proj, err := c.getProjectOrWriteError(w, r, id)
	if err != nil {
		return
	}
	cfg := internalconfig.DefaultPolicyConfig()
	if proj.Config != nil {
		cfg = proj.Config.Policy.WithDefaults()
	}
	envelope.WriteJSON(w, http.StatusOK, PolicyConfigResponse{
		ProjectID: string(id),
		Config:    newPolicyConfigDTO(cfg),
	})
}

func (c *PolicyController) setConfig(w http.ResponseWriter, r *http.Request) {
	if c.Projects == nil {
		apispec.NotImplemented(w, r, "PUT", "/api/v1/projects/{id}/policy")
		return
	}
	id := projectID(r)
	var diff UpdatePolicyConfigRequest
	if err := decodeJSONStrict(r, &diff); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Invalid JSON body", nil)
		return
	}
	proj, err := c.getProjectOrWriteError(w, r, id)
	if err != nil {
		return
	}
	full := domain.ProjectConfig{}
	if proj.Config != nil {
		full = *proj.Config
	}
	full.Policy = diff.applyTo(internalconfig.DefaultPolicyConfig())
	updated, err := c.Projects.SetConfig(r.Context(), id, projectsvc.SetConfigInput{Config: full})
	if err != nil {
		envelope.WriteError(w, r, err)
		return
	}
	if updated.Config == nil {
		envelope.WriteAPIError(w, r, http.StatusInternalServerError, "internal", "POLICY_CONFIG_UPDATE_FAILED", "Policy config update did not persist", nil)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, PolicyConfigResponse{
		ProjectID: string(id),
		Config:    newPolicyConfigDTO(updated.Config.Policy),
	})
}

// getProjectOrWriteError fetches a project and writes the appropriate error
// envelope (404 unknown, 409 degraded config, or the service's own error) when
// it cannot be used as a policy target. It returns a nil error only when proj
// is safe to read from.
func (c *PolicyController) getProjectOrWriteError(w http.ResponseWriter, r *http.Request, id domain.ProjectID) (projectsvc.Project, error) {
	res, err := c.Projects.Get(r.Context(), id)
	if err != nil {
		envelope.WriteError(w, r, err)
		return projectsvc.Project{}, err
	}
	if res.Project == nil {
		envelope.WriteAPIError(w, r, http.StatusConflict, "conflict", "PROJECT_DEGRADED", "Project config failed to load", nil)
		return projectsvc.Project{}, errProjectDegraded
	}
	return *res.Project, nil
}

var errProjectDegraded = errors.New("controllers: project config is degraded")

func (c *PolicyController) getRun(w http.ResponseWriter, r *http.Request) {
	if c.Engine == nil {
		apispec.NotImplemented(w, r, "GET", "/api/v1/policy/runs/{runId}")
		return
	}
	runID := chi.URLParam(r, "runId")
	run, err := c.Engine.GetRun(r.Context(), runID)
	if err != nil {
		writePolicyError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, newPolicyRunDTO(run))
}

func (c *PolicyController) getGates(w http.ResponseWriter, r *http.Request) {
	if c.Engine == nil {
		apispec.NotImplemented(w, r, "GET", "/api/v1/policy/runs/{runId}/gates")
		return
	}
	runID := chi.URLParam(r, "runId")
	run, err := c.Engine.GetRun(r.Context(), runID)
	if err != nil {
		writePolicyError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, PolicyRunGatesResponse{
		RunID: runID,
		Gates: newGateResultDTOs(run.GateHistory),
	})
}

func (c *PolicyController) decide(w http.ResponseWriter, r *http.Request) {
	if c.Engine == nil {
		apispec.NotImplemented(w, r, "POST", "/api/v1/policy/runs/{runId}/decide")
		return
	}
	runID := chi.URLParam(r, "runId")
	var in PolicyDecideRequest
	if err := decodeJSONStrict(r, &in); err != nil {
		envelope.WriteAPIError(w, r, http.StatusBadRequest, "bad_request", "INVALID_JSON", "Invalid JSON body", nil)
		return
	}
	decision := in.toDecision()
	if err := decision.Validate(); err != nil {
		envelope.WriteAPIError(w, r, http.StatusUnprocessableEntity, "unprocessable", "POLICY_DECISION_INVALID", err.Error(), nil)
		return
	}
	if err := c.Engine.Decide(r.Context(), runID, decision); err != nil {
		writePolicyError(w, r, err)
		return
	}
	run, err := c.Engine.GetRun(r.Context(), runID)
	if err != nil {
		writePolicyError(w, r, err)
		return
	}
	envelope.WriteJSON(w, http.StatusOK, newPolicyRunDTO(run))
}

// writePolicyError maps policy engine sentinel errors to their locked HTTP
// envelopes, falling back to 500 for unexpected failures.
func writePolicyError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, policy.ErrRunNotFound):
		envelope.WriteAPIError(w, r, http.StatusNotFound, "not_found", "POLICY_RUN_NOT_FOUND", "Unknown policy run", nil)
	case errors.Is(err, policy.ErrInvalidDecision):
		envelope.WriteAPIError(w, r, http.StatusUnprocessableEntity, "unprocessable", "POLICY_DECISION_INVALID", err.Error(), nil)
	case errors.Is(err, policy.ErrAlreadyTerminal):
		envelope.WriteAPIError(w, r, http.StatusConflict, "conflict", "POLICY_RUN_ALREADY_TERMINAL", "Policy run has already reached a terminal state", nil)
	default:
		envelope.WriteAPIError(w, r, http.StatusInternalServerError, "internal", "POLICY_OPERATION_FAILED", "Policy operation failed", nil)
	}
}
