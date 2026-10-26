// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	cistatus "github.com/edespino/gh-ci-status"
	"github.com/edespino/gh-ci-status/internal/fakegithub"
)

// TestMain pins the time zone before any test or server goroutine starts.
// With GH_CI_STATUS_HELPER=1 the test binary runs as gh-ci-status instead,
// against the fake GitHub named in the environment: the daemon tests start
// it as their background server.
func TestMain(m *testing.M) {
	if os.Getenv("GH_CI_STATUS_HELPER") == "1" {
		os.Exit(helperMain())
	}
	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	time.Local = la
	os.Exit(m.Run())
}

// helperMain is gh-ci-status for the daemon tests' background process. It
// "opens the browser" by creating the file "opened" in its settings folder.
func helperMain() int {
	dir := os.Getenv("GH_CI_STATUS_HELPER_DIR")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return run(ctx, os.Args[1:], deps{
		stdout: os.Stdout, stderr: os.Stderr,
		token: func(context.Context) (string, error) {
			if os.Getenv("GH_CI_STATUS_HELPER_NO_LOGIN") == "1" {
				return "", errors.New("no oauth token found for github.com")
			}
			return "tkn", nil
		},
		lookPath: func(string) (string, error) { return "/usr/bin/gh", nil },
		baseURL:  os.Getenv("GH_CI_STATUS_HELPER_BASEURL"), dir: dir,
		now: time.Now, open: func(string) error { return os.WriteFile(filepath.Join(dir, "opened"), nil, 0o600) },
		listen: func(addr string) (net.Listener, error) { return net.Listen("tcp", addr) },
	})
}

const testRepo = "example-org/example-repo"

var testNow = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

// syncBuf is a bytes.Buffer safe for the server goroutine and the test.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

type cliEnv struct {
	d       deps
	out     *syncBuf
	errOut  *syncBuf
	opened  chan string
	listens []string
	mu      sync.Mutex
}

func newCLI(t *testing.T, f *fakegithub.Server) *cliEnv {
	t.Helper()
	f.Handle("/user", 200, `{"login":"octocat"}`)
	e := &cliEnv{out: &syncBuf{}, errOut: &syncBuf{}, opened: make(chan string, 1)}
	e.d = deps{
		stdout: e.out, stderr: e.errOut,
		token:    func(context.Context) (string, error) { return "tkn", nil },
		lookPath: func(string) (string, error) { return "/usr/bin/gh", nil },
		baseURL:  f.URL(), dir: t.TempDir(),
		now:  func() time.Time { return testNow },
		open: func(u string) error { e.opened <- u; return nil },
		listen: func(addr string) (net.Listener, error) {
			e.mu.Lock()
			e.listens = append(e.listens, addr)
			e.mu.Unlock()
			return net.Listen("tcp", "127.0.0.1:0")
		},
	}
	return e
}

// serveRepo registers testRepo, its workflow list and one passing run on main
// for each file.
func serveRepo(f *fakegithub.Server, files ...string) {
	tr := "/repos/" + testRepo
	f.Handle(tr, 200, `{"full_name":"`+testRepo+`","default_branch":"main"}`)
	var wfs []string
	for i, file := range files {
		path := ".github/workflows/" + file
		wfs = append(wfs, fmt.Sprintf(`{"id":%d,"name":"WF %s","path":%q,"state":"active"}`, i+1, file, path))
		f.Handle(tr+"/actions/workflows/"+file, 200, fmt.Sprintf(`{"name":"WF %s","path":%q}`, file, path))
		f.Handle(tr+"/actions/workflows/"+file+"/runs?branch=main&exclude_pull_requests=true&per_page=100", 200,
			fmt.Sprintf(`{"total_count":1,"workflow_runs":[{"id":%d,"run_number":1,"head_branch":"main","status":"completed","conclusion":"success",`+
				`"created_at":"2026-10-23T12:00:00Z","html_url":"https://github.com/%s/actions/runs/%d","head_repository":{"full_name":%q},"path":%q}]}`,
				100+i, testRepo, 100+i, testRepo, path))
	}
	f.Handle(tr+"/actions/workflows?per_page=100", 200, `{"workflows":[`+strings.Join(wfs, ",")+`]}`)
}

func watchIn(t *testing.T, e *cliEnv, watches ...cistatus.Watch) {
	t.Helper()
	if err := cistatus.SaveConfig(e.d.dir, cistatus.Config{}.SetWatches(testRepo, watches)); err != nil {
		t.Fatal(err)
	}
}

func w(file, branch string) cistatus.Watch { return cistatus.Watch{Workflow: file, Branch: branch} }

