package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

const orgOverviewFixture = `{"overview":{` +
	`"holdingHq":{"projectId":"uppu-hq","orchestratorSessionId":"uppu-hq-1","activity":"idle","heartbeatEnabled":true,"heartbeatInterval":"30m"},` +
	`"companies":[{"id":"acme","name":"Acme","hq":{"projectId":"acme-hq","orchestratorSessionId":"acme-hq-1","activity":"idle","heartbeatEnabled":true,"heartbeatInterval":"30m"},` +
	`"projects":[{"id":"acme-api","name":"Acme API","kind":"single_repo","orchestratorSessionId":"acme-api-1","orchestratorActivity":"active","activeSessions":1,"totalSessions":2}]}],` +
	`"paused":false}}`

func orgCommandServer(t *testing.T) (*httptest.Server, *sessionRequestLog) {
	t.Helper()
	log := &sessionRequestLog{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.append(r)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/org/overview":
			_, _ = io.WriteString(w, orgOverviewFixture)
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/org/heartbeat":
			var body orgHeartbeatDTO
			_ = json.NewDecoder(r.Body).Decode(&body)
			_, _ = io.WriteString(w, `{"paused":`+boolJSON(body.Paused)+`}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, log
}

func boolJSON(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func TestOrgStatus_TableOutput(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, log := orgCommandServer(t)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "org", "status")
	if err != nil {
		t.Fatalf("org status failed: %v\nstderr=%s", err, errOut)
	}
	for _, want := range []string{
		"heartbeat: running",
		"holding hq: uppu-hq",
		"acme (Acme):",
		"hq: acme-hq",
		"acme-api (Acme API)",
		"orchestrator acme-api-1 (active)",
		"sessions 1/2",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	want := []string{"GET /api/v1/org/overview"}
	if got := log.all(); !reflect.DeepEqual(got, want) {
		t.Fatalf("requests = %#v, want %#v", got, want)
	}
}

func TestOrgStatus_JSONOutputDecodes(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := orgCommandServer(t)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "org", "status", "--json")
	if err != nil {
		t.Fatalf("org status --json failed: %v\nstderr=%s", err, errOut)
	}
	var got orgOverview
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("org status --json output is not decodable: %v\noutput=%s", err, out)
	}
	if got.HoldingHQ == nil || got.HoldingHQ.ProjectID != "uppu-hq" {
		t.Fatalf("holdingHq = %#v, want uppu-hq", got.HoldingHQ)
	}
	if len(got.Companies) != 1 || got.Companies[0].ID != "acme" {
		t.Fatalf("companies = %#v, want [acme]", got.Companies)
	}
}

func TestOrgStatus_CompanyFilter(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := orgCommandServer(t)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "org", "status", "--company", "acme")
	if err != nil {
		t.Fatalf("org status --company acme failed: %v\nstderr=%s", err, errOut)
	}
	if strings.Contains(out, "holding hq:") {
		t.Fatalf("company-filtered output must not show the holding hq:\n%s", out)
	}
	if !strings.Contains(out, "acme (Acme):") {
		t.Fatalf("output missing filtered company:\n%s", out)
	}
}

func TestOrgStatus_UnknownCompanyErrors(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := orgCommandServer(t)
	writeRunFileFor(t, cfg, srv)

	_, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "org", "status", "--company", "no-such-company")
	if err == nil {
		t.Fatal("expected an error for an unknown company id")
	}
	if !strings.Contains(err.Error(), "no-such-company") {
		t.Fatalf("error = %v, want it to mention the unknown company id; stderr=%s", err, errOut)
	}
}

func TestOrgPauseAndResume(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, log := orgCommandServer(t)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "org", "pause")
	if err != nil {
		t.Fatalf("org pause failed: %v\nstderr=%s", err, errOut)
	}
	if !strings.Contains(out, "heartbeat paused") {
		t.Fatalf("output missing paused confirmation:\n%s", out)
	}

	out, errOut, err = executeCLI(t, Deps{ProcessAlive: func(int) bool { return true }}, "org", "resume")
	if err != nil {
		t.Fatalf("org resume failed: %v\nstderr=%s", err, errOut)
	}
	if !strings.Contains(out, "heartbeat resumed") {
		t.Fatalf("output missing resumed confirmation:\n%s", out)
	}

	want := []string{"PUT /api/v1/org/heartbeat", "PUT /api/v1/org/heartbeat"}
	if got := log.all(); !reflect.DeepEqual(got, want) {
		t.Fatalf("requests = %#v, want %#v", got, want)
	}
}
