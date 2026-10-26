// SPDX-License-Identifier: Apache-2.0

package web

import (
	"bytes"
	"fmt"
	cistatus "github.com/edespino/gh-ci-status"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestMain pins the time zone for every test before any test or server
// goroutine starts. Changing time.Local inside a test races with goroutines of
// servers from earlier tests that still read the clock while closing.
func TestMain(m *testing.M) {
	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	time.Local = la
	os.Exit(m.Run())
}

func statusHTML(t *testing.T, rows ...Row) string {
	t.Helper()
	var b bytes.Buffer
	p := StatusPage{Repos: []string{testRepo}, Summary: cistatus.Summary(rows), Generated: "Oct 24, 5:00 AM PDT", ReadAt: "5:00 AM", Rows: rows}
	if err := renderStatus(&b, p); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func redRow() Row {
	return Row{
		Repo: testRepo, Watch: Watch{Workflow: "b.yml", Branch: "release/x"}, Name: "Build B", State: Failing,
		Latest:    runAt(120, 20, "release/x", "completed", "failure", "2026-10-20T12:00:00Z"),
		LastGreen: runAt(118, 18, "release/x", "completed", "success", "2026-10-10T12:00:00Z"),
		RedSince:  ts("2026-10-14T12:00:00Z"), Age: "10 days", FailedRuns: 2,
		FailedStep: &StepRef{URL: "https://github.com/" + testRepo + "/actions/runs/120/job/902", Label: "Phase 310: Build database (Build B)"},
	}
}

func TestStatusLinks(t *testing.T) {
	html := statusHTML(t, redRow())
	for _, want := range []string{
		`href="https://github.com/` + testRepo + `/actions/workflows/b.yml?query=branch%3Arelease%2Fx"`,
		`href="https://github.com/` + testRepo + `/actions/runs/120"`,
		`href="https://github.com/` + testRepo + `/actions/runs/120/job/902"`,
		`href="https://github.com/` + testRepo + `/actions/runs/118"`,
		"failing for 10 days", "release/x", "Phase 310: Build database (Build B)", "2 failed runs since the last passing run <a href=",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("page lacks %s", want)
		}
	}
}

func TestStatusEscapes(t *testing.T) {
	r := redRow()
	r.Name = "<b>Odd</b> & Co"
	html := statusHTML(t, r)
	if strings.Contains(html, "<b>Odd</b>") || !strings.Contains(html, "&lt;b&gt;Odd&lt;/b&gt; &amp; Co") {
		t.Fatal("name not escaped")
	}
}

func TestRunningAndNeverGreenText(t *testing.T) {
	first := runAt(41, 9, "main", "in_progress", "", "2026-10-24T11:30:00Z")
	running := Row{Repo: testRepo, Watch: mainWatch, Name: "Busy", State: Running, Latest: first, InProgress: first}
	never := Row{Repo: testRepo, Watch: mainWatch, Name: "Never", State: Failing, Age: "72 days", FailedRuns: 12, AllHistory: true,
		Latest: runAt(3, 3, "main", "completed", "failure", "2026-10-20T12:00:00Z")}
	partial := never
	partial.Name, partial.AllHistory, partial.FailedRuns = "Partial", false, 100
	html := statusHTML(t, never, partial, running)
	for _, want := range []string{`<span class="spinner" aria-hidden="true"></span>running #9</a>`, "started Oct 24, 4:30 AM PDT", "never passed on main", "12 failed runs", "no passing run in the last 100 main runs"} {
		if !strings.Contains(html, want) {
			t.Errorf("page lacks %q", want)
		}
	}
}

// A row with a run in progress keeps its result and names the run in
// progress beside it, linked.
func TestRunInProgressText(t *testing.T) {
	rerun := redRow()
	rerun.InProgress = runAt(121, 21, "release/x", "in_progress", "", "2026-10-24T11:00:00Z")
	green := Row{Repo: testRepo, Watch: mainWatch, Name: "Green", State: Passing,
		Latest:     runAt(50, 5, "main", "completed", "success", "2026-10-23T12:00:00Z"),
		InProgress: runAt(51, 6, "main", "queued", "", "2026-10-24T11:10:00Z")}
	html := statusHTML(t, rerun, green)
	for _, want := range []string{
		`>failing #20</a>`, `<a href="https://github.com/` + testRepo + `/actions/runs/121">running #21</a>, started Oct 24, 4:00 AM PDT`,
		`>passing #5</a>`, `<a href="https://github.com/` + testRepo + `/actions/runs/51">waiting #6</a> (queued) since Oct 24, 4:10 AM PDT`,
		`data-state="passing" data-running="1" data-waiting="1" data-name="Green `,
		`<span class="spinner" aria-hidden="true"></span><a href="https://github.com/` + testRepo + `/actions/runs/121">running #21</a>`,
		`<svg class="clock" aria-hidden="true"`, `</svg><a href="https://github.com/` + testRepo + `/actions/runs/51">waiting #6</a>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("page lacks %s", want)
		}
	}
	if strings.Contains(statusHTML(t, redRow()), "running #") {
		t.Error("a row without a run in progress names one")
	}
	if strings.Count(html, "data-waiting=") != 1 {
		t.Error("data-waiting only on a row whose run has not started")
	}
}

func TestOtherNoneErrorText(t *testing.T) {
	html := statusHTML(t,
		Row{Repo: testRepo, Watch: mainWatch, Name: "Err", State: Error, Err: "GET /x: 404 Not Found"},
		Row{Repo: testRepo, Watch: mainWatch, Name: "Cancel", State: Other, Conclusion: "cancelled", Latest: runAt(5, 5, "main", "completed", "cancelled", "2026-10-24T10:00:00Z")},
		Row{Repo: testRepo, Watch: Watch{Workflow: "n.yml", Branch: "dev"}, Name: "Nothing", State: None},
	)
	for _, want := range []string{"GET /x: 404 Not Found", "cancelled #5", "no run on dev yet"} {
		if !strings.Contains(html, want) {
			t.Errorf("page lacks %q", want)
		}
	}
}

// The header names the repository and when GitHub was read (beside Refresh,
// the full time on hover); the counts are on the filter buttons, not here.
func TestStatusHeaderAndActions(t *testing.T) {
	html := statusHTML(t, redRow())
	header := regexp.MustCompile(`(?s)<header>.*?</header>`).FindString(html)
	for _, want := range []string{`<p class="sub">` + testRepo + `</p>`, `<span class="read" title="read from GitHub Oct 24, 5:00 AM PDT">read 5:00 AM</span>`,
		`action="/refresh?key=`, `method="post"`, `href="/configure?key=`} {
		if !strings.Contains(header, want) {
			t.Errorf("header lacks %q:\n%s", want, header)
		}
	}
	if strings.Contains(header, "1 failing") || strings.Contains(header, "generated") {
		t.Errorf("the summary is still in the header:\n%s", header)
	}
	if strings.Index(header, `class="read"`) > strings.Index(header, `action="/refresh`) {
		t.Error("the time should come before Refresh")
	}
}

func TestPagesSelfContained(t *testing.T) {
	var cfg bytes.Buffer
	if err := renderConfigure(&cfg, sampleConfigurePage()); err != nil {
		t.Fatal(err)
	}
	for _, page := range []string{statusHTML(t, redRow()), cfg.String()} {
		for _, bad := range []string{"<script src=", `<link rel="stylesheet"`, "@import"} {
			if strings.Contains(page, bad) {
				t.Errorf("page contains %q", bad)
			}
		}
	}
}

func TestLocalTime(t *testing.T) {
	if got := localTime(ts("2026-10-23T12:00:00Z")); got != "Oct 23, 5:00 AM PDT" {
		t.Fatalf("localTime = %q", got)
	}
	if got := localTime(ts("2026-01-15T20:05:00Z")); got != "Jan 15, 12:05 PM PST" {
		t.Fatalf("localTime = %q", got)
	}
}

// Phase 2 (Task 10): Configure page with a Watching list and an Add list.

func sampleConfigurePage() ConfigurePage {
	return ConfigurePage{Repo: testRepo, Repos: []string{testRepo}, DefaultBranch: "main", Key: testKey, AnyWatched: true,
		Watching: []WatchItem{
			{File: "b.yml", Name: "Build B", Branches: []string{"main", "release/x"}},
			{File: "gone.yml", Name: "gone.yml", Missing: true, Branches: []string{"main"}},
		},
		Items: []ConfigureItem{
			{File: "b.yml", Name: "Build B", State: "active", WatchedOnDefault: true, Branches: []BranchStatus{
				{Name: "release/x", State: Passing, RunNumber: 4, RunURL: "https://github.com/o/r/actions/runs/4", Watched: true},
				{Name: "dev", State: Failing, RunNumber: 5, RunURL: "https://github.com/o/r/actions/runs/5"},
			}},
			{File: "c.yml", Name: "<C> & co", State: "disabled_manually"},
		},
	}
}

func configureHTML(t *testing.T) string {
	t.Helper()
	var b bytes.Buffer
	if err := renderConfigure(&b, sampleConfigurePage()); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestConfigurePageWatching(t *testing.T) {
	html := configureHTML(t)
	for _, want := range []string{
		`data-workflow="b.yml" data-branch="main"`, `data-workflow="b.yml" data-branch="release/x"`,
		`action="/unwatch?key=` + testKey + `"`, `name="workflow" value="b.yml"`, `name="branch" value="release/x"`,
		"not in repo", `href="/?key=` + testKey + `"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("page lacks %s", want)
		}
	}
	if n := strings.Count(html, `class="watch-item`); n != 2 {
		t.Errorf("watch items = %d", n)
	}
}

func TestConfigurePageAddList(t *testing.T) {
	html := configureHTML(t)
	for _, want := range []string{
		`data-filter="all"`, `data-filter="failing"`, `data-filter="unwatched"`, `type="search"`,
		`action="/watch?key=` + testKey + `"`, "+ main", "<details", "release/x", "dev",
		`href="https://github.com/o/r/actions/runs/5"`, `placeholder="another branch"`, "disabled_manually",
		"&lt;C&gt; &amp; co",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("page lacks %s", want)
		}
	}
	if n := strings.Count(html, `class="add-item`); n != 2 {
		t.Errorf("add items = %d", n)
	}
	if strings.Contains(html, "<C> & co") {
		t.Error("unescaped workflow name")
	}
}

func TestConfigurePageWatchFailingForm(t *testing.T) {
	html := configureHTML(t)
	if !strings.Contains(html, `action="/watch-many?key=`+testKey+`"`) || !strings.Contains(html, `id="watch-failing"`) {
		t.Fatal("no watch-failing form")
	}
	if !strings.Contains(html, `"/api/row?key=`) && !strings.Contains(html, "/api/row") {
		t.Fatal("page script does not load row status")
	}
}

// Busy feedback: every same-site action that waits on GitHub shows an overlay.
func TestPagesHaveBusyOverlay(t *testing.T) {
	status := statusHTML(t, redRow())
	cfg := configureHTML(t)
	for name, page := range map[string]string{"status": status, "configure": cfg} {
		for _, want := range []string{`id="busy"`, `id="busy-text"`, `addEventListener("submit"`, `a[data-busy]`, "pageshow"} {
			if !strings.Contains(page, want) {
				t.Errorf("%s page lacks %s", name, want)
			}
		}
	}
	for _, want := range []string{`action="/refresh?key=`, `href="/configure?key=`} {
		re := regexp.MustCompile(`<(form|a)[^>]*` + regexp.QuoteMeta(want) + `[^>]*>`)
		tag := re.FindString(status)
		if !strings.Contains(tag, "data-busy=") {
			t.Errorf("status page element %q has no data-busy", tag)
		}
	}
	for _, want := range []string{`href="/?key=`, `action="/watch-many?key=`, `action="/unwatch?key=`, `action="/watch?key=`} {
		for _, tag := range regexp.MustCompile(`<(form|a)[^>]*`+regexp.QuoteMeta(want)+`[^>]*>`).FindAllString(cfg, -1) {
			if !strings.Contains(tag, "data-busy=") {
				t.Errorf("configure page element %q has no data-busy", tag)
			}
		}
	}
	if strings.Contains(status, `data-busy`) && regexp.MustCompile(`<a[^>]*href="https://github\.com[^>]*data-busy`).MatchString(status) {
		t.Error("links to GitHub must not show the overlay")
	}
}

func TestConfigurePageHasProgress(t *testing.T) {
	if !strings.Contains(configureHTML(t), `id="progress"`) {
		t.Fatal("configure page lacks the status progress counter")
	}
}

// Removing: a Remove button per watched workflow, plus Remove all, which
// asks before it changes anything.
func TestConfigurePageRemoveControls(t *testing.T) {
	html := configureHTML(t)
	for _, file := range []string{"b.yml", "gone.yml"} {
		re := regexp.MustCompile(`<form[^>]*action="/unwatch-workflow\?key=` + testKey + `"[^>]*data-busy=[^>]*><input type="hidden" name="repo" value="` + testRepo + `"><input type="hidden" name="workflow" value="` + regexp.QuoteMeta(file) + `"><button type="submit"[^>]*>Remove</button>`)
		if !re.MatchString(html) {
			t.Errorf("no Remove for %s", file)
		}
	}
	for action, ask := range map[string]string{
		"/unwatch-all": "Stop watching 2 workflows in example-org/example-repo?",
	} {
		tag := regexp.MustCompile(`<form[^>]*action="` + action + `\?key=` + testKey + `"[^>]*>`).FindString(html)
		if !strings.Contains(tag, `data-confirm="`+ask+`"`) || !strings.Contains(tag, "data-busy=") {
			t.Errorf("%s form = %q", action, tag)
		}
	}
	p := sampleConfigurePage()
	p.Watching = nil
	var b bytes.Buffer
	if err := renderConfigure(&b, p); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "/unwatch-all") || strings.Contains(b.String(), "/restore-defaults") {
		t.Error("empty list: want no Remove all, and there are no defaults to restore")
	}
}

