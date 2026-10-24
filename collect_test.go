// SPDX-License-Identifier: Apache-2.0

package cistatus

import (
	"context"
	"fmt"
	"github.com/OWNER/gh-ci-status/internal/fakegithub"
	"strings"
	"testing"
	"time"
)

const fr = "o/r" // repo used with the fake API

func runJSON(id, number int, branch, status, conclusion, created string) string {
	c := "null"
	if conclusion != "" {
		c = `"` + conclusion + `"`
	}
	return fmt.Sprintf(`{"id":%d,"run_number":%d,"head_branch":%q,"status":%q,"conclusion":%s,"created_at":%q,"run_started_at":%q,`+
		`"html_url":"https://github.com/o/r/actions/runs/%d","head_repository":{"full_name":"o/r"}}`,
		id, number, branch, status, c, created, created, id)
}

func runsPath(file, branch string) string {
	return "/repos/o/r/actions/workflows/" + file + "/runs?branch=" + strings.ReplaceAll(branch, "/", "%2F") + "&exclude_pull_requests=true&per_page=100"
}

// serveWorkflow registers a workflow name and its runs on one branch.
func serveWorkflow(f *fakegithub.Server, file, name, branch string, runs ...string) {
	f.Handle("/repos/o/r/actions/workflows/"+file, 200, fmt.Sprintf(`{"name":%q,"path":".github/workflows/%s","state":"active"}`, name, file))
	f.Handle(runsPath(file, branch), 200, fmt.Sprintf(`{"total_count":%d,"workflow_runs":[%s]}`, len(runs), strings.Join(runs, ",")))
}

func count(reqs []string, substr string) int {
	n := 0
	for _, r := range reqs {
		if strings.Contains(r, substr) {
			n++
		}
	}
	return n
}

func TestCollectTwoBranchesOfOneWorkflow(t *testing.T) {
	f := fakegithub.New(t)
	serveWorkflow(f, "b.yml", "Build B", "main", runJSON(10, 1, "main", "completed", "success", "2026-10-23T12:00:00Z"))
	f.Handle(runsPath("b.yml", "dev"), 200, `{"total_count":1,"workflow_runs":[`+runJSON(20, 2, "dev", "completed", "failure", "2026-10-24T06:00:00Z")+`]}`)
	f.Handle("/repos/o/r/actions/runs/20/jobs?per_page=100", 200, `{"jobs":[]}`)
	rows := Collect(context.Background(), testClient(f), fr, []Watch{{"b.yml", "main"}, {"b.yml", "dev"}}, testNow)
	if len(rows) != 2 || rows[0].Watch.Branch != "dev" || rows[0].State != Failing || rows[1].State != Passing {
		t.Fatalf("rows = %+v", rows)
	}
	reqs := f.Requests()
	if n := count(reqs, "/actions/workflows/b.yml") - count(reqs, "/runs?"); n != 1 {
		t.Errorf("workflow lookups for b.yml = %d, want 1 (%v)", n, reqs)
	}
	if count(reqs, "runs?branch=main&") != 1 || count(reqs, "runs?branch=dev&") != 1 {
		t.Errorf("runs requests = %v", reqs)
	}
	if rows[0].Name != "Build B" || rows[1].Name != "Build B" {
		t.Errorf("names = %q, %q", rows[0].Name, rows[1].Name)
	}
}

func TestCollectJobsOnlyForRed(t *testing.T) {
	f := fakegithub.New(t)
	for i, file := range []string{"a.yml", "c.yml", "d.yml"} {
		serveWorkflow(f, file, "W"+file, "main", runJSON(100+i, 1, "main", "completed", "success", "2026-10-23T12:00:00Z"))
	}
	serveWorkflow(f, "red.yml", "Build App", "main", runJSON(120, 20, "main", "completed", "failure", "2026-10-20T12:00:00Z"))
	f.Handle("/repos/o/r/actions/runs/120/jobs?per_page=100", 200, `{"jobs":[
		{"name":"setup","conclusion":"success","html_url":"https://github.com/o/r/actions/runs/120/job/901","steps":[]},
		{"name":"Build App","conclusion":"failure","html_url":"https://github.com/o/r/actions/runs/120/job/902",
		 "steps":[{"name":"Checkout","conclusion":"success"},{"name":"Compile","conclusion":"failure"}]}]}`)
	watches := []Watch{{"a.yml", "main"}, {"c.yml", "main"}, {"d.yml", "main"}, {"red.yml", "main"}}
	rows := Collect(context.Background(), testClient(f), fr, watches, testNow)
	if n := count(f.Requests(), "/jobs"); n != 1 {
		t.Fatalf("jobs requests = %d", n)
	}
	want := StepRef{URL: "https://github.com/o/r/actions/runs/120/job/902", Label: "Compile (Build App)"}
	if rows[0].State != Failing || rows[0].FailedStep == nil || *rows[0].FailedStep != want {
		t.Fatalf("red row = %+v", rows[0])
	}
}

