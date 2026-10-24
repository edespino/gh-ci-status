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
	b, err := json.Marshal(NewReport([]string{testRepo}, []Row{red, never, partial, gone}, testNow))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		`"generated":"2026-10-24T12:00:00Z"`, `"summary":"3 red, oldest 23 days · 1 unreadable"`, `"repos":["example-org/example-repo"]`,
		`"repo":"example-org/example-repo","workflow":"b.yml","name":"Build","branch":"main","state":"failing"`,
		`"red_since":"2026-10-14T12:00:00Z","age":"10 days","failed_runs":2`,
		`"failed_step":{"label":"Compile (build)","url":"https://github.com/x/job/9"}`,
		`"latest":{"number":20,"url":"https://github.com/example-org/example-repo/actions/runs/120","status":"completed","conclusion":"failure","created_at":"2026-10-20T12:00:00Z"}`,
		`"last_green":{"number":18,`, `"never_green":true`,
		`"history_url":"https://github.com/example-org/example-repo/actions/workflows/gone.yml?query=branch%3Adev"`,
		`"state":"error"`, `"error":"GET /x: 404 Not Found"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("report lacks %s\n%s", want, s)
		}
	}
	if strings.Count(s, `"never_green"`) != 1 || !strings.Contains(s, `"name":"Never","branch":"main","state":"failing","red_since":"2026-10-01T12:00:00Z","age":"23 days","failed_runs":3,"never_green":true`) {
		t.Errorf("never_green should appear only for the never-green row: %s", s)
	}
}

func TestReportEmptyListsAreArrays(t *testing.T) {
	b, _ := json.Marshal(NewReport(nil, nil, testNow))
	if s := string(b); !strings.Contains(s, `"repos":[]`) || !strings.Contains(s, `"rows":[]`) || !strings.Contains(s, `"summary":"nothing watched"`) {
		t.Fatalf("empty report = %s", s)
	}
}

func runPtr(r Run) *Run { return &r }
