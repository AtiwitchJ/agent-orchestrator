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
	projectsvc "github.com/modernagent/modern-agent/backend/internal/service/project"
	"github.com/modernagent/modern-agent/backend/internal/storage/sqlite"
)

// newCompaniesTestServer builds a server whose Companies and Projects
// managers share one throwaway sqlite store, so a project registered through
// /projects can be assigned a company through /projects/{id}/company.
func newCompaniesTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{
		Companies: companysvc.New(store),
		Projects:  projectsvc.New(store),
	}, httpd.ControlDeps{}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCompaniesRoutes_DefaultToStubsWithoutManager(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(httpd.NewRouterWithControl(config.Config{}, log, nil, httpd.APIDeps{}, httpd.ControlDeps{}))
	t.Cleanup(srv.Close)

	body, status, headers := doRequest(t, srv, "GET", "/api/v1/companies", "")
	assertJSON(t, headers)
	assertErrorCode(t, body, status, http.StatusNotImplemented, "NOT_IMPLEMENTED")

	body, status, _ = doRequest(t, srv, "PUT", "/api/v1/projects/whatever/company", `{"companyId":"acme"}`)
	assertErrorCode(t, body, status, http.StatusNotImplemented, "NOT_IMPLEMENTED")
}

func TestCompaniesAPI_ListCreate(t *testing.T) {
	srv := newCompaniesTestServer(t)

	body, status, headers := doRequest(t, srv, "GET", "/api/v1/companies", "")
	if status != http.StatusOK {
		t.Fatalf("GET companies = %d, want 200; body=%s", status, body)
	}
	assertJSON(t, headers)

	var list struct {
		Companies []companysvc.Company `json:"companies"`
	}
	mustJSON(t, body, &list)
	if len(list.Companies) != 0 {
		t.Fatalf("initial company count = %d, want 0", len(list.Companies))
	}

	body, status, _ = doRequest(t, srv, "POST", "/api/v1/companies", `{"name":"Acme Corp"}`)
	if status != http.StatusCreated {
		t.Fatalf("POST company = %d, want 201; body=%s", status, body)
	}
	var created struct {
		Company companysvc.Company `json:"company"`
	}
	mustJSON(t, body, &created)
	if created.Company.ID != "acme-corp" || created.Company.Name != "Acme Corp" {
		t.Fatalf("created company = %#v", created.Company)
	}

	body, status, _ = doRequest(t, srv, "GET", "/api/v1/companies", "")
	if status != http.StatusOK {
		t.Fatalf("GET companies after create = %d, want 200; body=%s", status, body)
	}
	mustJSON(t, body, &list)
	if len(list.Companies) != 1 || list.Companies[0].ID != "acme-corp" {
		t.Fatalf("companies after create = %#v", list.Companies)
	}
}

func TestCompaniesAPI_CreateValidation(t *testing.T) {
	srv := newCompaniesTestServer(t)

	body, status, _ := doRequest(t, srv, "POST", "/api/v1/companies", `{`)
	assertErrorCode(t, body, status, http.StatusBadRequest, "INVALID_JSON")

	body, status, _ = doRequest(t, srv, "POST", "/api/v1/companies", `{"name":""}`)
	assertErrorCode(t, body, status, http.StatusBadRequest, "NAME_REQUIRED")
}

func TestCompaniesAPI_AssignAndUnassignProject(t *testing.T) {
	srv := newCompaniesTestServer(t)
	repo := gitRepo(t, "assign-repo")

	body, status, _ := doRequest(t, srv, "POST", "/api/v1/projects", `{"path":`+quote(repo)+`,"projectId":"ao"}`)
	if status != http.StatusCreated {
		t.Fatalf("seed project = %d, want 201; body=%s", status, body)
	}
	body, status, _ = doRequest(t, srv, "POST", "/api/v1/companies", `{"name":"Acme"}`)
	if status != http.StatusCreated {
		t.Fatalf("seed company = %d, want 201; body=%s", status, body)
	}

	body, status, _ = doRequest(t, srv, "PUT", "/api/v1/projects/ao/company", `{"companyId":"acme"}`)
	if status != http.StatusOK {
		t.Fatalf("assign = %d, want 200; body=%s", status, body)
	}

	body, status, _ = doRequest(t, srv, "GET", "/api/v1/projects", "")
	if status != http.StatusOK {
		t.Fatalf("GET projects = %d, want 200; body=%s", status, body)
	}
	var afterAssign struct {
		Projects []projectsvc.Summary `json:"projects"`
	}
	mustJSON(t, body, &afterAssign)
	if len(afterAssign.Projects) != 1 || afterAssign.Projects[0].CompanyID != "acme" {
		t.Fatalf("project summary after assign = %#v, want companyId acme", afterAssign.Projects)
	}

	// Unassign with an empty companyId.
	body, status, _ = doRequest(t, srv, "PUT", "/api/v1/projects/ao/company", `{"companyId":""}`)
	if status != http.StatusOK {
		t.Fatalf("unassign = %d, want 200; body=%s", status, body)
	}
	body, status, _ = doRequest(t, srv, "GET", "/api/v1/projects", "")
	// A fresh struct — CompanyID has `omitempty`, so a stale reused variable
	// would keep the prior "acme" value when the key is absent from this body.
	var afterUnassign struct {
		Projects []projectsvc.Summary `json:"projects"`
	}
	mustJSON(t, body, &afterUnassign)
	if len(afterUnassign.Projects) != 1 || afterUnassign.Projects[0].CompanyID != "" {
		t.Fatalf("project summary after unassign = %#v, want empty companyId", afterUnassign.Projects)
	}
}

func TestCompaniesAPI_AssignRejectsUnknownCompanyOrProject(t *testing.T) {
	srv := newCompaniesTestServer(t)
	repo := gitRepo(t, "reject-repo")

	body, status, _ := doRequest(t, srv, "POST", "/api/v1/projects", `{"path":`+quote(repo)+`,"projectId":"ao"}`)
	if status != http.StatusCreated {
		t.Fatalf("seed project = %d, want 201; body=%s", status, body)
	}

	body, status, _ = doRequest(t, srv, "PUT", "/api/v1/projects/ao/company", `{"companyId":"no-such-company"}`)
	assertErrorCode(t, body, status, http.StatusNotFound, "COMPANY_NOT_FOUND")

	body, status, _ = doRequest(t, srv, "POST", "/api/v1/companies", `{"name":"Acme"}`)
	if status != http.StatusCreated {
		t.Fatalf("seed company = %d, want 201; body=%s", status, body)
	}
	body, status, _ = doRequest(t, srv, "PUT", "/api/v1/projects/no-such-project/company", `{"companyId":"acme"}`)
	assertErrorCode(t, body, status, http.StatusNotFound, "PROJECT_NOT_FOUND")
}
