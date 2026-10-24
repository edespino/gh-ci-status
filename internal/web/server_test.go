// SPDX-License-Identifier: Apache-2.0

package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cistatus "github.com/OWNER/ci-status"
)

func TestForbiddenWithoutKey(t *testing.T) {
	s, _, _ := newTestServer(t)
	for _, target := range []string{"/", "/configure", "/setup", "/healthz", "/?key=wrong"} {
		if rec := do(s, "GET", target, host, nil); rec.Code != 403 {
			t.Errorf("%s: %d", target, rec.Code)
		}
	}
}

func TestForbiddenForeignHost(t *testing.T) {
	s, _, _ := newTestServer(t)
	for _, h := range []string{"evil.example:8723", "127.0.0.1:9999", "localhost", "0.0.0.0:8723"} {
		if rec := do(s, "GET", keyed("/healthz"), h, nil); rec.Code != 403 {
			t.Errorf("Host %s: %d", h, rec.Code)
		}
	}
	if rec := do(s, "GET", keyed("/healthz"), "localhost:8723", nil); rec.Code != 200 {
		t.Errorf("localhost:8723: %d", rec.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	s, _, _ := newTestServer(t)
	for _, rec := range []*httptest.ResponseRecorder{
		do(s, "GET", keyed("/healthz"), host, nil),
		do(s, "GET", "/healthz", host, nil),
	} {
		h := rec.Header()
		if h.Get("Referrer-Policy") != "no-referrer" || h.Get("X-Content-Type-Options") != "nosniff" ||
			!strings.Contains(h.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
			t.Fatalf("headers = %v", h)
		}
	}
}

func TestRootRendersStatus(t *testing.T) {
	s, f, _ := newTestServer(t)
	serveRepo(f, "b.yml")
	writeConfig(t, s, Watch{Workflow: "b.yml", Branch: "main"})
	rec := do(s, "GET", keyed("/"), host, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "WF b.yml") || !strings.Contains(rec.Body.String(), "passing #1") {
		t.Fatalf("code %d body %s", rec.Code, rec.Body.String())
	}
}

// A new user has nothing set up: the status page sends them to Setup.
func TestRootWithoutReposGoesToSetup(t *testing.T) {
	s, f, _ := newTestServer(t)
	for _, prepare := range []func(){
		func() {},
		func() { _ = cistatus.SaveConfig(s.dir, Config{}) },
	} {
		prepare()
		rec := do(s, "GET", keyed("/"), host, nil)
		if rec.Code != 303 || rec.Header().Get("Location") != "/setup?key="+testKey {
			t.Fatalf("code %d location %q", rec.Code, rec.Header().Get("Location"))
		}
	}
	if n := len(f.Requests()); n != 0 {
		t.Errorf("nothing set up, yet %d GitHub requests: %v", n, f.Requests())
	}
}

func TestRootCorruptConfig(t *testing.T) {
	s, _, _ := newTestServer(t)
	path := filepath.Join(s.dir, "config.json")
	if err := os.WriteFile(path, []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := do(s, "GET", keyed("/"), host, nil)
	body := rec.Body.String()
	if rec.Code != 500 || !strings.Contains(body, path) || !strings.Contains(body, "Open Setup") {
		t.Fatalf("code %d body %s", rec.Code, body)
	}
}

// With more than one repository, each row names its repository.
func TestStatusNamesRepoWhenSeveral(t *testing.T) {
	s, f, _ := newTestServer(t)
	serveRepo(f, "b.yml")
	f.Handle("/repos/o/other/actions/workflows/c.yml", 200, `{"name":"Other C","path":".github/workflows/c.yml"}`)
	f.Handle("/repos/o/other/actions/workflows/c.yml/runs?branch=dev&exclude_pull_requests=true&per_page=100", 200,
		`{"total_count":0,"workflow_runs":[]}`)
	writeConfig(t, s, Watch{Workflow: "b.yml", Branch: "main"})
	one := do(s, "GET", keyed("/"), host, nil).Body.String()
	if strings.Contains(one, "<small>"+testRepo) {
		t.Error("a single repository should not be repeated on every row")
	}
	cfg := loadConfig(t, s).AddWatches("o/other", Watch{Workflow: "c.yml", Branch: "dev"})
	if err := cistatus.SaveConfig(s.dir, cfg); err != nil {
		t.Fatal(err)
	}
	two := do(s, "GET", keyed("/"), host, nil).Body.String()
	for _, want := range []string{"<small>" + testRepo + " · b.yml · main</small>", "<small>o/other · c.yml · dev</small>", testRepo + ", o/other"} {
		if !strings.Contains(two, want) {
			t.Errorf("page lacks %s", want)
		}
	}
}

func TestRefreshRedirects(t *testing.T) {
	s, _, _ := newTestServer(t)
	rec := do(s, "POST", keyed("/refresh"), host, url.Values{})
	if rec.Code != 303 || rec.Header().Get("Location") != "/?key="+testKey {
		t.Fatalf("code %d location %q", rec.Code, rec.Header().Get("Location"))
	}
	if rec := do(s, "GET", keyed("/refresh"), host, nil); rec.Code != 405 {
		t.Errorf("GET /refresh: %d", rec.Code)
	}
}

func TestHealthz(t *testing.T) {
	s, _, _ := newTestServer(t)
	if rec := do(s, "GET", keyed("/healthz"), host, nil); rec.Body.String() != "ok" {
		t.Fatalf("body %q", rec.Body.String())
	}
}

func TestTokenNeverInResponsesOrLog(t *testing.T) {
	s, f, logs := newTestServer(t)
	s.client = func(context.Context) (*Client, error) { c := testClient(f); c.Token = "tkn-secret"; return c, nil }
	serveRepo(f, "b.yml")
	writeConfig(t, s, Watch{Workflow: "b.yml", Branch: "main"}, Watch{Workflow: "gone.yml", Branch: "main"})
	recs := []*httptest.ResponseRecorder{
		do(s, "GET", keyed("/"), host, nil),
		do(s, "GET", keyed("/configure"), host, nil),
		do(s, "GET", keyed("/setup"), host, nil),
		do(s, "POST", keyed("/setup/add"), host, url.Values{"repo": {"o/missing"}}),
		do(s, "POST", keyed("/watch"), host, form("workflow", "b.yml", "branch", "bad branch")),
		do(s, "GET", keyed("/api/row")+"&repo="+testRepo+"&workflow=b.yml&branch=main", host, nil),
		do(s, "POST", keyed("/refresh"), host, url.Values{}),
		do(s, "GET", keyed("/healthz"), host, nil),
		do(s, "GET", keyed("/"), "evil.example:8723", nil),
	}
	for i, rec := range recs {
		if strings.Contains(rec.Body.String(), "tkn-secret") || strings.Contains(fmt.Sprint(rec.Header()), "tkn-secret") {
			t.Errorf("response %d leaks the token", i)
		}
	}
	if strings.Contains(logs.String(), "tkn-secret") || strings.Contains(logs.String(), testKey) {
		t.Errorf("log leaks the token or key: %s", logs.String())
	}
	if logs.Len() == 0 {
		t.Error("expected request log lines")
	}
}

// No cookie: cookies ignore ports, so any server on 127.0.0.1 would receive
// it. The key travels in every link, form action and redirect instead, so a
// page on another local port cannot act.
func TestNoCookieIsSet(t *testing.T) {
	s, _, _ := newTestServer(t)
	if rec := do(s, "GET", keyed("/healthz"), host, nil); rec.Header().Get("Set-Cookie") != "" {
		t.Fatalf("Set-Cookie = %q", rec.Header().Get("Set-Cookie"))
	}
}

func TestPostWithoutKeyRejected(t *testing.T) {
	s, _, _ := newTestServer(t)
	writeConfig(t, s, Watch{Workflow: "a.yml", Branch: "main"})
	before, _ := os.ReadFile(filepath.Join(s.dir, "config.json"))
	for _, path := range []string{"/watch", "/unwatch", "/watch-failing", "/unwatch-workflow", "/unwatch-all", "/setup/add", "/setup/remove", "/configure/refresh"} {
		req := httptest.NewRequest("POST", path, strings.NewReader(form("workflow", "b.yml", "branch", "main").Encode()))
		req.Host = host
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "http://127.0.0.1:3000")
		req.AddCookie(&http.Cookie{Name: "ci_status_key", Value: testKey})
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != 403 {
			t.Errorf("%s without key: %d", path, rec.Code)
		}
	}
	if after, _ := os.ReadFile(filepath.Join(s.dir, "config.json")); !bytes.Equal(before, after) {
		t.Fatal("config changed without the key")
	}
}

// The token is read on every page load, so 'gh auth login' fixes a running
// server without a restart.
func TestTokenReadPerRequest(t *testing.T) {
	s, f, _ := newTestServer(t)
	serveRepo(f, "b.yml")
	writeConfig(t, s, Watch{Workflow: "b.yml", Branch: "main"})
	tokens := []string{"t1", "t2"}
	s.client = func(context.Context) (*Client, error) {
		c := testClient(f)
		c.Token, tokens = tokens[0], tokens[1:]
		return c, nil
	}
	do(s, "GET", keyed("/"), host, nil)
	do(s, "GET", keyed("/"), host, nil)
	h := f.Headers()
	if h[0].Get("Authorization") != "Bearer t1" || h[len(h)-1].Get("Authorization") != "Bearer t2" {
		t.Fatalf("first %q last %q", h[0].Get("Authorization"), h[len(h)-1].Get("Authorization"))
	}
}

func TestTokenErrorShownOnPage(t *testing.T) {
	s, _, _ := newTestServer(t)
	writeConfig(t, s, Watch{Workflow: "b.yml", Branch: "main"})
	s.client = func(context.Context) (*Client, error) {
		return nil, fmt.Errorf("gh auth token failed (exit status 1); run 'gh auth login'")
	}
	for path, code := range map[string]int{"/": 502, "/configure": 502, "/setup": 200} {
		rec := do(s, "GET", keyed(path), host, nil)
		if rec.Code != code || !strings.Contains(rec.Body.String(), "gh auth login") {
			t.Errorf("%s: code %d body %q", path, rec.Code, rec.Body.String())
		}
	}
}

func TestAPIRowPassingAndFailing(t *testing.T) {
	s, f, _ := newTestServer(t)
	serveRepo(f, "b.yml")
	writeConfig(t, s)
	f.Handle(tr+"/actions/workflows/red.yml/runs?branch=main&exclude_pull_requests=true&per_page=100", 200,
		`{"total_count":1,"workflow_runs":[`+srvRun(300, 7, "main", "failure", "2026-10-20T12:00:00Z", ".github/workflows/red.yml")+`]}`)
	for _, c := range []struct{ file, state, url string }{
		{"b.yml", "passing", "/actions/runs/100"},
		{"red.yml", "failing", "/actions/runs/300"},
	} {
		rec := do(s, "GET", keyed("/api/row")+"&repo="+testRepo+"&workflow="+c.file+"&branch=main", host, nil)
		var got RowStatus
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != 200 {
			t.Fatalf("%s: code %d body %q err %v", c.file, rec.Code, rec.Body.String(), err)
		}
		if string(got.State) != c.state || !strings.HasSuffix(got.URL, c.url) || got.Text == "" {
			t.Errorf("%s: %+v", c.file, got)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("content type %q", ct)
		}
	}
	if n := count(f.Requests(), "/jobs"); n != 0 {
		t.Errorf("api/row made %d jobs requests", n)
	}
}

// /api/row reads only repositories that are set up, so the key alone cannot
// turn the server into a reader of anything else.
func TestAPIRowRejectsBadInput(t *testing.T) {
	s, f, _ := newTestServer(t)
	writeConfig(t, s)
	for _, q := range []string{
		"&repo=" + testRepo + "&workflow=../x.yml&branch=main",
		"&repo=" + testRepo + "&workflow=b.yml&branch=a%20b",
		"&repo=" + testRepo + "&workflow=b.yml",
		"&repo=o/not-set-up&workflow=b.yml&branch=main",
		"&workflow=b.yml&branch=main",
	} {
		if rec := do(s, "GET", keyed("/api/row")+q, host, nil); rec.Code != 400 {
			t.Errorf("%s: %d", q, rec.Code)
		}
	}
	if n := len(f.Requests()); n != 0 {
		t.Errorf("rejected requests reached GitHub: %v", f.Requests())
	}
}

func TestWatchSaves(t *testing.T) {
	s, f, _ := newTestServer(t)
	serveRepo(f, "b.yml")
	writeConfig(t, s, Watch{Workflow: "a.yml", Branch: "main"})
	rec := do(s, "POST", keyed("/watch"), host, form("workflow", "rc.yml", "branch", "main"))
	if rec.Code != 303 || rec.Header().Get("Location") != "/configure?key="+testKey+"&repo="+url.QueryEscape(testRepo) {
		t.Fatalf("code %d location %q", rec.Code, rec.Header().Get("Location"))
	}
	if got := loadConfig(t, s).Watches(testRepo); len(got) != 2 || got[1] != (Watch{Workflow: "rc.yml", Branch: "main"}) {
		t.Fatalf("watches %+v", got)
	}
}

func TestWatchNeedsASetUpRepo(t *testing.T) {
	s, _, _ := newTestServer(t)
	writeConfig(t, s, Watch{Workflow: "a.yml", Branch: "main"})
	before, _ := os.ReadFile(filepath.Join(s.dir, "config.json"))
	for _, repo := range []string{"", "o/not-set-up"} {
		rec := do(s, "POST", keyed("/watch"), host, url.Values{"repo": {repo}, "workflow": {"b.yml"}, "branch": {"main"}})
		if rec.Code != 400 || !strings.Contains(rec.Body.String(), "Setup") {
			t.Errorf("repo %q: code %d body %q", repo, rec.Code, rec.Body.String())
		}
	}
	if after, _ := os.ReadFile(filepath.Join(s.dir, "config.json")); !bytes.Equal(before, after) {
		t.Fatal("config changed")
	}
}

func TestUnwatch(t *testing.T) {
	s, _, _ := newTestServer(t)
	writeConfig(t, s, Watch{Workflow: "a.yml", Branch: "main"}, Watch{Workflow: "a.yml", Branch: "dev"})
	rec := do(s, "POST", keyed("/unwatch"), host, form("workflow", "a.yml", "branch", "dev"))
	if got := loadConfig(t, s).Watches(testRepo); rec.Code != 303 || len(got) != 1 || got[0] != (Watch{Workflow: "a.yml", Branch: "main"}) {
		t.Fatalf("code %d watches %+v", rec.Code, got)
	}
}

func TestWatchFailingAddsAll(t *testing.T) {
	s, f, _ := newTestServer(t)
	serveRepo(f, "b.yml")
	writeConfig(t, s, Watch{Workflow: "a.yml", Branch: "main"})
	rec := do(s, "POST", keyed("/watch-failing"), host, form("workflow", "x.yml", "workflow", "a.yml", "workflow", "y.yml", "workflow", "x.yml"))
	want := []Watch{{Workflow: "a.yml", Branch: "main"}, {Workflow: "x.yml", Branch: "main"}, {Workflow: "y.yml", Branch: "main"}}
	got := loadConfig(t, s).Watches(testRepo)
	if rec.Code != 303 || len(got) != 3 {
		t.Fatalf("code %d watches %+v", rec.Code, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("watches %+v", got)
		}
	}
	before, _ := os.ReadFile(filepath.Join(s.dir, "config.json"))
	rec = do(s, "POST", keyed("/watch-failing"), host, form("workflow", "z.yml", "workflow", "../bad.yml"))
	after, _ := os.ReadFile(filepath.Join(s.dir, "config.json"))
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "../bad.yml") || !bytes.Equal(before, after) {
		t.Fatalf("bad input: code %d body %q changed=%v", rec.Code, rec.Body.String(), !bytes.Equal(before, after))
	}
}

func TestWatchRejectsBadBranch(t *testing.T) {
	s, _, _ := newTestServer(t)
	writeConfig(t, s, Watch{Workflow: "a.yml", Branch: "main"})
	rec := do(s, "POST", keyed("/watch"), host, form("workflow", "a.yml", "branch", "bad branch"))
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "bad branch") {
		t.Fatalf("code %d body %q", rec.Code, rec.Body.String())
	}
}