func TestHelp(t *testing.T) {
	e := newCLI(t, fakegithub.New(t))
	if code := run(context.Background(), []string{"--help"}, e.d); code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{"serve", "--port", "--no-open", "--new-key", "--json", "add OWNER/REPO", "check", "Exit codes", "gh extension install", "gh ci-status stop", "--daemon"} {
		if !strings.Contains(e.out.String(), want) {
			t.Errorf("usage lacks %s", want)
		}
	}
	if code := run(context.Background(), []string{"bogus"}, e.d); code != 2 {
		t.Errorf("unknown command exit %d", code)
	}
}

func TestVersion(t *testing.T) {
	e := newCLI(t, fakegithub.New(t))
	if code := run(context.Background(), []string{"--version"}, e.d); code != 0 || !strings.HasPrefix(e.out.String(), "gh-ci-status ") {
		t.Fatalf("exit %d out %q", code, e.out.String())
	}
}

// A new user has nothing set up: no defaults, and a message that says what
// to do. Nothing is read from GitHub.
func TestStatusNotSetUp(t *testing.T) {
	f := fakegithub.New(t)
	e := newCLI(t, f)
	if code := run(context.Background(), nil, e.d); code != 2 || !strings.Contains(e.errOut.String(), "gh ci-status add OWNER/REPO") ||
		!strings.Contains(e.errOut.String(), "gh ci-status serve") || e.out.String() != "" {
		t.Fatalf("exit %d out %q err %q", code, e.out.String(), e.errOut.String())
	}
	e2 := newCLI(t, f)
	if code := run(context.Background(), []string{"--json"}, e2.d); code != 2 || !strings.Contains(e2.out.String(), `"repos": []`) {
		t.Fatalf("json: exit %d out %q", code, e2.out.String())
	}
	if n := count(f.Requests(), "/repos/"); n != 0 {
		t.Errorf("nothing set up, yet %d repository requests", n)
	}
}

func TestTextViewExitCodes(t *testing.T) {
	f := fakegithub.New(t)
	serveRepo(f, "b.yml")
	e := newCLI(t, f)
	watchIn(t, e, w("b.yml", "main"))
	if code := run(context.Background(), nil, e.d); code != 0 || !strings.HasPrefix(e.out.String(), "nothing red · 1 passing") {
		t.Fatalf("exit %d out %q", code, e.out.String())
	}
	watchIn(t, e, w("b.yml", "main"), w("gone.yml", "main"))
	if code := run(context.Background(), nil, e.d); code != 1 {
		t.Fatalf("exit %d with an error row", code)
	}
}

func TestTextViewCorruptConfig(t *testing.T) {
	e := newCLI(t, fakegithub.New(t))
	path := filepath.Join(e.d.dir, "config.json")
	if err := os.WriteFile(path, []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run(context.Background(), nil, e.d); code != 1 || !strings.Contains(e.errOut.String(), path) {
		t.Fatalf("exit %d stderr %q", code, e.errOut.String())
	}
}

// --json is the machine-readable report (the same an MCP tool returns).
func TestJSONOutput(t *testing.T) {
	f := fakegithub.New(t)
	serveRepo(f, "b.yml")
	e := newCLI(t, f)
	watchIn(t, e, w("b.yml", "main"))
	if code := run(context.Background(), []string{"--json"}, e.d); code != 0 {
		t.Fatalf("exit %d err %q", code, e.errOut.String())
	}
	var rep cistatus.Report
	if err := json.Unmarshal([]byte(e.out.String()), &rep); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, e.out.String())
	}
	if rep.Version == "" {
		t.Error("the JSON report does not name its version")
	}
	if len(rep.Rows) != 1 || rep.Rows[0].State != cistatus.Passing || rep.Rows[0].Latest == nil || rep.Summary != "nothing red · 1 passing" ||
		len(rep.Repos) != 1 || rep.Repos[0] != testRepo {
		t.Fatalf("report %+v", rep)
	}
	if strings.Contains(e.out.String(), "\x1b[") || strings.Contains(e.out.String(), "from red to green") {
		t.Fatal("JSON output must be plain")
	}
}

func TestTokenFailureExits1(t *testing.T) {
	e := newCLI(t, fakegithub.New(t))
	watchIn(t, e, w("b.yml", "main"))
	e.d.token = func(context.Context) (string, error) {
		return "", errors.New("gh not found on PATH; install the GitHub CLI")
	}
	for _, args := range [][]string{nil, {"serve", "--no-open"}} {
		if code := run(context.Background(), args, e.d); code != 1 || !strings.Contains(e.errOut.String()+e.out.String(), "gh not found") {
			t.Fatalf("%v: exit %d stderr %q", args, code, e.errOut.String())
		}
	}
	if len(e.listens) != 0 {
		t.Fatalf("listened despite token failure: %v", e.listens)
	}
}

