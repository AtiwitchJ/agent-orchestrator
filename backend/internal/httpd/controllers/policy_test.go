package controllers_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	internalconfig "github.com/modernagent/modern-agent/backend/internal/config"
	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/httpd"
	"github.com/modernagent/modern-agent/backend/internal/httpd/apierr"
	"github.com/modernagent/modern-agent/backend/internal/policy"
	projectsvc "github.com/modernagent/modern-agent/backend/internal/service/project"
)

// projectNotFoundErr mirrors the *apierr.Error projectsvc.Service.Get actually
// returns for an unknown project id, so tests exercise the same envelope.WriteError
// path production traffic hits.
var projectNotFoundErr = apierr.NotFound("PROJECT_NOT_FOUND", "Unknown project")

type fakePolicyProjectManager struct {
	projectsvc.Manager
	getResult projectsvc.GetResult
	getErr    error

	setConfigResult projectsvc.Project
	setConfigErr    error
	lastSetConfig   projectsvc.SetConfigInput
}

func (f *fakePolicyProjectManager) Get(context.Context, domain.ProjectID) (projectsvc.GetResult, error) {
	return f.getResult, f.getErr
}

func (f *fakePolicyProjectManager) SetConfig(_ context.Context, _ domain.ProjectID, in projectsvc.SetConfigInput) (projectsvc.Project, error) {
	f.lastSetConfig = in
	return f.setConfigResult, f.setConfigErr
}

type fakePolicyEngine struct {
	runResult policy.Run
	runErr    error
	decideErr error
	decided   []policy.Decision
}

func (f *fakePolicyEngine) Run(context.Context, string) error { return nil }

func (f *fakePolicyEngine) Decide(_ context.Context, _ string, d policy.Decision) error {
	f.decided = append(f.decided, d)
	return f.decideErr
}

func (f *fakePolicyEngine) GetRun(context.Context, string) (policy.Run, error) {
	return f.runResult, f.runErr
}

func newPolicyTestServer(t *testing.T, projects projectsvc.Manager, engine policy.Engine) *httptest.Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(httpd.NewRouterWithControl(internalconfig.Config{}, log, nil, httpd.APIDeps{Projects: projects, Policy: engine}, httpd.ControlDeps{}))
	t.Cleanup(srv.Close)
	return srv
}

// ---- Nil deps -> 501 ----

func TestPolicyRoutes_NilProjects_ConfigRoutesReturn501(t *testing.T) {
	srv := newPolicyTestServer(t, nil, &fakePolicyEngine{})

	body, status, headers := doRequest(t, srv, "GET", "/api/v1/projects/proj-1/policy", "")
	assertJSON(t, headers)
	assertErrorCode(t, body, status, http.StatusNotImplemented, "NOT_IMPLEMENTED")

	body, status, headers = doRequest(t, srv, "PUT", "/api/v1/projects/proj-1/policy", "{}")
	assertJSON(t, headers)
	assertErrorCode(t, body, status, http.StatusNotImplemented, "NOT_IMPLEMENTED")
}

func TestPolicyRoutes_NilEngine_RunRoutesReturn501(t *testing.T) {
	srv := newPolicyTestServer(t, &fakePolicyProjectManager{}, nil)

	body, status, headers := doRequest(t, srv, "GET", "/api/v1/policy/runs/run-1", "")
	assertJSON(t, headers)
	assertErrorCode(t, body, status, http.StatusNotImplemented, "NOT_IMPLEMENTED")

	body, status, headers = doRequest(t, srv, "GET", "/api/v1/policy/runs/run-1/gates", "")
	assertJSON(t, headers)
	assertErrorCode(t, body, status, http.StatusNotImplemented, "NOT_IMPLEMENTED")

	body, status, headers = doRequest(t, srv, "POST", "/api/v1/policy/runs/run-1/decide", `{"action":"approve"}`)
	assertJSON(t, headers)
	assertErrorCode(t, body, status, http.StatusNotImplemented, "NOT_IMPLEMENTED")
}

// ---- GET /projects/{id}/policy ----