func TestNoPostConfigure(t *testing.T) {
	s, _, _ := newTestServer(t)
	if rec := do(s, "POST", keyed("/configure"), host, url.Values{}); rec.Code != 405 {
		t.Fatalf("POST /configure: %d", rec.Code)
	}
}

func TestConfigurePageCarriesKeyAndRepo(t *testing.T) {
	s, f, _ := newTestServer(t)
	serveRepo(f, "extra.yml", "b.yml")
	writeConfig(t, s, Watch{Workflow: "b.yml", Branch: "main"})
	body := do(s, "GET", keyed("/configure")+"&repo="+testRepo, host, nil).Body.String()
	for _, want := range []string{`href="/?key=` + testKey + `"`, `action="/unwatch?key=` + testKey + `"`,
		`action="/watch?key=` + testKey + `"`, `action="/watch-failing?key=` + testKey + `"`, "WF b.yml", "WF extra.yml",
		`<input type="hidden" name="repo" value="` + testRepo + `">`, `href="/setup?key=` + testKey + `"`} {
		if !strings.Contains(body, want) {
			t.Errorf("configure page lacks %s", want)
		}
	}
	if strings.Count(body, "<form") != strings.Count(body, `name="repo"`) {
		t.Errorf("forms %d, repo inputs %d: every form must name its repository", strings.Count(body, "<form"), strings.Count(body, `name="repo"`))
	}
}

