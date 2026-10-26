// SPDX-License-Identifier: Apache-2.0

package cistatus

import (
	"encoding/json"
	"strings"
	"testing"
)

// The JSON report's field names are a contract for scripts and MCP tools.
func TestReportJSON(t *testing.T) {
	red := Row{
		Repo: testRepo, Watch: Watch{"b.yml", "main"}, Name: "Build", State: Failing,
		Latest:    runPtr(mkRun(120, 20, "main", "completed", "failure", "2026-10-20T12:00:00Z", "")),
		LastGreen: runPtr(mkRun(118, 18, "main", "completed", "success", "2026-10-10T12:00:00Z", "")),
		RedSince:  ts("2026-10-14T12:00:00Z"), Age: "10 days", FailedRuns: 2,
		FailedStep: &StepRef{URL: "https://github.com/x/job/9", Label: "Compile (build)"},
	}
	never := Row{Repo: testRepo, Watch: Watch{"n.yml", "main"}, Name: "Never", State: Failing, AllHistory: true,
		Latest: runPtr(mkRun(5, 5, "main", "completed", "failure", "2026-10-20T12:00:00Z", "")), RedSince: ts("2026-10-01T12:00:00Z"), Age: "23 days", FailedRuns: 3}
	// No green in the runs read, but not every run was read: not "never green".
	partial := Row{Repo: testRepo, Watch: Watch{"p.yml", "main"}, Name: "Partial", State: Failing, AllHistory: false,
		Latest: runPtr(mkRun(6, 6, "main", "completed", "failure", "2026-10-20T12:00:00Z", "")), RedSince: ts("2026-10-02T12:00:00Z"), Age: "22 days", FailedRuns: 100}
	gone := Row{Repo: testRepo, Watch: Watch{"gone.yml", "dev"}, Name: "gone.yml", State: Error, Err: "GET /x: 404 Not Found"}
	off := Row{Repo: testRepo, Watch: Watch{"old.yml", "main"}, Name: "Old", State: Disabled, WorkflowState: "disabled_manually",
		Latest: runPtr(mkRun(150, 9, "main", "completed", "failure", "2026-08-01T12:00:00Z", ""))}
	// Red, with a new run in progress: still failing, the run in progress beside it.
	rerun := Row{Repo: testRepo, Watch: Watch{"r.yml", "main"}, Name: "Rerun", State: Failing,
		Latest:     runPtr(mkRun(7, 7, "main", "completed", "failure", "2026-10-22T12:00:00Z", "")),
		InProgress: runPtr(mkRun(8, 8, "main", "in_progress", "", "2026-10-24T11:00:00Z", "")),
		RedSince:   ts("2026-10-22T12:00:00Z"), Age: "2 days", FailedRuns: 1}
	// No run finished yet: running.
	firstRun := runPtr(mkRun(1, 1, "main", "queued", "", "2026-10-24T11:50:00Z", ""))
	first := Row{Repo: testRepo, Watch: Watch{"f.yml", "main"}, Name: "First", State: Running, Latest: firstRun, InProgress: firstRun}
	b, err := json.Marshal(NewReport([]string{testRepo}, []Row{red, never, partial, gone, off, rerun, first}, testNow))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		`"generated":"2026-10-24T12:00:00Z"`, `"summary":"4 failing, oldest 23 days · 2 running · 1 unreadable · 1 disabled"`, `"repos":["example-org/example-repo"]`,
		`"repo":"example-org/example-repo","workflow":"b.yml","name":"Build","branch":"main","state":"failing"`,
		`"red_since":"2026-10-14T12:00:00Z","age":"10 days","failed_runs":2`,
		`"failed_step":{"label":"Compile (build)","url":"https://github.com/x/job/9"}`,
		`"latest":{"number":20,"url":"https://github.com/example-org/example-repo/actions/runs/120","status":"completed","conclusion":"failure","created_at":"2026-10-20T12:00:00Z"}`,
		`"last_green":{"number":18,`, `"never_green":true`,
		`"history_url":"https://github.com/example-org/example-repo/actions/workflows/gone.yml?query=branch%3Adev"`,
		`"state":"error"`, `"error":"GET /x: 404 Not Found"`,
		`"name":"Old","branch":"main","state":"disabled","workflow_state":"disabled_manually","latest":{"number":9,`,
		`"name":"Rerun","branch":"main","state":"failing"`,
		`"latest":{"number":7,"url":"https://github.com/example-org/example-repo/actions/runs/7","status":"completed","conclusion":"failure","created_at":"2026-10-22T12:00:00Z"},` +
			`"in_progress":{"number":8,"url":"https://github.com/example-org/example-repo/actions/runs/8","status":"in_progress","created_at":"2026-10-24T11:00:00Z"},` +
			`"previous":{"number":7,`,
		`"name":"First","branch":"main","state":"running","latest":{"number":1,`,
		`"in_progress":{"number":1,"url":"https://github.com/example-org/example-repo/actions/runs/1","status":"queued",`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("report lacks %s\n%s", want, s)
		}
	}
	if strings.Count(s, `"previous"`) != 1 || strings.Count(s, `"in_progress":{`) != 2 {
		t.Errorf("previous only beside a run in progress after a finished one; in_progress only with a run in progress: %s", s)
	}
	if strings.Count(s, `"never_green"`) != 1 || !strings.Contains(s, `"name":"Never","branch":"main","state":"failing","red_since":"2026-10-01T12:00:00Z","age":"23 days","failed_runs":3,"never_green":true`) {
		t.Errorf("never_green should appear only for the never-green row: %s", s)
	}
}

// The report names the version that wrote it when the caller sets it.
func TestReportVersion(t *testing.T) {
	rep := NewReport(nil, nil, testNow)
	b, _ := json.Marshal(rep)
	if strings.Contains(string(b), `"version"`) {
		t.Fatalf("version without a value: %s", b)
	}
	rep.Version = "v0.1.0"
	b, _ = json.Marshal(rep)
	if !strings.Contains(string(b), `"generated":"2026-10-24T12:00:00Z","version":"v0.1.0"`) {
		t.Fatalf("report = %s", b)
	}
}

func TestReportEmptyListsAreArrays(t *testing.T) {
	b, _ := json.Marshal(NewReport(nil, nil, testNow))
	if s := string(b); !strings.Contains(s, `"repos":[]`) || !strings.Contains(s, `"rows":[]`) || !strings.Contains(s, `"summary":"nothing watched"`) {
		t.Fatalf("empty report = %s", s)
	}
}

func runPtr(r Run) *Run { return &r }
