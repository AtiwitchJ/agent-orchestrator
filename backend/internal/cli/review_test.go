package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// reviewCapture records the method/path/body of the request the CLI made.
type reviewCapture struct {
	method string
	path   string
	body   string
}

func reviewServer(t *testing.T, status int, respBody string) (*httptest.Server, *reviewCapture) {
	t.Helper()
	capture := &reviewCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capture.method = r.Method
		capture.path = r.URL.Path
		capture.body = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, respBody)
	}))
	t.Cleanup(srv.Close)
	return srv, capture
}

func aliveDeps() Deps { return Deps{ProcessAlive: func(int) bool { return true }} }

func TestReviewSubmitReadsBodyFile(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := reviewServer(t, http.StatusOK, `{"review":{"verdict":"changes_requested"}}`)
	writeRunFileFor(t, cfg, srv)

	bodyFile := filepath.Join(t.TempDir(), "review.md")
	if err := os.WriteFile(bodyFile, []byte("please fix"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, errOut, err := executeCLI(t, aliveDeps(),
		"review", "submit", "mer-1", "--run", "run-1", "--verdict", "changes_requested", "--body", bodyFile)
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if capture.method != http.MethodPost || capture.path != "/api/v1/sessions/mer-1/reviews/submit" {
		t.Fatalf("request = %s %s", capture.method, capture.path)
	}
	var req submitReviewRequest
	if err := json.Unmarshal([]byte(capture.body), &req); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if req.RunID != "run-1" || req.Verdict != "changes_requested" || req.Body != "please fix" {
		t.Fatalf("request = %+v", req)
	}
}

func TestReviewSubmitReadsBodyFromStdin(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := reviewServer(t, http.StatusOK, `{"review":{"verdict":"changes_requested"}}`)
	writeRunFileFor(t, cfg, srv)

	deps := aliveDeps()
	deps.In = strings.NewReader("please fix from stdin")
	_, errOut, err := executeCLI(t, deps,
		"review", "submit", "mer-1", "--run", "run-1", "--verdict", "changes_requested", "--body", "-")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	var req submitReviewRequest
	if err := json.Unmarshal([]byte(capture.body), &req); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if req.Body != "please fix from stdin" {
		t.Fatalf("body = %q, want the stdin contents", req.Body)
	}
}

func TestReviewSubmitAcceptsUnderscoreFlags(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := reviewServer(t, http.StatusOK, `{"review":{"verdict":"changes_requested"}}`)
	writeRunFileFor(t, cfg, srv)

	// Reviewer agents often spell --review-id as --review_id; both must work.
	_, errOut, err := executeCLI(t, aliveDeps(),
		"review", "submit", "mer-1", "--run", "run-1", "--verdict", "changes_requested", "--review_id", "98765")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	var req submitReviewRequest
	if err := json.Unmarshal([]byte(capture.body), &req); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if req.GithubReviewID != "98765" {
		t.Fatalf("githubReviewId = %q, want 98765", req.GithubReviewID)
	}
}

func TestReviewSubmitBatchReadsReviewsFromStdin(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := reviewServer(t, http.StatusOK, `{"reviews":[{"id":"run-1","verdict":"changes_requested"},{"id":"run-2","verdict":"approved"}]}`)
	writeRunFileFor(t, cfg, srv)

	deps := aliveDeps()
	deps.In = strings.NewReader(`{"reviews":[{"runId":"run-1","verdict":"changes_requested","body":"fix auth","githubReviewId":"101"},{"runId":"run-2","verdict":"approved","body":"looks good"}]}`)
	out, errOut, err := executeCLI(t, deps, "review", "submit", "mer-1", "--reviews", "-")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if !strings.Contains(out, "recorded 2 review(s) for mer-1") {
		t.Fatalf("stdout = %q", out)
	}
	var req submitReviewRequest
	if err := json.Unmarshal([]byte(capture.body), &req); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(req.Reviews) != 2 || req.Reviews[0].RunID != "run-1" || req.Reviews[0].GithubReviewID != "101" || req.Reviews[1].Verdict != "approved" {
		t.Fatalf("request = %+v", req)
	}
	if req.RunID != "" || req.Verdict != "" {
		t.Fatalf("batch request should not also set legacy fields: %+v", req)
	}
}

func TestReviewSubmitUsesSessionFlag(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := reviewServer(t, http.StatusOK, `{"review":{"verdict":"approved"}}`)
	writeRunFileFor(t, cfg, srv)

	if _, errOut, err := executeCLI(t, aliveDeps(), "review", "submit", "--session", "mer-7", "--run", "run-7", "--verdict", "approved"); err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if capture.path != "/api/v1/sessions/mer-7/reviews/submit" {
		t.Fatalf("path = %q, want mer-7", capture.path)
	}
}

func TestReviewSubmitMissingVerdictIsUsageError(t *testing.T) {
	setConfigEnv(t)
	_, _, err := executeCLI(t, aliveDeps(), "review", "submit", "mer-1", "--run", "run-1")
	if got := ExitCode(err); got != 2 {
		t.Fatalf("exit code = %d, want 2 (usage); err=%v", got, err)
	}
}

func TestReviewSubmitMissingWorkerIsUsageError(t *testing.T) {
	setConfigEnv(t)
	_, _, err := executeCLI(t, aliveDeps(), "review", "submit", "--run", "run-1", "--verdict", "approved")
	if got := ExitCode(err); got != 2 {
		t.Fatalf("exit code = %d, want 2 (usage); err=%v", got, err)
	}
}

func TestReviewSubmitMissingRunIsUsageError(t *testing.T) {
	setConfigEnv(t)
	_, _, err := executeCLI(t, aliveDeps(), "review", "submit", "mer-1", "--verdict", "approved")
	if got := ExitCode(err); got != 2 {
		t.Fatalf("exit code = %d, want 2 (usage); err=%v", got, err)
	}
}

// ---- review list ----

func TestReviewListFlattensReviewsAcrossSessions(t *testing.T) {
	cfg := setConfigEnv(t)
	sessionsBody := `{"sessions":[{"id":"proj-1"},{"id":"proj-2"}]}`
	reviewsProj1 := `{"reviewerHandleId":"h-1","reviews":[{"prUrl":"https://github.com/o/r/pull/1","prNumber":1,"title":"fix bug","status":"needs_review"}]}`
	reviewsProj2 := `{"reviewerHandleId":"h-2","reviews":[{"prUrl":"https://github.com/o/r/pull/2","prNumber":2,"title":"add feature","status":"up_to_date","latestRun":{"verdict":"approved"}}]}`
	srv, calls := prTestServer(t, map[string]string{
		"GET /api/v1/sessions":                sessionsBody,
		"GET /api/v1/sessions/proj-1/reviews": reviewsProj1,
		"GET /api/v1/sessions/proj-2/reviews": reviewsProj2,
	})
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, aliveDeps(), "review", "list", "myproj", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	var resp struct {
		Reviews []reviewListEntry `json:"reviews"`
	}
	if jsonErr := json.Unmarshal([]byte(out), &resp); jsonErr != nil {
		t.Fatalf("decode: %v\nout=%s", jsonErr, out)
	}
	if len(resp.Reviews) != 2 {
		t.Fatalf("Reviews = %+v, want 2 entries", resp.Reviews)
	}
	if resp.Reviews[1].LatestRun == nil || resp.Reviews[1].LatestRun.Verdict != "approved" {
		t.Errorf("Reviews[1] = %+v, want latestRun.verdict=approved", resp.Reviews[1])
	}
	if !containsCall(*calls, "GET /api/v1/sessions?active=true&project=myproj") ||
		!containsCall(*calls, "GET /api/v1/sessions/proj-1/reviews") ||
		!containsCall(*calls, "GET /api/v1/sessions/proj-2/reviews") {
		t.Errorf("calls = %v", *calls)
	}
}

func TestReviewListNoResults(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, _ := prTestServer(t, map[string]string{"GET /api/v1/sessions": `{"sessions":[]}`})
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, aliveDeps(), "review", "list", "myproj")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if out != "(no reviews for myproj)\n" {
		t.Errorf("out = %q", out)
	}
}

func TestReviewListMissingProjectIsUsageError(t *testing.T) {
	setConfigEnv(t)
	_, _, err := executeCLI(t, aliveDeps(), "review", "list", " ")
	if got := ExitCode(err); got != 2 {
		t.Fatalf("exit code = %d, want 2 (usage); err=%v", got, err)
	}
}

// ---- review execute ----

func TestReviewExecute(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, calls := prTestServer(t, map[string]string{
		"POST /api/v1/sessions/mer-1/reviews/trigger": `{"reviewerHandleId":"h-1","reviews":[{"prUrl":"https://github.com/o/r/pull/1","prNumber":1}]}`,
	})
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, aliveDeps(), "review", "execute", "mer-1")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if out != "triggered review for mer-1 (1 PR(s))\n" {
		t.Errorf("out = %q", out)
	}
	if !containsCall(*calls, "POST /api/v1/sessions/mer-1/reviews/trigger") {
		t.Errorf("calls = %v", *calls)
	}
}

