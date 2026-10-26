// SPDX-License-Identifier: Apache-2.0

package web

import (
	cistatus "github.com/edespino/gh-ci-status"
	"strings"
	"testing"
)

func wfAt(name, path, state string) Workflow { return Workflow{Name: name, Path: path, State: state} }

// recentRun is a run of the workflow at path on branch; event "" means push.
func recentRun(id int64, number int, path, branch, status, conclusion, created, repo, event string) Run {
	r := mkRun(id, number, branch, status, conclusion, created, repo)
	r.Path = path
	if event == "" {
		event = "push"
	}
	r.Event = event
	return r
}

func itemFor(items []ConfigureItem, file string) *ConfigureItem {
	for i := range items {
		if items[i].File == file {
			return &items[i]
		}
	}
	return nil
}

func TestConfigureModelWatching(t *testing.T) {
	wfs := []Workflow{wfAt("Build B", ".github/workflows/b.yml", "active"), wfAt("Alpha", ".github/workflows/a.yml", "active")}
	watched := []Watch{{Workflow: "b.yml", Branch: "main"}, {Workflow: "gone.yml", Branch: "main"}, {Workflow: "b.yml", Branch: "dev"}, {Workflow: "a.yml", Branch: "main"}}
	watching, _ := configureModel(wfs, nil, testRepo, watched, "main")
	if len(watching) != 3 {
		t.Fatalf("watching = %+v", watching)
	}
	b, g, a := watching[0], watching[1], watching[2]
	if b.File != "b.yml" || b.Name != "Build B" || strings.Join(b.Branches, ",") != "main,dev" || b.Missing {
		t.Errorf("b = %+v", b)
	}
	if g.File != "gone.yml" || g.Name != "gone.yml" || !g.Missing {
		t.Errorf("gone = %+v", g)
	}
	if a.Name != "Alpha" {
		t.Errorf("a = %+v", a)
	}
}

func TestConfigureModelItems(t *testing.T) {
	wfs := []Workflow{
		wfAt("CodeQL", "dynamic/github-code-quality/codeql", "active"),
		wfAt("Zed", ".github/workflows/z.yml", "disabled_manually"),
		wfAt("alpha", ".github/workflows/a.yml", "active"),
	}
	// The Add list is about the default branch: a workflow watched only on dev
	// still offers + main.
	_, items := configureModel(wfs, nil, testRepo, []Watch{{Workflow: "z.yml", Branch: "dev"}, {Workflow: "a.yml", Branch: "main"}}, "main")
	if len(items) != 2 || items[0].File != "a.yml" || items[1].File != "z.yml" {
		t.Fatalf("items = %+v", items)
	}
	if items[1].State != "disabled_manually" || items[1].WatchedOnDefault || !items[0].WatchedOnDefault {
		t.Errorf("items = %+v", items)
	}
}

func TestConfigureModelBranchStatuses(t *testing.T) {
	p := ".github/workflows/b.yml"
	recent := []Run{
		recentRun(9, 9, p, "dev", "completed", "failure", "2026-10-24T10:00:00Z", "", ""),
		recentRun(8, 8, p, "dev", "completed", "success", "2026-10-24T09:00:00Z", "", ""),
		recentRun(7, 7, p, "release/x", "in_progress", "", "2026-10-24T08:00:00Z", "", ""),
		recentRun(6, 6, p, "main", "completed", "success", "2026-10-24T11:00:00Z", "", ""),
		recentRun(5, 5, p, "fork-branch", "completed", "failure", "2026-10-24T11:30:00Z", "someone/fork", ""),
		recentRun(4, 4, p, "pr-branch", "completed", "failure", "2026-10-24T11:40:00Z", "", "pull_request"),
		recentRun(3, 3, p, "old", "completed", "cancelled", "2026-10-20T08:00:00Z", "", ""),
		recentRun(2, 2, ".github/workflows/c.yml", "other", "completed", "success", "2026-10-24T07:00:00Z", "", ""),
	}
	_, items := configureModel([]Workflow{wfAt("B", p, "active")}, recent, testRepo, []Watch{{Workflow: "b.yml", Branch: "dev"}}, "main")
	got := items[0].Branches
	want := []BranchStatus{
		{Name: "dev", State: Failing, RunNumber: 9, RunURL: "https://github.com/" + testRepo + "/actions/runs/9", Watched: true},
		{Name: "release/x", State: Running, RunNumber: 7, RunURL: "https://github.com/" + testRepo + "/actions/runs/7"},
		{Name: "old", State: Other, Conclusion: "cancelled", RunNumber: 3, RunURL: "https://github.com/" + testRepo + "/actions/runs/3"},
	}
	if len(got) != len(want) {
		t.Fatalf("branches = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("branch %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestRowStatusText(t *testing.T) {
	failing := Row{Repo: testRepo, Watch: mainWatch, State: Failing, RedSince: ts("2026-10-14T12:00:00Z"),
		Latest: runAt(120, 20, "main", "completed", "failure", "2026-10-20T12:00:00Z")}
	passing := Row{Repo: testRepo, Watch: mainWatch, State: Passing, Latest: runAt(50, 5, "main", "completed", "success", "2026-10-23T12:00:00Z")}
	running := Row{Repo: testRepo, Watch: mainWatch, State: Running, Latest: runAt(41, 9, "main", "in_progress", "", "2026-10-24T11:30:00Z")}
	other := Row{Repo: testRepo, Watch: mainWatch, State: Other, Conclusion: "cancelled", Latest: runAt(5, 5, "main", "completed", "cancelled", "2026-10-24T10:00:00Z")}
	none := Row{Repo: testRepo, Watch: Watch{Workflow: "b.yml", Branch: "dev"}, State: None}
	errRow := Row{Repo: testRepo, Watch: mainWatch, State: Error, Err: "GET /x: 404 Not Found"}
	cases := []struct {
		row       Row
		text, url string
	}{
		{failing, "failing since Oct 14 (#20)", "https://github.com/" + testRepo + "/actions/runs/120"},
		{passing, "passing · Oct 23, 5:00 AM PDT", "https://github.com/" + testRepo + "/actions/runs/50"},
		{running, "running #9", "https://github.com/" + testRepo + "/actions/runs/41"},
		{other, "cancelled #5", "https://github.com/" + testRepo + "/actions/runs/5"},
		{none, "no run on dev", cistatus.HistoryURL(testRepo, "b.yml", "dev")},
		{errRow, "GET /x: 404 Not Found", cistatus.HistoryURL(testRepo, "b.yml", "main")},
	}
	for _, c := range cases {
		got := rowStatus(c.row)
		if got.State != c.row.State || got.Text != c.text || got.URL != c.url {
			t.Errorf("rowStatus(%s) = %+v, want text %q url %q", c.row.State, got, c.text, c.url)
		}
	}
}
