// SPDX-License-Identifier: Apache-2.0

package web

import (
	"bytes"
	"context"
	"fmt"
	"github.com/edespino/gh-ci-status/internal/banner"
	"github.com/edespino/gh-ci-status/internal/fakegithub"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The page's script talks to /api/row with fetch(); a Content-Security-Policy
// without connect-src blocks it (connect-src falls back to default-src 'none').
func TestCSPAllowsSameOriginFetch(t *testing.T) {
	s, _, _ := newTestServer(t)
	csp := do(s, "GET", keyed("/healthz"), host, nil).Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "connect-src 'self'") {
		t.Fatalf("CSP %q lacks connect-src 'self'", csp)
	}
}

// chrome finds a Chrome or Chromium to drive headless: $CHROME_PATH, a
// browser on PATH, or the macOS application. Tests that need it are skipped
// when there is none.
func chrome(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("CHROME_PATH"); p != "" {
		return p
	}
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	const mac = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	if _, err := os.Stat(mac); err == nil {
		return mac
	}
	t.Skip("no Chrome or Chromium found; set CHROME_PATH to run the browser tests")
	return ""
}

// TestConfigurePageInBrowser loads the Configure page in headless Chrome
// against a fake GitHub with one failing, unwatched workflow and checks that
// the script filled in the Failing count and enabled "Watch all failing".
// Skipped when there is no Chrome.
func TestConfigurePageInBrowser(t *testing.T) {
	chrome(t)
	f := fakegithub.New(t)
	serveRepo(f, "b.yml", "red.yml")
	f.Handle(tr+"/actions/workflows/red.yml/runs?branch=main&exclude_pull_requests=true&per_page=100", 200,
		`{"total_count":1,"workflow_runs":[`+srvRun(300, 7, "main", "failure", "2026-10-20T12:00:00Z", ".github/workflows/red.yml")+`]}`)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	s := New(Options{Client: func(context.Context) (*Client, error) { return testClient(f), nil }, Dir: t.TempDir(),
		Key: testKey, Port: port, Now: func() time.Time { return testNow }})
	writeConfig(t, s, Watch{Workflow: "b.yml", Branch: "main"})
	srv := &http.Server{Handler: s.Handler()}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	dom := chromeDOM(t, "http://127.0.0.1:"+strconv.Itoa(port)+"/configure?key="+testKey)
	for _, id := range []string{"progress", "progress-head"} {
		if !regexp.MustCompile(`<span id="` + id + `"[^>]*hidden`).MatchString(dom) {
			t.Errorf("%s still showing after all statuses loaded: %s", id, regexp.MustCompile(`<span id="`+id+`"[^>]*>`).FindString(dom))
		}
	}
	if m := regexp.MustCompile(`id="n-failing">(\d+)<`).FindStringSubmatch(dom); m == nil || m[1] != "1" {
		t.Errorf("Failing count = %v, want 1", m)
	}
	if !regexp.MustCompile(`<form method="post" action="/watch-many[^>]*>.*?<input type="hidden" name="workflow" value="red\.yml">.*?<button type="submit">`).MatchString(dom) {
		t.Errorf("watch-failing form not enabled with red.yml:\n%s", regexp.MustCompile(`(?s)<form method="post" action="/watch-many.*?</form>`).FindString(dom))
	}
}

// chromeDOM loads url in headless Chrome and returns the DOM after its
// scripts have run.
func chromeDOM(t *testing.T, url string, flags ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	// --no-sandbox: CI runners may not allow Chrome's sandbox; these pages are
	// the tests' own.
	args := append([]string{"--headless=new", "--disable-gpu", "--no-first-run", "--no-sandbox",
		"--user-data-dir=" + filepath.Join(t.TempDir(), "profile"), "--virtual-time-budget=10000"}, flags...)
	out, err := exec.CommandContext(ctx, chrome(t), append(args, "--dump-dom", url)...).Output()
	if err != nil {
		t.Fatalf("chrome: %v", err)
	}
	return string(out)
}