func TestStatusNothingWatched(t *testing.T) {
	html := statusHTML(t)
	if !regexp.MustCompile(`Nothing watched\. <a href="/configure\?key=[^"]*"[^>]*>Choose workflows</a> to watch\.`).MatchString(html) {
		t.Fatalf("no empty state: %s", html)
	}
	var b bytes.Buffer
	if err := renderStatus(&b, StatusPage{Repos: []string{testRepo}, Summary: "cannot read GitHub", Error: "boom"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "Nothing watched") {
		t.Error("error page must not claim nothing is watched")
	}
}

// Status, back to the status page, is a button in the header, shown once
// something is watched.
func TestWorkflowsStatusButton(t *testing.T) {
	tag := regexp.MustCompile(`<header>(?s:.*?)(<a [^>]*>Status</a>)(?s:.*?)</header>`).FindStringSubmatch(configureHTML(t))
	if tag == nil || !strings.Contains(tag[1], `class="done"`) || !strings.Contains(tag[1], `href="/?key=`+testKey+`"`) || !strings.Contains(tag[1], "data-busy=") {
		t.Fatalf("Status in header = %v", tag)
	}
	p := sampleConfigurePage()
	p.AnyWatched = false
	var b bytes.Buffer
	if err := renderConfigure(&b, p); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), `class="done"`) {
		t.Error("Status offered with nothing watched")
	}
}