func TestPolicyRoutes_GetConfig_NoStoredConfig_ReturnsDefaults(t *testing.T) {
	mgr := &fakePolicyProjectManager{getResult: projectsvc.GetResult{
		Status:  "ok",
		Project: &projectsvc.Project{ID: "proj-1"},
	}}
	srv := newPolicyTestServer(t, mgr, &fakePolicyEngine{})

	body, status, _ := doRequest(t, srv, "GET", "/api/v1/projects/proj-1/policy", "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", status, body)
	}
	var resp struct {
		ProjectID string `json:"projectId"`
		Config    struct {
			Enabled          bool   `json:"enabled"`
			TrackerLabel     string `json:"trackerLabel"`
			MaxAutoFixRounds int    `json:"maxAutoFixRounds"`
			ReviewStrategy   string `json:"reviewStrategy"`
		} `json:"config"`
	}
	mustJSON(t, body, &resp)
	def := internalconfig.DefaultPolicyConfig()
	if resp.ProjectID != "proj-1" {
		t.Errorf("projectId = %q, want proj-1", resp.ProjectID)
	}
	if resp.Config.Enabled != def.Enabled || resp.Config.TrackerLabel != def.TrackerLabel ||
		resp.Config.MaxAutoFixRounds != def.MaxAutoFixRounds || resp.Config.ReviewStrategy != def.ReviewStrategy {
		t.Errorf("config = %+v, want defaults %+v", resp.Config, def)
	}
}

func TestPolicyRoutes_GetConfig_StoredOverridesAreMerged(t *testing.T) {
	stored := internalconfig.PolicyConfig{Enabled: true, MaxAutoFixRounds: 5}
	mgr := &fakePolicyProjectManager{getResult: projectsvc.GetResult{
		Status:  "ok",
		Project: &projectsvc.Project{ID: "proj-1", Config: &domain.ProjectConfig{Policy: stored}},
	}}
	srv := newPolicyTestServer(t, mgr, &fakePolicyEngine{})

	body, status, _ := doRequest(t, srv, "GET", "/api/v1/projects/proj-1/policy", "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", status, body)
	}
	var resp struct {
		Config struct {
			Enabled          bool   `json:"enabled"`
			MaxAutoFixRounds int    `json:"maxAutoFixRounds"`
			ReviewStrategy   string `json:"reviewStrategy"`
		} `json:"config"`
	}
	mustJSON(t, body, &resp)
	// Enabled/MaxAutoFixRounds are the explicit overrides; ReviewStrategy falls
	// back to the default because WithDefaults fills it.
	if !resp.Config.Enabled || resp.Config.MaxAutoFixRounds != 5 {
		t.Errorf("config = %+v, want overrides preserved", resp.Config)
	}
	if resp.Config.ReviewStrategy != internalconfig.PolicyReviewSameAgent {
		t.Errorf("reviewStrategy = %q, want default %q", resp.Config.ReviewStrategy, internalconfig.PolicyReviewSameAgent)
	}
}

func TestPolicyRoutes_GetConfig_ProjectNotFound_404(t *testing.T) {
	mgr := &fakePolicyProjectManager{getErr: projectNotFoundErr}
	srv := newPolicyTestServer(t, mgr, &fakePolicyEngine{})

	body, status, headers := doRequest(t, srv, "GET", "/api/v1/projects/missing/policy", "")
	assertJSON(t, headers)
	assertErrorCode(t, body, status, http.StatusNotFound, "PROJECT_NOT_FOUND")
}

func TestPolicyRoutes_GetConfig_DegradedProject_409(t *testing.T) {
	mgr := &fakePolicyProjectManager{getResult: projectsvc.GetResult{
		Status:   "degraded",
		Degraded: &projectsvc.Degraded{ID: "proj-1"},
	}}
	srv := newPolicyTestServer(t, mgr, &fakePolicyEngine{})

	body, status, headers := doRequest(t, srv, "GET", "/api/v1/projects/proj-1/policy", "")
	assertJSON(t, headers)
	assertErrorCode(t, body, status, http.StatusConflict, "PROJECT_DEGRADED")
}

// ---- PUT /projects/{id}/policy ----

func TestPolicyRoutes_SetConfig_InvalidJSON_400(t *testing.T) {
	mgr := &fakePolicyProjectManager{getResult: projectsvc.GetResult{Status: "ok", Project: &projectsvc.Project{ID: "proj-1"}}}
	srv := newPolicyTestServer(t, mgr, &fakePolicyEngine{})

	body, status, headers := doRequest(t, srv, "PUT", "/api/v1/projects/proj-1/policy", `{"enabled": not-json}`)
	assertJSON(t, headers)
	assertErrorCode(t, body, status, http.StatusBadRequest, "INVALID_JSON")
}

