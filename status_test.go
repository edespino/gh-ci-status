// SPDX-License-Identifier: Apache-2.0

package cistatus

import (
	"strconv"
	"testing"
	"time"
)

const testRepo = "example-org/example-repo"

var testNow = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

func ts(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// mkRun builds a run; repo "" means testRepo.
func mkRun(id int64, number int, branch, status, conclusion, created, repo string) Run {
	if repo == "" {
		repo = testRepo
	}
	r := Run{ID: id, RunNumber: number, HeadBranch: branch, Status: status, Conclusion: conclusion,
		CreatedAt: ts(created), RunStartedAt: ts(created), HTMLURL: "https://github.com/" + testRepo + "/actions/runs/" + strconv.FormatInt(id, 10)}
	r.HeadRepository.FullName = repo
	return r
}

var mainWatch = Watch{Workflow: "b.yml", Branch: "main"}

func TestFailingAgeSinceFirstRedAfterGreen(t *testing.T) {
	runs := []Run{
		mkRun(120, 20, "main", "completed", "failure", "2026-10-20T12:00:00Z", ""),
		mkRun(119, 19, "main", "completed", "failure", "2026-10-14T12:00:00Z", ""),
		mkRun(118, 18, "main", "completed", "success", "2026-10-10T12:00:00Z", ""),
	}
	r := Evaluate(testRepo, mainWatch, "Build B", runs, 3, testNow)
	if r.State != Failing || r.Age != "10 days" || r.FailedRuns != 2 || r.LastGreen == nil || r.LastGreen.RunNumber != 18 {
		t.Fatalf("row = %+v", r)
	}
	if !r.RedSince.Equal(ts("2026-10-14T12:00:00Z")) || r.Latest.RunNumber != 20 || r.Name != "Build B" || r.Repo != testRepo {
		t.Fatalf("row = %+v", r)
	}
}

func TestNeverGreenWhenWholeHistoryFetched(t *testing.T) {
	runs := []Run{
		mkRun(3, 3, "main", "completed", "failure", "2026-10-20T12:00:00Z", ""),
		mkRun(2, 2, "main", "completed", "timed_out", "2026-08-13T12:00:00Z", ""),
	}
	r := Evaluate(testRepo, mainWatch, "B", runs, 2, testNow)
	if r.State != Failing || r.LastGreen != nil || !r.AllHistory || r.Age != "72 days" || r.FailedRuns != 2 {
		t.Fatalf("row = %+v", r)
	}
	r = Evaluate(testRepo, mainWatch, "B", runs, 150, testNow)
	if r.AllHistory {
		t.Fatalf("AllHistory with 2 of 150 fetched: %+v", r)
	}
}

func TestRunningKeepsPrevious(t *testing.T) {
	runs := []Run{
		mkRun(41, 9, "main", "in_progress", "", "2026-10-24T11:30:00Z", ""),
		mkRun(40, 8, "main", "completed", "success", "2026-10-23T11:30:00Z", ""),
	}
	r := Evaluate(testRepo, mainWatch, "B", runs, 2, testNow)
	if r.State != Running || r.Latest.RunNumber != 9 || r.Previous == nil || r.Previous.RunNumber != 8 {
		t.Fatalf("row = %+v", r)
	}
}

func TestOtherBranchAndForkIgnored(t *testing.T) {
	runs := []Run{
		mkRun(61, 7, "feat/x", "completed", "failure", "2026-10-24T10:00:00Z", ""),
		mkRun(62, 8, "main", "completed", "failure", "2026-10-24T11:00:00Z", "someone/fork"),
		mkRun(60, 6, "main", "completed", "success", "2026-10-23T10:00:00Z", ""),
	}
	r := Evaluate(testRepo, mainWatch, "B", runs, 3, testNow)
	if r.State != Passing || r.Latest.ID != 60 {
		t.Fatalf("row = %+v", r)
	}
}

func TestDevBranchWatch(t *testing.T) {
	runs := []Run{
		mkRun(71, 3, "dev", "completed", "failure", "2026-10-24T06:00:00Z", ""),
		mkRun(70, 2, "main", "completed", "success", "2026-10-24T07:00:00Z", ""),
	}
	r := Evaluate(testRepo, Watch{Workflow: "b.yml", Branch: "dev"}, "B", runs, 2, testNow)
	if r.State != Failing || r.Latest.ID != 71 || r.Age != "6 hours" {
		t.Fatalf("row = %+v", r)
	}
}

func TestOtherConclusionKeptByName(t *testing.T) {
	runs := []Run{mkRun(5, 5, "main", "completed", "cancelled", "2026-10-24T10:00:00Z", "")}
	r := Evaluate(testRepo, mainWatch, "B", runs, 1, testNow)
	if r.State != Other || r.Conclusion != "cancelled" {
		t.Fatalf("row = %+v", r)
	}
}

func TestNoRunsOnBranch(t *testing.T) {
	r := Evaluate(testRepo, mainWatch, "B", nil, 0, testNow)
	if r.State != None || r.Latest != nil {
		t.Fatalf("row = %+v", r)
	}
}

func TestFormatAge(t *testing.T) {
	cases := map[time.Duration]string{
		71 * 24 * time.Hour: "71 days",
		24 * time.Hour:      "1 day",
		5 * time.Hour:       "5 hours",
		time.Minute:         "1 minute",
		30 * time.Second:    "under a minute",
	}
	for d, want := range cases {
		if got := FormatAge(d); got != want {
			t.Errorf("FormatAge(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestSortRows(t *testing.T) {
	in := []Row{
		{Name: "passing", State: Passing},
		{Name: "young", State: Failing, RedSince: ts("2026-10-22T00:00:00Z")},
		{Name: "running", State: Running},
		{Name: "none", State: None},
		{Name: "old", State: Failing, RedSince: ts("2026-08-13T00:00:00Z")},
		{Name: "error", State: Error},
		{Name: "other", State: Other},
		{Name: "off", State: Disabled},
	}
	want := []string{"old", "young", "error", "running", "other", "none", "passing", "off"}
	got := SortRows(in)
	for i, w := range want {
		if got[i].Name != w {
			t.Fatalf("order = %v", names(got))
		}
	}
}

func names(rows []Row) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.Name)
	}
	return out
}

func TestSummary(t *testing.T) {
	rows := []Row{
		{Name: "Build App", Watch: Watch{Branch: "main"}, State: Failing, Age: "71 days", RedSince: ts("2026-08-14T12:00:00Z")},
		{Name: "Nightly backfill", Watch: Watch{Branch: "main"}, State: Failing, Age: "120 days", RedSince: ts("2026-06-26T12:00:00Z")},
		{Name: "Busy", State: Running},
		{Name: "Gone", State: Error},
		{Name: "Cancelled", State: Other},
		{Name: "Never", State: None},
		{Name: "Lint", State: Passing},
		{Name: "Docs", State: Passing},
		{Name: "Retired", State: Disabled},
	}
	if got := Summary(rows); got != "2 red, oldest 120 days · 1 running · 1 unreadable · 1 other result · 1 with no runs · 2 passing · 1 disabled" {
		t.Fatalf("summary = %q", got)
	}
	green := make([]Row, 7)
	for i := range green {
		green[i].State = Passing
	}
	if got := Summary(green); got != "nothing red · 7 passing" {
		t.Fatalf("summary = %q", got)
	}
	if got := Summary(nil); got != "nothing watched" {
		t.Fatalf("Summary(nil) = %q", got)
	}
}

// Review finding 1: a pull_request run whose head branch is main (a PR from
// main into another branch) tests a merge, not main; it must not decide the
// main row.
func TestPullRequestRunsIgnored(t *testing.T) {
	pr := mkRun(90, 9, "main", "completed", "failure", "2026-10-24T10:00:00Z", "")
	pr.Event = "pull_request"
	prt := mkRun(91, 10, "main", "completed", "failure", "2026-10-24T11:00:00Z", "")
	prt.Event = "pull_request_target"
	push := mkRun(80, 8, "main", "completed", "success", "2026-10-23T10:00:00Z", "")
	push.Event = "push"
	r := Evaluate(testRepo, mainWatch, "B", []Run{pr, prt, push}, 3, testNow)
	if r.State != Passing || r.Latest.ID != 80 {
		t.Fatalf("row = %+v", r)
	}
}

func TestRunState(t *testing.T) {
	for _, c := range []struct {
		status, conclusion string
		want               State
	}{
		{"in_progress", "", Running}, {"queued", "", Running},
		{"completed", "success", Passing},
		{"completed", "failure", Failing}, {"completed", "timed_out", Failing}, {"completed", "startup_failure", Failing},
		{"completed", "cancelled", Other}, {"completed", "skipped", Other},
	} {
		if got := RunState(Run{Status: c.status, Conclusion: c.conclusion}); got != c.want {
			t.Errorf("RunState(%s, %s) = %s, want %s", c.status, c.conclusion, got, c.want)
		}
	}
}

func TestHistoryURL(t *testing.T) {
	got := HistoryURL("o/r", "b.yml", "release/x")
	if got != "https://github.com/o/r/actions/workflows/b.yml?query=branch%3Arelease%2Fx" {
		t.Fatalf("got %q", got)
	}
}

// GitHub treats repository names case-insensitively; a run whose head
// repository differs only in case is this repository's run.
func TestRepoNameCaseIgnored(t *testing.T) {
	runs := []Run{mkRun(70, 2, "main", "completed", "failure", "2026-10-24T07:00:00Z", "Example-Org/Example-Repo")}
	if r := Evaluate(testRepo, mainWatch, "B", runs, 1, testNow); r.State != Failing {
		t.Fatalf("row = %+v", r)
	}
}