// The status page's header has Workflows (a checklist icon) and Settings
// (the gear), as buttons with their labels.
func TestStatusWorkflowsAndSettingsButtons(t *testing.T) {
	html := statusHTML(t, redRow())
	var gear, checklist bytes.Buffer
	if err := templates.ExecuteTemplate(&gear, "icon-gear", nil); err != nil {
		t.Fatal(err)
	}
	if err := templates.ExecuteTemplate(&checklist, "icon-checklist", nil); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ href, icon, label string }{
		{`/configure\?key=`, checklist.String(), "Workflows</a>"},
		{`/setup\?key=[0-9a-f]*&amp;from=status"`, gear.String(), "Settings</a>"},
	} {
		tag := regexp.MustCompile(`<a [^>]*href="` + c.href + `[^>]*>(?s:.*?)</a>`).FindString(html)
		for _, want := range []string{`class="btn"`, "data-busy=", c.icon, c.label} {
			if !strings.Contains(tag, want) {
				t.Errorf("link %q lacks %s", tag, want)
			}
		}
	}
	if strings.Contains(html, ">Configure<") || strings.Contains(html, ">Setup<") {
		t.Error("the old names are still shown")
	}
}

// Workflows reaches Settings as the status page does: a gear button in the
// header, which tells Settings to come back to this repository's Workflows.
func TestWorkflowsSettingsButton(t *testing.T) {
	html := configureHTML(t)
	var gear bytes.Buffer
	if err := templates.ExecuteTemplate(&gear, "icon-gear", nil); err != nil {
		t.Fatal(err)
	}
	tag := regexp.MustCompile(`<header>(?s:.*?)(<a [^>]*href="/setup[^"]*"[^>]*>(?s:.*?)</a>)(?s:.*?)</header>`).FindStringSubmatch(html)
	if tag == nil {
		t.Fatal("no Settings button in the Workflows header")
	}
	for _, want := range []string{`class="btn"`, `href="/setup?key=` + testKey + `&amp;from=workflows&amp;from_repo=example-org%2fexample-repo"`, "data-busy=", gear.String() + "Settings</a>"} {
		if !strings.Contains(tag[1], want) {
			t.Errorf("Settings button %q lacks %s", tag[1], want)
		}
	}
	if strings.Contains(html, "+ Add repository") {
		t.Error("Workflows still reaches Settings through + Add repository")
	}
}