func TestPolicyRoutes_SetConfig_EmptyDiff_ResetsToDefaults(t *testing.T) {
	stored := internalconfig.PolicyConfig{Enabled: true, MaxAutoFixRounds: 5}
	mgr := &fakePolicyProjectManager{
		getResult: projectsvc.GetResult{Status: "ok", Project: &projectsvc.Project{ID: "proj-1", Config: &domain.ProjectConfig{Policy: stored}}},
	}
	mgr.setConfigResult = projectsvc.Project{ID: "proj-1", Config: &domain.ProjectConfig{Policy: internalconfig.DefaultPolicyConfig()}}
	srv := newPolicyTestServer(t, mgr, &fakePolicyEngine{})

	body, status, _ := doRequest(t, srv, "PUT", "/api/v1/projects/proj-1/policy", "{}")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", status, body)
	}
	if mgr.lastSetConfig.Config.Policy != internalconfig.DefaultPolicyConfig() {
		t.Errorf("SetConfig received %+v, want defaults", mgr.lastSetConfig.Config.Policy)
	}
}

func TestPolicyRoutes_SetConfig_DiffOverlaysDefaultsAndPreservesOtherFields(t *testing.T) {
	mgr := &fakePolicyProjectManager{
		getResult: projectsvc.GetResult{Status: "ok", Project: &projectsvc.Project{
			ID: "proj-1",
			Config: &domain.ProjectConfig{
				DefaultBranch: "main",
				Env:           map[string]string{"FOO": "bar"},
			},
		}},
	}
	mgr.setConfigResult = projectsvc.Project{ID: "proj-1", Config: &domain.ProjectConfig{}}
	srv := newPolicyTestServer(t, mgr, &fakePolicyEngine{})

	body, status, _ := doRequest(t, srv, "PUT", "/api/v1/projects/proj-1/policy", `{"enabled":true,"maxAutoFixRounds":7}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", status, body)
	}
	got := mgr.lastSetConfig.Config
	if !got.Policy.Enabled || got.Policy.MaxAutoFixRounds != 7 {
		t.Errorf("Policy = %+v, want Enabled=true MaxAutoFixRounds=7", got.Policy)
	}
	if got.Policy.ReviewStrategy != internalconfig.PolicyReviewSameAgent {
		t.Errorf("ReviewStrategy = %q, want default", got.Policy.ReviewStrategy)
	}
	if got.DefaultBranch != "main" || got.Env["FOO"] != "bar" {
		t.Errorf("non-policy fields not preserved: %+v", got)
	}
}

func TestPolicyRoutes_SetConfig_ProjectNotFound_404(t *testing.T) {
	mgr := &fakePolicyProjectManager{getErr: projectNotFoundErr}
	srv := newPolicyTestServer(t, mgr, &fakePolicyEngine{})

	body, status, headers := doRequest(t, srv, "PUT", "/api/v1/projects/missing/policy", "{}")
	assertJSON(t, headers)
	assertErrorCode(t, body, status, http.StatusNotFound, "PROJECT_NOT_FOUND")
}

// ---- GET /policy/runs/{runId} ----

func TestPolicyRoutes_GetRun_200(t *testing.T) {
	run := policy.Run{
		ID: "run-1", ProjectID: "proj-1", SessionID: "sess-1", PRID: "pr-1",
		Config:      policy.DefaultPolicyConfig(),
		CurrentGate: policy.GateHuman,
		StartedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		GateHistory: []policy.GateResult{
			{RunID: "run-1", GateID: policy.GateCI, Attempt: 1, Outcome: policy.OutcomePass, Duration: 30 * time.Second},
		},
	}
	engine := &fakePolicyEngine{runResult: run}
	srv := newPolicyTestServer(t, &fakePolicyProjectManager{}, engine)

	body, status, _ := doRequest(t, srv, "GET", "/api/v1/policy/runs/run-1", "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", status, body)
	}
	var resp struct {
		ID          string `json:"id"`
		CurrentGate string `json:"currentGate"`
		History     []struct {
			GateID     string `json:"gateId"`
			Outcome    string `json:"outcome"`
			DurationMS int64  `json:"durationMs"`
		} `json:"history"`
	}
	mustJSON(t, body, &resp)
	if resp.ID != "run-1" || resp.CurrentGate != string(policy.GateHuman) {
		t.Errorf("resp = %+v", resp)
	}
	if len(resp.History) != 1 || resp.History[0].GateID != string(policy.GateCI) || resp.History[0].Outcome != string(policy.OutcomePass) || resp.History[0].DurationMS != 30000 {
		t.Errorf("history = %+v", resp.History)
	}
}

func TestPolicyRoutes_GetRun_NotFound_404(t *testing.T) {
	engine := &fakePolicyEngine{runErr: policy.ErrRunNotFound}
	srv := newPolicyTestServer(t, &fakePolicyProjectManager{}, engine)

	body, status, headers := doRequest(t, srv, "GET", "/api/v1/policy/runs/missing", "")
	assertJSON(t, headers)
	assertErrorCode(t, body, status, http.StatusNotFound, "POLICY_RUN_NOT_FOUND")
}

// ---- GET /policy/runs/{runId}/gates ----

func TestPolicyRoutes_GetGates_200(t *testing.T) {
	run := policy.Run{
		ID: "run-1",
		GateHistory: []policy.GateResult{
			{RunID: "run-1", GateID: policy.GateCI, Attempt: 1, Outcome: policy.OutcomePass},
			{RunID: "run-1", GateID: policy.GateReview, Attempt: 1, Outcome: policy.OutcomeFail, Reason: "changes requested"},
		},
	}
	engine := &fakePolicyEngine{runResult: run}
	srv := newPolicyTestServer(t, &fakePolicyProjectManager{}, engine)

	body, status, _ := doRequest(t, srv, "GET", "/api/v1/policy/runs/run-1/gates", "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", status, body)
	}
	var resp struct {
		RunID string `json:"runId"`
		Gates []struct {
			GateID string `json:"gateId"`
		} `json:"gates"`
	}
	mustJSON(t, body, &resp)
	if resp.RunID != "run-1" || len(resp.Gates) != 2 {
		t.Errorf("resp = %+v", resp)
	}
}

// ---- POST /policy/runs/{runId}/decide ----

func TestPolicyRoutes_Decide_InvalidAction_422(t *testing.T) {
	engine := &fakePolicyEngine{}
	srv := newPolicyTestServer(t, &fakePolicyProjectManager{}, engine)

	body, status, headers := doRequest(t, srv, "POST", "/api/v1/policy/runs/run-1/decide", `{"action":"bogus"}`)
	assertJSON(t, headers)
	assertErrorCode(t, body, status, http.StatusUnprocessableEntity, "POLICY_DECISION_INVALID")
	if len(engine.decided) != 0 {
		t.Error("engine.Decide should not be called for an invalid decision")
	}
}

func TestPolicyRoutes_Decide_OverrideWithoutJustification_422(t *testing.T) {
	engine := &fakePolicyEngine{}
	srv := newPolicyTestServer(t, &fakePolicyProjectManager{}, engine)

	body, status, headers := doRequest(t, srv, "POST", "/api/v1/policy/runs/run-1/decide", `{"action":"override"}`)
	assertJSON(t, headers)
	assertErrorCode(t, body, status, http.StatusUnprocessableEntity, "POLICY_DECISION_INVALID")
}

func TestPolicyRoutes_Decide_NotFound_404(t *testing.T) {
	engine := &fakePolicyEngine{decideErr: policy.ErrRunNotFound}
	srv := newPolicyTestServer(t, &fakePolicyProjectManager{}, engine)

	body, status, headers := doRequest(t, srv, "POST", "/api/v1/policy/runs/missing/decide", `{"action":"approve"}`)
	assertJSON(t, headers)
	assertErrorCode(t, body, status, http.StatusNotFound, "POLICY_RUN_NOT_FOUND")
}

func TestPolicyRoutes_Decide_AlreadyTerminal_409(t *testing.T) {
	engine := &fakePolicyEngine{decideErr: policy.ErrAlreadyTerminal}
	srv := newPolicyTestServer(t, &fakePolicyProjectManager{}, engine)

	body, status, headers := doRequest(t, srv, "POST", "/api/v1/policy/runs/run-1/decide", `{"action":"approve"}`)
	assertJSON(t, headers)
	assertErrorCode(t, body, status, http.StatusConflict, "POLICY_RUN_ALREADY_TERMINAL")
}

func TestPolicyRoutes_Decide_200(t *testing.T) {
	engine := &fakePolicyEngine{runResult: policy.Run{ID: "run-1", CurrentGate: policy.GateFinal}}
	srv := newPolicyTestServer(t, &fakePolicyProjectManager{}, engine)

	body, status, _ := doRequest(t, srv, "POST", "/api/v1/policy/runs/run-1/decide", `{"action":"request_changes","message":"needs tests"}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", status, body)
	}
	if len(engine.decided) != 1 {
		t.Fatalf("engine.Decide called %d times, want 1", len(engine.decided))
	}
	if engine.decided[0].Action != policy.DecisionRequestChanges || engine.decided[0].Justification != "needs tests" {
		t.Errorf("decided = %+v", engine.decided[0])
	}
	var resp struct {
		ID string `json:"id"`
	}
	mustJSON(t, body, &resp)
	if resp.ID != "run-1" {
		t.Errorf("id = %q, want run-1", resp.ID)
	}
}