func TestCollectFailedJobWithoutFailedStep(t *testing.T) {
	f := fakegithub.New(t)
	serveWorkflow(f, "red.yml", "R", "main", runJSON(130, 3, "main", "completed", "failure", "2026-10-20T12:00:00Z"))
	f.Handle("/repos/o/r/actions/runs/130/jobs?per_page=100", 200,
		`{"jobs":[{"name":"deploy","conclusion":"timed_out","html_url":"https://github.com/o/r/actions/runs/130/job/7","steps":[]}]}`)
	rows := Collect(context.Background(), testClient(f), fr, []Watch{{"red.yml", "main"}}, testNow)
	if rows[0].FailedStep == nil || rows[0].FailedStep.Label != "deploy" {
		t.Fatalf("row = %+v", rows[0])
	}
}

func TestCollectMissingWorkflowIsErrorRow(t *testing.T) {
	f := fakegithub.New(t)
	serveWorkflow(f, "ok.yml", "OK", "main", runJSON(1, 1, "main", "completed", "success", "2026-10-23T12:00:00Z"))
	rows := Collect(context.Background(), testClient(f), fr, []Watch{{"gone.yml", "main"}, {"ok.yml", "main"}}, testNow)
	if len(rows) != 2 || rows[0].State != Error || !strings.Contains(rows[0].Err, "gone.yml") || rows[1].State != Passing {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].Name != "gone.yml" {
		t.Errorf("error row name = %q, want the file", rows[0].Name)
	}
}

func TestCollectUnauthorizedAllRowsSayLogin(t *testing.T) {
	f := fakegithub.New(t)
	f.Fallback(401, `{"message":"Bad credentials"}`)
	rows := Collect(context.Background(), testClient(f), fr, []Watch{{"a.yml", "main"}, {"b.yml", "dev"}}, testNow)
	for _, r := range rows {
		if r.State != Error || !strings.Contains(r.Err, "gh auth login") {
			t.Fatalf("row = %+v", r)
		}
	}
}

func TestCollectBoundedConcurrency(t *testing.T) {
	f := fakegithub.New(t)
	f.SetDelay(30 * time.Millisecond)
	var watches []Watch
	for i := 0; i < 20; i++ {
		file := fmt.Sprintf("w%d.yml", i)
		serveWorkflow(f, file, file, "main", runJSON(i+1, 1, "main", "completed", "success", "2026-10-23T12:00:00Z"))
		watches = append(watches, Watch{file, "main"})
	}
	rows := Collect(context.Background(), testClient(f), fr, watches, testNow)
	if len(rows) != 20 {
		t.Fatalf("rows = %d", len(rows))
	}
	if f.MaxInFlight() > 8 || f.MaxInFlight() < 2 {
		t.Fatalf("max in-flight requests = %d, want 2..8", f.MaxInFlight())
	}
}

func TestReadRowWithoutJobs(t *testing.T) {
	f := fakegithub.New(t)
	serveWorkflow(f, "red.yml", "R", "main", runJSON(140, 4, "main", "completed", "failure", "2026-10-20T12:00:00Z"))
	row := ReadRow(context.Background(), testClient(f), fr, Watch{"red.yml", "main"}, "R", testNow, false)
	if row.State != Failing || row.Name != "R" || row.FailedStep != nil || count(f.Requests(), "/jobs") != 0 {
		t.Fatalf("row %+v requests %v", row, f.Requests())
	}
}