// Both Refresh buttons carry the same refresh icon before their label.
func TestRefreshButtonsHaveIcon(t *testing.T) {
	p := sampleConfigurePage()
	p.ReadAt = "5:00 AM"
	var cfg bytes.Buffer
	if err := renderConfigure(&cfg, p); err != nil {
		t.Fatal(err)
	}
	button := regexp.MustCompile(`(?s)<form[^>]*action="/(?:configure/)?refresh\?key=[^"]*"[^>]*>\s*(?:<input type="hidden"[^>]*>)?<button[^>]*>(<svg aria-hidden="true".*?</svg>)Refresh</button>`)
	s := button.FindStringSubmatch(statusHTML(t, redRow()))
	c := button.FindStringSubmatch(cfg.String())
	if s == nil || c == nil {
		t.Fatalf("Refresh without icon: status %v, configure %v", s != nil, c != nil)
	}
	if s[1] != c[1] {
		t.Errorf("the two Refresh icons differ:\n%s\n%s", s[1], c[1])
	}
}

// A workflow turned off on GitHub is a greyed row that says so, with its last
// run, and is not red.
func TestDisabledRow(t *testing.T) {
	off := Row{Repo: testRepo, Watch: Watch{Workflow: "old.yml", Branch: "main"}, Name: "Old", State: cistatus.Disabled,
		WorkflowState: "disabled_manually", Latest: runAt(150, 9, "main", "completed", "failure", "2026-08-01T12:00:00Z")}
	html := statusHTML(t, off)
	for _, want := range []string{`<div class="row off"`, ">disabled<", "turned off on GitHub (disabled_manually)",
		"last run: failing", `href="https://github.com/` + testRepo + `/actions/runs/150"`} {
		if !strings.Contains(html, want) {
			t.Errorf("page lacks %s", want)
		}
	}
	if !regexp.MustCompile(`\.row\.off\{[^}]*opacity`).MatchString(html) {
		t.Error("disabled rows are not greyed")
	}
	if strings.Contains(html, `class="age"`) || strings.Contains(html, `class="row bad"`) {
		t.Error("a disabled row must not look red")
	}
}