// TestConfirmGatesSubmit submits a data-confirm form twice in headless Chrome:
// declined, it must not submit or show the overlay; accepted, it submits and
// shows the overlay. Skipped when there is no Chrome.
func TestConfirmGatesSubmit(t *testing.T) {
	chrome(t)
	var busy bytes.Buffer
	if err := templates.ExecuteTemplate(&busy, "busy", nil); err != nil {
		t.Fatal(err)
	}
	page := `<!doctype html><html><body>
<form id="f" method="post" action="/x" data-confirm="Sure?" data-busy="Saving…"><button type="submit">go</button></form>` + busy.String() + `
<script>
var out = [];
document.addEventListener("submit", function (e) { out.push(e.defaultPrevented ? "cancelled" : "sent"); e.preventDefault(); });
function state() { return document.getElementById("busy").hidden ? "idle" : "busy"; }
window.confirm = function () { return false; };
document.getElementById("f").requestSubmit();
out.push(state());
window.confirm = function () { return true; };
document.getElementById("f").requestSubmit();
out.push(state());
document.body.setAttribute("data-result", out.join(","));
</script></body></html>`
	path := filepath.Join(t.TempDir(), "confirm.html")
	if err := os.WriteFile(path, []byte(page), 0o600); err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`data-result="([^"]*)"`).FindStringSubmatch(chromeDOM(t, "file://"+path))
	if m == nil || m[1] != "cancelled,idle,sent,busy" {
		t.Fatalf("result = %v, want cancelled,idle,sent,busy", m)
	}
}

