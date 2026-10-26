// SPDX-License-Identifier: Apache-2.0

package cistatus

import (
	"context"
	"errors"
	"github.com/edespino/gh-ci-status/internal/fakegithub"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestClientSendsBearerToken(t *testing.T) {
	f := fakegithub.New(t)
	f.Handle("/repos/o/r", 200, `{"default_branch":"main"}`)
	if _, err := testClient(f).DefaultBranch(context.Background(), "o/r"); err != nil {
		t.Fatal(err)
	}
	h := f.Headers()[0]
	if got := h.Get("Authorization"); got != "Bearer tkn" {
		t.Errorf("Authorization = %q", got)
	}
	if got := h.Get("Accept"); got != "application/vnd.github+json" {
		t.Errorf("Accept = %q", got)
	}
}

func TestDefaultBranch(t *testing.T) {
	f := fakegithub.New(t)
	f.Handle("/repos/o/r", 200, `{"default_branch":"trunk"}`)
	got, err := testClient(f).DefaultBranch(context.Background(), "o/r")
	if err != nil || got != "trunk" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestRunsEncodesBranchWithSlash(t *testing.T) {
	f := fakegithub.New(t)
	f.Handle("/repos/o/r/actions/workflows/b.yml/runs?branch=release%2Fx&exclude_pull_requests=true&per_page=100", 200,
		`{"total_count":7,"workflow_runs":[{"id":11,"run_number":3,"head_branch":"release/x","status":"completed","conclusion":null,
		 "created_at":"2026-10-20T12:00:00Z","html_url":"https://github.com/o/r/actions/runs/11","head_repository":{"full_name":"o/r"},
		 "path":".github/workflows/b.yml"}]}`)
	runs, total, err := testClient(f).Runs(context.Background(), "o/r", "b.yml", "release/x")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(f.Requests()[0])
	if u.Path != "/repos/o/r/actions/workflows/b.yml/runs" {
		t.Errorf("path = %q", u.Path)
	}
	q := u.Query()
	if q.Get("branch") != "release/x" || q.Get("per_page") != "100" || q.Get("exclude_pull_requests") != "true" {
		t.Errorf("query = %v", q)
	}
	if u.RawQuery != "branch=release%2Fx&exclude_pull_requests=true&per_page=100" {
		t.Errorf("raw query = %q", u.RawQuery)
	}
	if total != 7 || len(runs) != 1 {
		t.Fatalf("total=%d runs=%d", total, len(runs))
	}
	r := runs[0]
	if r.ID != 11 || r.RunNumber != 3 || r.HeadBranch != "release/x" || r.Conclusion != "" ||
		r.HeadRepository.FullName != "o/r" || r.Path != ".github/workflows/b.yml" ||
		!r.CreatedAt.Equal(time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("run decoded as %+v", r)
	}
}

func TestWorkflowAndJobs(t *testing.T) {
	f := fakegithub.New(t)
	f.Handle("/repos/o/r/actions/workflows/b.yml", 200, `{"id":5,"name":"Build B","path":".github/workflows/b.yml","state":"active"}`)
	f.Handle("/repos/o/r/actions/runs/11/jobs?per_page=100", 200,
		`{"jobs":[{"name":"build","conclusion":"failure","html_url":"https://github.com/o/r/actions/runs/11/job/9",
		  "steps":[{"name":"Checkout","conclusion":"success"},{"name":"Compile","conclusion":"failure"}]}]}`)
	c := testClient(f)
	wf, err := c.Workflow(context.Background(), "o/r", "b.yml")
	if err != nil || wf.Name != "Build B" || wf.State != "active" || wf.ID != 5 {
		t.Fatalf("workflow %+v, %v", wf, err)
	}
	jobs, err := c.Jobs(context.Background(), "o/r", 11)
	if err != nil || len(jobs) != 1 || jobs[0].HTMLURL == "" || len(jobs[0].Steps) != 2 || jobs[0].Steps[1].Conclusion != "failure" {
		t.Fatalf("jobs %+v, %v", jobs, err)
	}
}

func TestWorkflowsFollowsLinkNext(t *testing.T) {
	f := fakegithub.New(t)
	f.HandleWithHeader("/repos/o/r/actions/workflows?per_page=100", 200,
		`{"total_count":3,"workflows":[{"name":"A","path":".github/workflows/a.yml"},{"name":"B","path":".github/workflows/b.yml"}]}`,
		http.Header{"Link": {`<` + f.URL() + `/repos/o/r/actions/workflows?per_page=100&page=2>; rel="next", <` + f.URL() + `/repos/o/r/actions/workflows?per_page=100&page=2>; rel="last"`}})
	f.Handle("/repos/o/r/actions/workflows?per_page=100&page=2", 200,
		`{"total_count":3,"workflows":[{"name":"C","path":".github/workflows/c.yml"}]}`)
	wfs, err := testClient(f).Workflows(context.Background(), "o/r")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, w := range wfs {
		names = append(names, w.Name)
	}
	if strings.Join(names, ",") != "A,B,C" {
		t.Fatalf("names = %v", names)
	}
}

func TestWorkflowsIgnoresNextLinkToOtherHost(t *testing.T) {
	f := fakegithub.New(t)
	f.HandleWithHeader("/repos/o/r/actions/workflows?per_page=100", 200,
		`{"workflows":[{"name":"A","path":".github/workflows/a.yml"}]}`,
		http.Header{"Link": {`<https://evil.example/steal?page=2>; rel="next"`}})
	if _, err := testClient(f).Workflows(context.Background(), "o/r"); err == nil || !strings.Contains(err.Error(), "next page") {
		t.Fatalf("expected refusal to follow a foreign next link, got %v", err)
	}
}

func TestRecentRunsFullPageFetchesNext(t *testing.T) {
	f := fakegithub.New(t)
	full := make([]string, 100)
	for i := range full {
		full[i] = `{"id":` + strconv.Itoa(i+1) + `}`
	}
	f.Handle("/repos/o/r/actions/runs?page=1&per_page=100", 200, `{"workflow_runs":[`+strings.Join(full, ",")+`]}`)
	f.Handle("/repos/o/r/actions/runs?page=2&per_page=100", 200, `{"workflow_runs":[{"id":101}]}`)
	runs, err := testClient(f).RecentRuns(context.Background(), "o/r", 2)
	if err != nil || len(runs) != 101 || runs[100].ID != 101 {
		t.Fatalf("got %d runs, %v", len(runs), err)
	}
}

// A short page is the last one: runs or errors from later pages are ignored.
func TestRecentRunsShortPageEndsList(t *testing.T) {
	f := fakegithub.New(t)
	f.Handle("/repos/o/r/actions/runs?page=1&per_page=100", 200, `{"workflow_runs":[{"id":1},{"id":2}]}`)
	f.Handle("/repos/o/r/actions/runs?page=2&per_page=100", 200, `{"workflow_runs":[{"id":3}]}`)
	f.Handle("/repos/o/r/actions/runs?page=3&per_page=100", 500, `{"message":"boom"}`)
	runs, err := testClient(f).RecentRuns(context.Background(), "o/r", 3)
	if err != nil || len(runs) != 2 {
		t.Fatalf("got %d runs, %v", len(runs), err)
	}
}

// The pages are requested at the same time; each is slow on GitHub.
func TestRecentRunsPagesReadTogether(t *testing.T) {
	f := fakegithub.New(t)
	f.SetDelay(30 * time.Millisecond)
	f.Handle("/repos/o/r/actions/runs?page=1&per_page=100", 200, `{"workflow_runs":[{"id":1}]}`)
	f.Handle("/repos/o/r/actions/runs?page=2&per_page=100", 200, `{"workflow_runs":[]}`)
	if _, err := testClient(f).RecentRuns(context.Background(), "o/r", 2); err != nil {
		t.Fatal(err)
	}
	if f.MaxInFlight() != 2 {
		t.Fatalf("max in-flight requests = %d, want 2", f.MaxInFlight())
	}
}

func TestNotFoundErrorNamesPathNotToken(t *testing.T) {
	f := fakegithub.New(t)
	_, err := testClient(f).Workflow(context.Background(), "o/r", "b.yml")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "404") || !strings.Contains(msg, "b.yml") || strings.Contains(msg, "tkn") {
		t.Fatalf("error = %q", msg)
	}
}

func TestUnauthorizedSaysLogin(t *testing.T) {
	f := fakegithub.New(t)
	f.Handle("/repos/o/r/actions/workflows/b.yml", 401, `{"message":"Bad credentials"}`)
	_, err := testClient(f).Workflow(context.Background(), "o/r", "b.yml")
	if err == nil || !strings.Contains(err.Error(), "gh auth login") {
		t.Fatalf("error = %v", err)
	}
}

func TestTimeoutSaysTimedOut(t *testing.T) {
	f := fakegithub.New(t)
	f.SetDelay(200 * time.Millisecond)
	f.Handle("/repos/o/r/actions/workflows/b.yml", 200, `{"name":"B"}`)
	c := testClient(f)
	c.HTTP.Timeout = 50 * time.Millisecond
	_, err := c.Workflow(context.Background(), "o/r", "b.yml")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error = %v", err)
	}
}

func TestRepositoryAndUser(t *testing.T) {
	f := fakegithub.New(t)
	f.Handle("/repos/O/R", 200, `{"full_name":"o/r","default_branch":"trunk","private":true}`)
	f.Handle("/user", 200, `{"login":"octocat"}`)
	c := testClient(f)
	r, err := c.Repository(context.Background(), "O/R")
	if err != nil || r.FullName != "o/r" || r.DefaultBranch != "trunk" || !r.Private {
		t.Fatalf("repository %+v, %v", r, err)
	}
	if login, err := c.User(context.Background()); err != nil || login != "octocat" {
		t.Fatalf("user %q, %v", login, err)
	}
}

// API errors carry the HTTP status for callers that branch on it.
func TestAPIErrorCarriesStatus(t *testing.T) {
	f := fakegithub.New(t)
	_, err := testClient(f).Repository(context.Background(), "o/missing")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 404 || apiErr.Path != "/repos/o/missing" {
		t.Fatalf("error = %#v", err)
	}
	if err.Error() != "GET /repos/o/missing: 404 Not Found" {
		t.Fatalf("message = %q", err.Error())
	}
}