func TestReviewExecuteMissingSessionIsUsageError(t *testing.T) {
	setConfigEnv(t)
	_, _, err := executeCLI(t, aliveDeps(), "review", "execute", " ")
	if got := ExitCode(err); got != 2 {
		t.Fatalf("exit code = %d, want 2 (usage); err=%v", got, err)
	}
}

// ---- review send ----

func TestReviewSend(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := reviewServer(t, http.StatusOK, `{"review":{"verdict":"approved"}}`)
	writeRunFileFor(t, cfg, srv)

	out, errOut, err := executeCLI(t, aliveDeps(), "review", "send", "mer-1", "run-1", "--verdict", "approved")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	if out != "sent approved review for mer-1\n" {
		t.Errorf("out = %q", out)
	}
	if capture.method != http.MethodPost || capture.path != "/api/v1/sessions/mer-1/reviews/submit" {
		t.Fatalf("request = %s %s", capture.method, capture.path)
	}
	var req submitReviewRequest
	if err := json.Unmarshal([]byte(capture.body), &req); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if req.RunID != "run-1" || req.Verdict != "approved" {
		t.Fatalf("request = %+v", req)
	}
}

func TestReviewSendReadsBodyFromStdin(t *testing.T) {
	cfg := setConfigEnv(t)
	srv, capture := reviewServer(t, http.StatusOK, `{"review":{"verdict":"changes_requested"}}`)
	writeRunFileFor(t, cfg, srv)

	deps := aliveDeps()
	deps.In = strings.NewReader("please fix from stdin")
	_, errOut, err := executeCLI(t, deps, "review", "send", "mer-1", "run-1", "--verdict", "changes_requested", "--body", "-")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr=%s", err, errOut)
	}
	var req submitReviewRequest
	if err := json.Unmarshal([]byte(capture.body), &req); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if req.Body != "please fix from stdin" {
		t.Fatalf("body = %q, want the stdin contents", req.Body)
	}
}

func TestReviewSendMissingVerdictIsUsageError(t *testing.T) {
	setConfigEnv(t)
	_, _, err := executeCLI(t, aliveDeps(), "review", "send", "mer-1", "run-1")
	if got := ExitCode(err); got != 2 {
		t.Fatalf("exit code = %d, want 2 (usage); err=%v", got, err)
	}
}

func TestReviewSendMissingArgsIsUsageError(t *testing.T) {
	setConfigEnv(t)
	_, _, err := executeCLI(t, aliveDeps(), "review", "send", "mer-1")
	if got := ExitCode(err); got != 2 {
		t.Fatalf("exit code = %d, want 2 (usage); err=%v", got, err)
	}
}