// TestDoneVisibleAfterScroll renders a long Configure page, scrolls to the
// bottom in headless Chrome and checks that Done is still on screen and that
// the filter bar sits below the header rather than over it.
func TestDoneVisibleAfterScroll(t *testing.T) {
	chrome(t)
	p := sampleConfigurePage()
	for i := 0; i < 80; i++ {
		p.Items = append(p.Items, ConfigureItem{File: fmt.Sprintf("w%d.yml", i), Name: fmt.Sprintf("Workflow %d", i), State: "active"})
	}
	var b bytes.Buffer
	if err := renderConfigure(&b, p); err != nil {
		t.Fatal(err)
	}
	probe := `<script>
window.scrollTo(0, document.body.scrollHeight);
var done = document.querySelector("a.done").getBoundingClientRect();
var head = document.querySelector("header").getBoundingClientRect();
var tools = document.querySelector(".tools").getBoundingClientRect();
var r = [];
r.push(window.scrollY > 0 ? "scrolled" : "not-scrolled");
r.push(done.top >= 0 && done.bottom <= window.innerHeight ? "done-visible" : "done-hidden");
r.push(tools.top >= head.bottom - 1 ? "tools-below" : "tools-overlap");
document.body.setAttribute("data-result", r.join(","));
</script></body>`
	path := filepath.Join(t.TempDir(), "configure.html")
	if err := os.WriteFile(path, []byte(strings.Replace(b.String(), "</body>", probe, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`data-result="([^"]*)"`).FindStringSubmatch(chromeDOM(t, "file://"+path))
	if m == nil || m[1] != "scrolled,done-visible,tools-below" {
		t.Fatalf("result = %v, want scrolled,done-visible,tools-below", m)
	}
}

// TestConfigureShowsLoadingUntilDone stalls one status request and checks that
// the page still says it is reading: a spinner in the header, the progress
// message beside the Add workflows heading (not among the toolbar controls),
// Failing count "…", and "Watch all failing" disabled although a failing,
// unwatched row has loaded.
func TestConfigureShowsLoadingUntilDone(t *testing.T) {
	chrome(t)
	var b bytes.Buffer
	if err := renderConfigure(&b, sampleConfigurePage()); err != nil {
		t.Fatal(err)
	}
	stub := `<body><script>
window.fetch = function (url) {
  if (url.indexOf("workflow=gone.yml") !== -1) { return new Promise(function () {}); }
  return Promise.resolve({json: function () { return {state: "failing", text: "failing", url: "#"}; }});
};
</script>`
	path := filepath.Join(t.TempDir(), "configure.html")
	if err := os.WriteFile(path, []byte(strings.Replace(b.String(), "<body>", stub, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	dom := chromeDOM(t, "file://"+path)
	for _, id := range []string{"progress", "progress-head"} {
		if tag := regexp.MustCompile(`<span id="` + id + `"[^>]*>`).FindString(dom); tag == "" || strings.Contains(tag, "hidden") {
			t.Errorf("%s tag %q: want it showing while a status is outstanding", id, tag)
		}
	}
	if !regexp.MustCompile(`(?s)<h2>Add workflows</h2>\s*<span id="progress"[^>]*>(?:<[^>]*>|\s)*Reading statuses 3 of 4`).MatchString(dom) {
		t.Errorf("progress message not beside the Add workflows heading: %s", regexp.MustCompile(`(?s)<h2>Add workflows</h2>.*?</div>`).FindString(dom))
	}
	if tools := regexp.MustCompile(`(?s)<div class="tools">.*?</div>`).FindString(dom); strings.Contains(tools, "Reading statuses") {
		t.Errorf("progress message still in the toolbar: %s", tools)
	}
	header := regexp.MustCompile(`(?s)<header>.*?</header>`).FindString(dom)
	if strings.Contains(regexp.MustCompile(`<[^>]*>`).ReplaceAllString(header, ""), "Reading statuses") {
		t.Errorf("header shows the progress text; want only a spinner: %s", header)
	}
	if !strings.Contains(dom, `id="n-add-failing"></span>`) {
		t.Errorf("Watch all failing label: %s", regexp.MustCompile(`id="n-add-failing">[^<]*<`).FindString(dom))
	}
	if !strings.Contains(dom, `id="n-failing">…<`) {
		t.Errorf("Failing count: %s", regexp.MustCompile(`id="n-failing">[^<]*<`).FindString(dom))
	}
	if !regexp.MustCompile(`(?s)<form[^>]*id="watch-failing".*?<button type="submit"[^>]*disabled`).MatchString(dom) {
		t.Errorf("Watch all failing enabled before all statuses loaded: %s", regexp.MustCompile(`(?s)<form[^>]*id="watch-failing".*?</form>`).FindString(dom))
	}
}

// TestFiltersHideRows types in the filter and presses Failing in headless
// Chrome and counts the rows actually displayed, not just marked hidden.
func TestFiltersHideRows(t *testing.T) {
	chrome(t)
	var b bytes.Buffer
	if err := renderConfigure(&b, sampleConfigurePage()); err != nil {
		t.Fatal(err)
	}
	stub := `<body><script>
window.fetch = function (url) {
  var st = url.indexOf("workflow=c.yml") !== -1 ? "failing" : "passing";
  return Promise.resolve({json: function () { return {state: st, text: st, url: "#"}; }});
};
</script>`
	probe := `<script>
setTimeout(function () {
  function displayed() {
    return Array.prototype.filter.call(document.querySelectorAll(".add-item"), function (i) {
      return getComputedStyle(i).display !== "none";
    }).length;
  }
  var r = [];
  var f = document.getElementById("filter");
  f.value = "build"; f.dispatchEvent(new Event("input"));
  r.push("text=" + displayed());
  f.value = ""; f.dispatchEvent(new Event("input"));
  document.querySelector('[data-filter="failing"]').click();
  r.push("failing=" + displayed());
  document.body.setAttribute("data-result", r.join(","));
}, 100);
</script></body>`
	page := strings.Replace(strings.Replace(b.String(), "<body>", stub, 1), "</body>", probe, 1)
	path := filepath.Join(t.TempDir(), "configure.html")
	if err := os.WriteFile(path, []byte(page), 0o600); err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`data-result="([^"]*)"`).FindStringSubmatch(chromeDOM(t, "file://"+path))
	if m == nil || m[1] != "text=1,failing=1" {
		t.Fatalf("displayed rows = %v, want text=1,failing=1", m)
	}
}

// Every page shows the wordmark (no tagline) at the start of its header.
func TestPagesShowWordmark(t *testing.T) {
	cfg := configureHTML(t)
	for name, page := range map[string]string{"status": statusHTML(t, redRow()), "configure": cfg, "setup": setupHTML(t)} {
		header := strings.SplitN(page, "</header>", 2)[0]
		if !regexp.MustCompile(`<header>\s*(<h1[^>]*>)?\s*<svg class="wordmark"[^>]*role="img" aria-label="gh-ci-status"`).MatchString(header) {
			t.Errorf("%s header does not start with the wordmark: %s", name, header)
		}
		if strings.Contains(header, banner.Tagline) {
			t.Errorf("%s header shows the tagline", name)
		}
	}
	if !strings.Contains(strings.SplitN(cfg, "</header>", 2)[0], ">Configure</h1>") {
		t.Error("configure header lost its title")
	}
}

// The logo is the same on every page: one size, set in the mark itself, and no
// page restyles its size.
func TestWordmarkSameOnEveryPage(t *testing.T) {
	tag := regexp.MustCompile(`<svg class="wordmark"[^>]*>`)
	status, cfg, setup := statusHTML(t, redRow()), configureHTML(t), setupHTML(t)
	s, c, u := tag.FindString(status), tag.FindString(cfg), tag.FindString(setup)
	if s == "" || s != c || s != u || !strings.Contains(s, ` height="22"`) {
		t.Fatalf("wordmark differs or has no fixed size:\nstatus    %s\nconfigure %s\nsetup     %s", s, c, u)
	}
	resize := regexp.MustCompile(`\.wordmark\{[^}]*(height|width)`)
	grey := regexp.MustCompile(`\.wordmark\{[^}]*color:var\(--muted\)`)
	for name, page := range map[string]string{"status": status, "configure": cfg, "setup": setup} {
		if m := resize.FindString(page); m != "" {
			t.Errorf("%s page resizes the wordmark: %s", name, m)
		}
		if !grey.MatchString(page) {
			t.Errorf("%s page does not give the wordmark's \"gh-\" its muted grey", name)
		}
	}
}

func setupHTML(t *testing.T) string {
	t.Helper()
	var b bytes.Buffer
	if err := renderSetup(&b, SetupPage{Key: testKey, Repos: []string{testRepo}}); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// TestWatchShownFollowsFilter filters the Add list in headless Chrome and
// checks that "Watch N shown" offers exactly the shown, unwatched workflows
// that are active on GitHub (not disabled or deleted), and follows the filter
// as it changes.
func TestWatchShownFollowsFilter(t *testing.T) {
	chrome(t)
	p := sampleConfigurePage()
	p.Items = append(p.Items,
		ConfigureItem{File: "d.yml", Name: "Deploy", State: "active"},
		ConfigureItem{File: "e.yml", Name: "Deploy docs", State: "active"},
		ConfigureItem{File: "f.yml", Name: "Deploy site", State: "active", WatchedOnDefault: true},
		ConfigureItem{File: "g.yml", Name: "Deploy old", State: "disabled_manually"},
		ConfigureItem{File: "h.yml", Name: "Deploy gone", State: "deleted"})
	var b bytes.Buffer
	if err := renderConfigure(&b, p); err != nil {
		t.Fatal(err)
	}
	stub := `<body><script>
window.fetch = function () { return Promise.resolve({json: function () { return {state: "passing", text: "passing", url: "#"}; }}); };
</script>`
	probe := `<script>
setTimeout(function () {
  function state() {
    var form = document.getElementById("watch-shown");
    var files = Array.prototype.map.call(form.querySelectorAll('#shown-inputs input[name="workflow"]'), function (i) { return i.value; });
    return form.querySelector("button").textContent.replace(/\s+/g, " ").trim() + "|" + files.join(",") + "|" +
      (form.querySelector("button").disabled ? "off" : "on") + "|" + form.dataset.confirm;
  }
  var f = document.getElementById("filter"), r = [];
  f.value = "deploy"; f.dispatchEvent(new Event("input"));
  r.push(state());
  f.value = "zzz"; f.dispatchEvent(new Event("input"));
  r.push(state());
  f.value = ""; f.dispatchEvent(new Event("input"));
  r.push(state());
  document.body.setAttribute("data-result", r.join(" / "));
}, 100);
</script></body>`
	page := strings.Replace(strings.Replace(b.String(), "<body>", stub, 1), "</body>", probe, 1)
	path := filepath.Join(t.TempDir(), "configure.html")
	if err := os.WriteFile(path, []byte(page), 0o600); err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`data-result="([^"]*)"`).FindStringSubmatch(chromeDOM(t, "file://"+path))
	want := "Watch 2 shown|d.yml,e.yml|on|Watch 2 workflows on main? / " +
		"Watch 0 shown||off|Watch 0 workflows on main? / " +
		"Watch 2 shown|d.yml,e.yml|on|Watch 2 workflows on main?"
	if m == nil || strings.ReplaceAll(m[1], "&gt;", ">") != want {
		t.Fatalf("got  %v\nwant %s", m, want)
	}
}

// viewPage is the Configure page with extra "Deploy" workflows, a stubbed
// status read, beforeScript run before the page's own script, and probe run
// after it; it returns the probe's data-result.
func viewPage(t *testing.T, beforeScript, probe string) string {
	t.Helper()
	p := sampleConfigurePage()
	p.Items = append(p.Items,
		ConfigureItem{File: "d.yml", Name: "Deploy", State: "active"},
		ConfigureItem{File: "e.yml", Name: "Deploy docs", State: "active"},
		ConfigureItem{File: "f.yml", Name: "Deploy site", State: "active", WatchedOnDefault: true})
	var b bytes.Buffer
	if err := renderConfigure(&b, p); err != nil {
		t.Fatal(err)
	}
	stub := `<body><script>
window.fetch = function () { return Promise.resolve({json: function () { return {state: "passing", text: "passing", url: "#"}; }}); };
` + beforeScript + `
</script>`
	page := strings.Replace(strings.Replace(b.String(), "<body>", stub, 1), "</body>", `<script>setTimeout(function () {`+probe+`}, 100);</script></body>`, 1)
	path := filepath.Join(t.TempDir(), "configure.html")
	if err := os.WriteFile(path, []byte(page), 0o600); err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`data-result="([^"]*)"`).FindStringSubmatch(chromeDOM(t, "file://"+path))
	if m == nil {
		t.Fatal("probe gave no result")
	}
	return m[1]
}

const viewKey = `"gh-ci-status/configure/` + testRepo + `"`

// After a change on Configure, the reloaded page shows the same filter, so
// several workflows can be added one after another; the saved view is used
// once.
func TestConfigureRestoresViewAfterChange(t *testing.T) {
	chrome(t)
	got := viewPage(t, `sessionStorage.setItem(`+viewKey+`, JSON.stringify({q: "deploy", mode: "unwatched", y: 0}));`, `
  var shown = Array.prototype.filter.call(document.querySelectorAll(".add-item"), function (i) { return getComputedStyle(i).display !== "none"; });
  document.body.setAttribute("data-result", [
    document.getElementById("filter").value,
    document.querySelector('[data-filter="unwatched"]').classList.contains("on") ? "unwatched-on" : "unwatched-off",
    shown.map(function (i) { return i.dataset.file; }).join(","),
    sessionStorage.getItem(`+viewKey+`) === null ? "used-once" : "still-saved"].join("|"));`)
	if got != "deploy|unwatched-on|d.yml,e.yml|used-once" {
		t.Fatalf("restored view = %q", got)
	}
}

// Pressing a row's "+ main" saves the view for the reload; a confirmation the
// user declines saves nothing.
func TestConfigureSavesViewOnChange(t *testing.T) {
	chrome(t)
	got := viewPage(t, ``, `
  var f = document.getElementById("filter");
  f.value = "deploy"; f.dispatchEvent(new Event("input"));
  document.querySelector('[data-filter="unwatched"]').click();
  window.addEventListener("submit", function (e) { e.preventDefault(); });
  window.confirm = function () { return false; };
  document.querySelector('form[action^="/unwatch-all"]').requestSubmit();
  var r = [sessionStorage.getItem(`+viewKey+`) === null ? "declined-nothing-saved" : "declined-saved"];
  document.querySelector('.add-item[data-file="d.yml"] form').requestSubmit();
  var s = JSON.parse(sessionStorage.getItem(`+viewKey+`) || "{}");
  r.push(s.q + "," + s.mode + "," + (typeof s.y));
  document.body.setAttribute("data-result", r.join("|"));`)
	if got != "declined-nothing-saved|deploy,unwatched,number" {
		t.Fatalf("saved view = %q", got)
	}
}

// "Watch all failing" skips workflows that are not active on GitHub too.
func TestWatchAllFailingSkipsTurnedOff(t *testing.T) {
	chrome(t)
	got := viewPage(t, `window.fetch = function () { return Promise.resolve({json: function () { return {state: "failing", text: "failing", url: "#"}; }}); };`, `
  var files = Array.prototype.map.call(document.querySelectorAll('#failing-inputs input[name="workflow"]'), function (i) { return i.value; });
  document.body.setAttribute("data-result", files.join(","));`)
	if got != "d.yml,e.yml" {
		t.Fatalf("Watch all failing offers %q, want only the active, unwatched ones", got)
	}
}

// statusPage renders the status page with rows, runs beforeScript before the
// page's own script and probe after it, and returns the probe's data-result.
func statusPage(t *testing.T, rows []Row, beforeScript, probe string, flags ...string) string {
	t.Helper()
	page := strings.Replace(strings.Replace(statusHTML(t, rows...), "<body>", "<body><script>"+beforeScript+"</script>", 1),
		"</body>", `<script>setTimeout(function () {`+probe+`}, 50);</script></body>`, 1)
	path := filepath.Join(t.TempDir(), "status.html")
	if err := os.WriteFile(path, []byte(page), 0o600); err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`data-result="([^"]*)"`).FindStringSubmatch(chromeDOM(t, "file://"+path, flags...))
	if m == nil {
		t.Fatal("probe gave no result")
	}
	return m[1]
}

// A row with a run in progress moves an amber band down its stripe; the
// stripe keeps the result's colour, grey when no run has finished. With
// reduced motion nothing moves: the stripe's lower half is amber instead.
func TestRunInProgressStripeMoves(t *testing.T) {
	chrome(t)
	rows := filterRows()
	rows[0].InProgress = runAt(121, 21, "release/x", "in_progress", "", "2026-10-24T11:00:00Z")
	first := runAt(60, 1, "main", "queued", "", "2026-10-24T11:20:00Z")
	rows = append(rows, Row{Repo: testRepo, Watch: Watch{Workflow: "new.yml", Branch: "main"}, Name: "New", State: Running, Latest: first, InProgress: first})
	const probe = `
  function row(n) { return document.querySelector('.row[data-name^="' + n + ' "]'); }
  var moving = document.getAnimations().filter(function (a) { return a.playState === "running" && a.effect.pseudoElement === "::before"; })
    .map(function (a) { return a.effect.target.dataset.name.split(" ")[0]; }).sort().join(",");
  var band = getComputedStyle(row("Build"), "::before").backgroundImage.indexOf("rgb(202, 138, 4)") !== -1;
  document.body.setAttribute("data-result", [matchMedia("(prefers-reduced-motion: reduce)").matches, moving, band,
    getComputedStyle(row("Build")).borderLeftColor, getComputedStyle(row("New")).borderLeftColor,
    getComputedStyle(row("Green"), "::before").content].join("|"));`
	if got := statusPage(t, rows, ``, probe); got != "false|Build,New|true|rgb(220, 38, 38)|rgb(161, 161, 170)|none" {
		t.Errorf("got %q", got)
	}
	if got := statusPage(t, rows, ``, probe, "--force-prefers-reduced-motion"); got != "true||true|rgb(220, 38, 38)|rgb(161, 161, 170)|none" {
		t.Errorf("reduced motion: got %q", got)
	}
}

func filterRows() []Row {
	green := Row{Repo: testRepo, Watch: mainWatch, Name: "Green", State: Passing, Latest: runAt(50, 5, "main", "completed", "success", "2026-10-23T12:00:00Z")}
	deploy := Row{Repo: testRepo, Watch: Watch{Workflow: "deploy.yml", Branch: "main"}, Name: "Deploy", State: Passing, Latest: runAt(51, 6, "main", "completed", "success", "2026-10-23T12:00:00Z")}
	return []Row{redRow(), green, deploy}
}

const shownJS = `function shown() { return Array.prototype.filter.call(document.querySelectorAll(".row"), function (r) { return getComputedStyle(r).display !== "none"; }).map(function (r) { return r.dataset.name.split(" ")[0]; }).join(","); }
function gapShown() { var g = document.querySelector(".gap"); return g && getComputedStyle(g).display !== "none" ? "gap" : "no-gap"; }
`

// Running shows every row with a run in progress, whatever its result; Red
// still shows a red row while it runs again.
func TestStatusRunningFilter(t *testing.T) {
	chrome(t)
	rows := filterRows()
	rows[0].InProgress = runAt(121, 21, "release/x", "in_progress", "", "2026-10-24T11:00:00Z")
	rows[1].InProgress = runAt(52, 7, "main", "queued", "", "2026-10-24T11:10:00Z")
	got := statusPage(t, rows, ``, shownJS+`
  var r = [];
  ["running", "failing", "passing"].forEach(function (m) {
    document.querySelector('[data-filter="' + m + '"]').click();
    r.push(m + ":" + shown());
  });
  document.body.setAttribute("data-result", r.join(" / "));`)
	if got != "running:Build,Green / failing:Build / passing:Green,Deploy" {
		t.Fatalf("got %q", got)
	}
}

func TestStatusFilterNarrowsRows(t *testing.T) {
	chrome(t)
	got := statusPage(t, filterRows(), ``, shownJS+`
  var f = document.getElementById("filter"), r = [];
  f.value = "deploy"; f.dispatchEvent(new Event("input"));
  r.push(shown() + ":" + gapShown());
  f.value = ""; f.dispatchEvent(new Event("input"));
  document.querySelector('[data-filter="failing"]').click();
  r.push(shown());
  f.value = "nothing-like-this"; f.dispatchEvent(new Event("input"));
  r.push(shown() + ":" + (document.getElementById("no-match").hidden ? "no-note" : "note"));
  document.querySelector('[data-filter="all"]').click();
  f.value = ""; f.dispatchEvent(new Event("input"));
  r.push(shown() + ":" + gapShown());
  document.body.setAttribute("data-result", r.join(" / "));`)
	if got != "Deploy:no-gap / Build / :note / Build,Green,Deploy:gap" {
		t.Fatalf("got %q", got)
	}
}

// The filter survives Refresh (used once); a saved state that is no longer
// present falls back to All.
func TestStatusKeepsFilterThroughRefresh(t *testing.T) {
	chrome(t)
	got := statusPage(t, filterRows(), `sessionStorage.setItem("gh-ci-status/status", JSON.stringify({q: "e", mode: "passing"}));`, shownJS+`
  var r = [document.getElementById("filter").value, shown(), sessionStorage.getItem("gh-ci-status/status") === null ? "used-once" : "still-saved"];
  window.addEventListener("submit", function (e) { e.preventDefault(); });
  document.querySelector('form[action^="/refresh"]').requestSubmit();
  var s = JSON.parse(sessionStorage.getItem("gh-ci-status/status") || "{}");
  r.push(s.q + "," + s.mode);
  document.body.setAttribute("data-result", r.join("|"));`)
	if got != "e|Green,Deploy|used-once|e,passing" {
		t.Fatalf("got %q", got)
	}
	gone := statusPage(t, filterRows(), `sessionStorage.setItem("gh-ci-status/status", JSON.stringify({q: "", mode: "running"}));`, shownJS+`
  document.body.setAttribute("data-result", shown() + ":" + document.querySelector(".seg button.on").dataset.filter);`)
	if gone != "Build,Green,Deploy:all" {
		t.Fatalf("saved state not present: got %q", gone)
	}
}