// "Watch N shown" posts to the same route as "Watch all failing", carries the
// repository, asks before saving, and starts disabled until the script fills it.
func TestConfigurePageWatchShownForm(t *testing.T) {
	tag := regexp.MustCompile(`(?s)<form[^>]*id="watch-shown"[^>]*>.*?</form>`).FindString(configureHTML(t))
	for _, want := range []string{`action="/watch-many?key=` + testKey + `"`, "data-busy=", "data-confirm=",
		`<input type="hidden" name="repo" value="` + testRepo + `">`, `id="shown-inputs"`, "disabled"} {
		if !strings.Contains(tag, want) {
			t.Errorf("watch-shown form lacks %s:\n%s", want, tag)
		}
	}
}

// The status page's filter: a box and one button per state present, with
// counts; rows carry what the filter matches on.
func TestStatusFilterBar(t *testing.T) {
	green := Row{Repo: testRepo, Watch: mainWatch, Name: "Green", State: Passing, Latest: runAt(50, 5, "main", "completed", "success", "2026-10-23T12:00:00Z")}
	off := Row{Repo: testRepo, Watch: Watch{Workflow: "old.yml", Branch: "main"}, Name: "Old", State: Disabled, WorkflowState: "disabled_manually"}
	html := statusHTML(t, redRow(), green, off)
	for _, want := range []string{`id="filter"`, `data-filter="all" class="on">All 3<`, `data-filter="failing">Failing 1<`,
		`data-filter="passing">Passing 1<`, `data-filter="disabled">Disabled 1<`, `id="no-match" hidden`,
		`data-state="failing" data-name="Build B b.yml release/x ` + testRepo + `"`, `[hidden]{display:none!important}`} {
		if !strings.Contains(html, want) {
			t.Errorf("status page lacks %s", want)
		}
	}
	if strings.Contains(html, `data-filter="running"`) || strings.Contains(html, `data-filter="none"`) {
		t.Error("buttons only for states present")
	}
	if empty := statusHTML(t); strings.Contains(empty, `id="filter"`) {
		t.Error("no filter when nothing is watched")
	}
	// Running counts every row with a run in progress, whatever its result.
	rerun := redRow()
	rerun.InProgress = runAt(121, 21, "release/x", "in_progress", "", "2026-10-24T11:00:00Z")
	busy := green
	busy.InProgress = runAt(51, 6, "main", "queued", "", "2026-10-24T11:10:00Z")
	first := runAt(60, 1, "main", "queued", "", "2026-10-24T11:20:00Z")
	never := Row{Repo: testRepo, Watch: Watch{Workflow: "new.yml", Branch: "main"}, Name: "New", State: Running, Latest: first, InProgress: first}
	html = statusHTML(t, rerun, busy, green, never)
	for _, want := range []string{`data-filter="failing">Failing 1<`, `data-filter="running">Running 3<`, `data-filter="passing">Passing 2<`,
		`data-state="failing" data-running="1" data-name="Build B`} {
		if !strings.Contains(html, want) {
			t.Errorf("status page lacks %s", want)
		}
	}
	if strings.Count(html, `data-running=`) != 3 {
		t.Error("data-running only on rows with a run in progress")
	}
}