func TestConfigureChoosesRepo(t *testing.T) {
	s, f, _ := newTestServer(t)
	serveRepo(f, "b.yml")
	if rec := do(s, "GET", keyed("/configure"), host, nil); rec.Code != 303 || rec.Header().Get("Location") != "/setup?key="+testKey {
		t.Fatalf("nothing set up: code %d location %q", rec.Code, rec.Header().Get("Location"))
	}
	writeConfig(t, s, Watch{Workflow: "b.yml", Branch: "main"})
	if body := do(s, "GET", keyed("/configure"), host, nil).Body.String(); !strings.Contains(body, "WF b.yml") {
		t.Fatal("without ?repo= Configure should open the first repository")
	}
	if rec := do(s, "GET", keyed("/configure")+"&repo=o/not-set-up", host, nil); rec.Code != 404 || !strings.Contains(rec.Body.String(), "Setup") {
		t.Fatalf("unknown repo: code %d body %q", rec.Code, rec.Body.String())
	}
}

func TestUnwatchWorkflowRemovesEveryBranch(t *testing.T) {
	s, _, _ := newTestServer(t)
	writeConfig(t, s, Watch{Workflow: "a.yml", Branch: "main"}, Watch{Workflow: "b.yml", Branch: "main"}, Watch{Workflow: "a.yml", Branch: "dev"})
	rec := do(s, "POST", keyed("/unwatch-workflow"), host, form("workflow", "a.yml"))
	if got := loadConfig(t, s).Watches(testRepo); rec.Code != 303 || len(got) != 1 || got[0] != (Watch{Workflow: "b.yml", Branch: "main"}) {
		t.Fatalf("code %d watches %+v", rec.Code, got)
	}
	if rec := do(s, "POST", keyed("/unwatch-workflow"), host, form("workflow", "../x.yml")); rec.Code != 400 {
		t.Fatalf("bad workflow: %d", rec.Code)
	}
}

// Remove all empties the list but keeps the repository set up, so the status
// page shows nothing watched rather than sending the user to Setup.
func TestUnwatchAllKeepsRepo(t *testing.T) {
	s, f, _ := newTestServer(t)
	serveRepo(f, "b.yml")
	writeConfig(t, s, Watch{Workflow: "a.yml", Branch: "main"}, Watch{Workflow: "b.yml", Branch: "dev"})
	rec := do(s, "POST", keyed("/unwatch-all"), host, form())
	cfg := loadConfig(t, s)
	if rec.Code != 303 || !cfg.HasRepo(testRepo) || len(cfg.Watches(testRepo)) != 0 {
		t.Fatalf("code %d cfg %+v", rec.Code, cfg)
	}
	page := do(s, "GET", keyed("/"), host, nil)
	if body := page.Body.String(); page.Code != 200 || !strings.Contains(body, "nothing watched") || strings.Contains(body, `class="row `) || !strings.Contains(body, "Open Configure") {
		t.Fatalf("status after remove all: %d %s", page.Code, body)
	}
}