func TestCheck(t *testing.T) {
	f := fakegithub.New(t)
	serveRepo(f, "b.yml")
	e := newCLI(t, f)
	watchIn(t, e, w("b.yml", "main"))
	if code := run(context.Background(), []string{"check"}, e.d); code != 0 {
		t.Fatalf("exit %d out %q", code, e.out.String())
	}
	for _, want := range []string{"ok    GitHub CLI: found at /usr/bin/gh", "ok    GitHub login", "ok    GitHub API: signed in as octocat",
		"ok    Repository " + testRepo + ": 1 workflow, default branch main"} {
		if !strings.Contains(e.out.String(), want) {
			t.Errorf("check output lacks %q:\n%s", want, e.out.String())
		}
	}
}

func TestCheckFailures(t *testing.T) {
	e := newCLI(t, fakegithub.New(t))
	if code := run(context.Background(), []string{"check"}, e.d); code != 1 || !strings.Contains(e.out.String(), "FAIL  Repositories: none set up yet") {
		t.Fatalf("no repos: exit %d out %q", code, e.out.String())
	}
	e = newCLI(t, fakegithub.New(t))
	e.d.lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	e.d.token = func(context.Context) (string, error) { return "", errors.New("gh not found on PATH") }
	code := run(context.Background(), []string{"check"}, e.d)
	out := e.out.String()
	if code != 1 || !strings.Contains(out, "FAIL  GitHub CLI") || !strings.Contains(out, "https://cli.github.com") || strings.Contains(out, "GitHub API") {
		t.Fatalf("no gh: exit %d out %q", code, out)
	}
}

func TestAdd(t *testing.T) {
	f := fakegithub.New(t)
	serveRepo(f, "b.yml")
	f.Handle("/repos/Example-Org/example-repo", 200, `{"full_name":"`+testRepo+`","default_branch":"main"}`)
	e := newCLI(t, f)
	if code := run(context.Background(), []string{"add", "https://github.com/Example-Org/example-repo"}, e.d); code != 0 {
		t.Fatalf("exit %d out %q err %q", code, e.out.String(), e.errOut.String())
	}
	cfg, _, _ := cistatus.LoadConfig(e.d.dir)
	if got := cfg.RepoNames(); len(got) != 1 || got[0] != testRepo {
		t.Fatalf("repos %v", got)
	}
	if !strings.Contains(e.out.String(), "Added "+testRepo) || !strings.Contains(e.out.String(), "gh ci-status serve") {
		t.Errorf("output %q", e.out.String())
	}
	e.out = &syncBuf{}
	e.d.stdout = e.out
	if code := run(context.Background(), []string{"add", testRepo}, e.d); code != 0 || !strings.Contains(e.out.String(), "already set up") {
		t.Fatalf("second add: exit %d out %q", code, e.out.String())
	}
}

