package controllers_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modernagent/modern-agent/backend/internal/config"
	"github.com/modernagent/modern-agent/backend/internal/httpd"
	companysvc "github.com/modernagent/modern-agent/backend/internal/service/company"
	orgsvc "github.com/modernagent/modern-agent/backend/internal/service/org"
	projectsvc "github.com/modernagent/modern-agent/backend/internal/service/project"
	"github.com/modernagent/modern-agent/backend/internal/storage/sqlite"
)

// newOrgTestServer builds a server whose Org, Companies, and Projects
// managers share one throwaway sqlite store.
func newOrgTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{
		Org:       orgsvc.New(store),
		Companies: companysvc.New(store),
		Projects:  projectsvc.New(store),
	}, httpd.ControlDeps{}))
	t.Cleanup(srv.Close)
	return srv
}

func TestOrgRoutes_DefaultToStubsWithoutManager(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{}, httpd.ControlDeps{}))
	t.Cleanup(srv.Close)

	body, status, headers := doRequest(t, srv, "GET", "/api/v1/org/overview", "")
	assertJSON(t, headers)
	assertErrorCode(t, body, status, http.StatusNotImplemented, "NOT_IMPLEMENTED")

	body, status, _ = doRequest(t, srv, "GET", "/api/v1/org/heartbeat", "")
	assertErrorCode(t, body, status, http.StatusNotImplemented, "NOT_IMPLEMENTED")

	body, status, _ = doRequest(t, srv, "PUT", "/api/v1/org/heartbeat", `{"paused":true}`)
	assertErrorCode(t, body, status, http.StatusNotImplemented, "NOT_IMPLEMENTED")

	body, status, _ = doRequest(t, srv, "PUT", "/api/v1/projects/whatever/hq", `{"role":"holding"}`)
	assertErrorCode(t, body, status, http.StatusNotImplemented, "NOT_IMPLEMENTED")
}

func TestOrgAPI_OverviewEmpty(t *testing.T) {
	srv := newOrgTestServer(t)

	body, status, headers := doRequest(t, srv, "GET", "/api/v1/org/overview", "")
	if status != http.StatusOK {
		t.Fatalf("GET overview = %d, want 200; body=%s", status, body)
	}
	assertJSON(t, headers)

	var got struct {
		Overview orgsvc.Overview `json:"overview"`
	}
	mustJSON(t, body, &got)
	if got.Overview.Paused {
		t.Fatal("fresh overview reports paused=true")
	}
	if got.Overview.HoldingHQ != nil {
		t.Fatalf("fresh overview has a holding hq: %#v", got.Overview.HoldingHQ)
	}
	if len(got.Overview.Companies) != 0 {
		t.Fatalf("fresh overview has companies: %#v", got.Overview.Companies)
	}
}

func TestOrgAPI_HeartbeatPauseRoundTrip(t *testing.T) {
	srv := newOrgTestServer(t)

	body, status, _ := doRequest(t, srv, "GET", "/api/v1/org/heartbeat", "")
	if status != http.StatusOK {
		t.Fatalf("GET heartbeat = %d, want 200; body=%s", status, body)
	}
	var got struct {
		Paused bool `json:"paused"`
	}
	mustJSON(t, body, &got)
	if got.Paused {
		t.Fatal("fresh heartbeat state reports paused=true")
	}

	body, status, _ = doRequest(t, srv, "PUT", "/api/v1/org/heartbeat", `{"paused":true}`)
	if status != http.StatusOK {
		t.Fatalf("PUT heartbeat = %d, want 200; body=%s", status, body)
	}
	mustJSON(t, body, &got)
	if !got.Paused {
		t.Fatal("PUT heartbeat paused=true did not round-trip")
	}

	body, status, _ = doRequest(t, srv, "GET", "/api/v1/org/heartbeat", "")
	if status != http.StatusOK {
		t.Fatalf("GET heartbeat after pause = %d, want 200; body=%s", status, body)
	}
	mustJSON(t, body, &got)
	if !got.Paused {
		t.Fatal("GET heartbeat after pause = false, want true (should survive across requests)")
	}
}

func TestOrgAPI_SetHQRoleRoundTripAndValidation(t *testing.T) {
	srv := newOrgTestServer(t)
	repo := gitRepo(t, "hq-repo")

	body, status, _ := doRequest(t, srv, "POST", "/api/v1/projects", `{"path":`+quote(repo)+`,"projectId":"uppu-hq"}`)
	if status != http.StatusCreated {
		t.Fatalf("seed project = %d, want 201; body=%s", status, body)
	}

	// Holding HQ requires no company — an unassigned project qualifies.
	body, status, _ = doRequest(t, srv, "PUT", "/api/v1/projects/uppu-hq/hq", `{"role":"holding"}`)
	if status != http.StatusOK {
		t.Fatalf("set holding hq = %d, want 200; body=%s", status, body)
	}

	body, status, _ = doRequest(t, srv, "GET", "/api/v1/org/overview", "")
	if status != http.StatusOK {
		t.Fatalf("GET overview = %d, want 200; body=%s", status, body)
	}
	var got struct {
		Overview orgsvc.Overview `json:"overview"`
	}
	mustJSON(t, body, &got)
	if got.Overview.HoldingHQ == nil || got.Overview.HoldingHQ.ProjectID != "uppu-hq" {
		t.Fatalf("overview holding hq = %#v, want project uppu-hq", got.Overview.HoldingHQ)
	}

	// An unknown role is rejected.
	body, status, _ = doRequest(t, srv, "PUT", "/api/v1/projects/uppu-hq/hq", `{"role":"empire"}`)
	assertErrorCode(t, body, status, http.StatusBadRequest, "HQ_ROLE_INVALID")

	// An unknown project is rejected.
	body, status, _ = doRequest(t, srv, "PUT", "/api/v1/projects/no-such-project/hq", `{"role":"company"}`)
	assertErrorCode(t, body, status, http.StatusNotFound, "PROJECT_NOT_FOUND")

	// A company HQ role on a company-assigned project succeeds; on an
	// unassigned project it's rejected.
	body, status, _ = doRequest(t, srv, "POST", "/api/v1/companies", `{"name":"Acme"}`)
	if status != http.StatusCreated {
		t.Fatalf("seed company = %d, want 201; body=%s", status, body)
	}
	body, status, _ = doRequest(t, srv, "PUT", "/api/v1/projects/uppu-hq/hq", `{"role":"company"}`)
	assertErrorCode(t, body, status, http.StatusBadRequest, "HQ_REQUIRES_COMPANY")
}