// A workflow already watched on the default branch shows a disabled
// "watching main" instead of + main; others keep + main.
func TestConfigureWatchedOnMainHasNoAddButton(t *testing.T) {
	html := configureHTML(t)
	item := func(file string) string {
		return regexp.MustCompile(`(?s)<div class="add-item" data-file="` + regexp.QuoteMeta(file) + `".*?<details>`).FindString(html)
	}
	b, c := item("b.yml"), item("c.yml")
	if !regexp.MustCompile(`<button type="button" disabled[^>]*>watching main</button>`).MatchString(b) || strings.Contains(b, `action="/watch?key=`) {
		t.Errorf("b.yml (watched on main):\n%s", b)
	}
	if !strings.Contains(c, ">+ main</button>") || !strings.Contains(c, `action="/watch?key=`) {
		t.Errorf("c.yml (not watched):\n%s", c)
	}
}

// Every page ends with the version and links to the project.
func TestPagesHaveFooter(t *testing.T) {
	var cfg, setup, status bytes.Buffer
	cp := sampleConfigurePage()
	cp.Version = "v9.9.9"
	if err := renderConfigure(&cfg, cp); err != nil {
		t.Fatal(err)
	}
	if err := renderSetup(&setup, SetupPage{Key: testKey, Version: "v9.9.9"}); err != nil {
		t.Fatal(err)
	}
	if err := renderStatus(&status, StatusPage{Repos: []string{testRepo}, Summary: "nothing watched", Version: "v9.9.9"}); err != nil {
		t.Fatal(err)
	}
	for name, page := range map[string]string{"configure": cfg.String(), "setup": setup.String(), "status": status.String()} {
		footer := regexp.MustCompile(`(?s)<footer>.*?</footer>`).FindString(page)
		for _, want := range []string{"gh-ci-status v9.9.9", `href="https://github.com/edespino/gh-ci-status/tree/v9.9.9">source</a>`, `href="https://github.com/edespino/gh-ci-status/issues"`} {
			if !strings.Contains(footer, want) {
				t.Errorf("%s footer lacks %s: %q", name, want, footer)
			}
		}
	}
	if footer := regexp.MustCompile(`(?s)<footer>.*?</footer>`).FindString(statusHTML(t)); !strings.Contains(footer, "gh-ci-status ·") ||
		!strings.Contains(footer, `href="https://github.com/edespino/gh-ci-status">source</a>`) {
		t.Errorf("without a version the footer still names the project: %q", footer)
	}
}