func TestAddProblems(t *testing.T) {
	e := newCLI(t, fakegithub.New(t))
	if code := run(context.Background(), []string{"add"}, e.d); code != 2 {
		t.Errorf("no argument: exit %d", code)
	}
	if code := run(context.Background(), []string{"add", "o/missing"}, e.d); code != 1 || !strings.Contains(e.out.String(), "FAIL  Repository o/missing: not found") {
		t.Errorf("missing: exit %d out %q", code, e.out.String())
	}
	path := filepath.Join(e.d.dir, "config.json")
	if err := os.WriteFile(path, []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := fakegithub.New(t)
	serveRepo(f, "b.yml")
	e.d.baseURL = f.URL()
	if code := run(context.Background(), []string{"add", testRepo}, e.d); code != 1 || !strings.Contains(e.errOut.String(), path) {
		t.Errorf("corrupt config: exit %d err %q", code, e.errOut.String())
	}
	if data, _ := os.ReadFile(path); string(data) != "{nope" {
		t.Error("add from the terminal must not overwrite an unreadable config")
	}
}

func TestServeBindsLoopback(t *testing.T) {
	e := newCLI(t, fakegithub.New(t))
	e.d.listen = func(addr string) (net.Listener, error) {
		e.listens = append(e.listens, addr)
		return nil, errors.New("stop here")
	}
	run(context.Background(), []string{"serve", "--no-open"}, e.d)
	if len(e.listens) != 1 || e.listens[0] != "127.0.0.1:8723" {
		t.Fatalf("listen addresses %v", e.listens)
	}
}

func TestPortInUse(t *testing.T) {
	e := newCLI(t, fakegithub.New(t))
	e.d.listen = func(string) (net.Listener, error) {
		return nil, errors.New("listen tcp 127.0.0.1:8723: bind: address already in use")
	}
	if code := run(context.Background(), []string{"serve"}, e.d); code != 1 || !strings.Contains(e.errOut.String(), "--port") {
		t.Fatalf("exit %d stderr %q", code, e.errOut.String())
	}
}

var urlRe = regexp.MustCompile(`http://127\.0\.0\.1:(\d+)/\?key=([0-9a-f]{64})`)

// startServe runs `gh-ci-status serve args...` until the URL is printed and
// returns it with a stop function that waits for a clean exit.
func startServe(t *testing.T, e *cliEnv, args ...string) (string, func() int) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() { done <- run(ctx, append([]string{"serve"}, args...), e.d) }()
	deadline := time.Now().Add(3 * time.Second)
	for !urlRe.MatchString(e.out.String()) {
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("no URL printed; stdout %q stderr %q", e.out.String(), e.errOut.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	return urlRe.FindString(e.out.String()), func() int { cancel(); return <-done }
}

func TestServePrintsStableURL(t *testing.T) {
	e := newCLI(t, fakegithub.New(t))
	u, stop := startServe(t, e, "--no-open", "--port", "0")
	key, err := os.ReadFile(filepath.Join(e.d.dir, "key"))
	if err != nil || !strings.HasSuffix(u, "key="+strings.TrimSpace(string(key))) {
		t.Fatalf("url %q key file %q %v", u, key, err)
	}
	resp, err := http.Get(strings.Replace(u, "/?key=", "/healthz?key=", 1))
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("healthz %v %v", resp, err)
	}
	resp.Body.Close()
	if code := stop(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	select {
	case got := <-e.opened:
		t.Fatalf("--no-open still opened %q", got)
	default:
	}
	e.out = &syncBuf{}
	e.d.stdout = e.out
	u2, stop2 := startServe(t, e, "--no-open", "--port", "0")
	stop2()
	if urlRe.FindStringSubmatch(u2)[2] != urlRe.FindStringSubmatch(u)[2] {
		t.Fatalf("key changed across restarts: %q vs %q", u, u2)
	}
}

// serve checks the GitHub CLI and login first and says what it found; with
// nothing set up it says the page opens on Setup.
func TestServeChecksAndFirstRunHint(t *testing.T) {
	e := newCLI(t, fakegithub.New(t))
	_, stop := startServe(t, e, "--no-open", "--port", "0")
	stop()
	out := e.out.String()
	for _, want := range []string{"ok    GitHub CLI", "ok    GitHub API: signed in as octocat", "No repository set up yet", "Setup"} {
		if !strings.Contains(out, want) {
			t.Errorf("serve output lacks %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "ok    GitHub API") > strings.Index(out, "serving http://") {
		t.Error("checks should come before the address")
	}
}

func TestServeOpensByDefault(t *testing.T) {
	e := newCLI(t, fakegithub.New(t))
	u, stop := startServe(t, e, "--port", "0")
	defer stop()
	select {
	case got := <-e.opened:
		if got != u {
			t.Fatalf("opened %q, printed %q", got, u)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("browser not opened")
	}
}

func TestServeNewKey(t *testing.T) {
	e := newCLI(t, fakegithub.New(t))
	u1, stop := startServe(t, e, "--no-open", "--port", "0")
	stop()
	e.out = &syncBuf{}
	e.d.stdout = e.out
	u2, stop2 := startServe(t, e, "--no-open", "--port", "0", "--new-key")
	stop2()
	if urlRe.FindStringSubmatch(u1)[2] == urlRe.FindStringSubmatch(u2)[2] {
		t.Fatal("--new-key kept the old key")
	}
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

// With a repository but nothing watched yet, the status says how to choose
// workflows. It is not an error.
func TestStatusNothingWatchedHint(t *testing.T) {
	e := newCLI(t, fakegithub.New(t))
	watchIn(t, e)
	if code := run(context.Background(), nil, e.d); code != 0 || e.out.String() != "nothing watched\n" ||
		!strings.Contains(e.errOut.String(), "Configure") || !strings.Contains(e.errOut.String(), "gh ci-status serve") {
		t.Fatalf("exit %d out %q err %q", code, e.out.String(), e.errOut.String())
	}
}
