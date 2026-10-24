// SPDX-License-Identifier: Apache-2.0

package web

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	cistatus "github.com/OWNER/gh-ci-status"
	"github.com/OWNER/gh-ci-status/internal/fakegithub"
)

// Short names for the library types these tests use throughout.
type (
	Watch      = cistatus.Watch
	Row        = cistatus.Row
	Run        = cistatus.Run
	Workflow   = cistatus.Workflow
	StepRef    = cistatus.StepRef
	Client     = cistatus.Client
	Config     = cistatus.Config
	RepoConfig = cistatus.RepoConfig
)

const (
	Failing = cistatus.Failing
	Error   = cistatus.Error
	Running = cistatus.Running
	Other   = cistatus.Other
	None    = cistatus.None
	Passing = cistatus.Passing
)

const testRepo = "example-org/example-repo"

const tr = "/repos/" + testRepo

var (
	testKey   = strings.Repeat("ab", 32)
	testNow   = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	mainWatch = Watch{Workflow: "b.yml", Branch: "main"}
)

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

func runAt(id int64, number int, branch, status, conclusion, created string) *Run {
	r := mkRun(id, number, branch, status, conclusion, created, "")
	return &r
}

// testClient reads the fake API with the token "tkn".
func testClient(f *fakegithub.Server) *Client {
	return &Client{BaseURL: f.URL(), Token: "tkn", HTTP: &http.Client{Timeout: 5 * time.Second}}
}

// newTestServer returns a server backed by a fake GitHub, with no repository
// set up yet.
func newTestServer(t *testing.T) (*Server, *fakegithub.Server, *bytes.Buffer) {
	t.Helper()
	f := fakegithub.New(t)
	f.Handle("/user", 200, `{"login":"octocat"}`)
	var logs bytes.Buffer
	s := New(Options{Client: func(context.Context) (*Client, error) { return testClient(f), nil }, Dir: t.TempDir(), Key: testKey,
		Port: 8723, Now: func() time.Time { return testNow }, Logger: log.New(&logs, "", 0)})
	return s, f, &logs
}

func srvRun(id, number int, branch, conclusion, created, path string) string {
	return fmt.Sprintf(`{"id":%d,"run_number":%d,"head_branch":%q,"status":"completed","conclusion":%q,"created_at":%q,"run_started_at":%q,`+
		`"html_url":"https://github.com/%s/actions/runs/%d","head_repository":{"full_name":%q},"path":%q}`,
		id, number, branch, conclusion, created, created, testRepo, id, testRepo, path)
}

// serveRepo registers testRepo's default branch, workflow list, recent runs
// and one passing run on main for every listed file.
func serveRepo(f *fakegithub.Server, files ...string) {
	f.Handle(tr, 200, `{"full_name":"`+testRepo+`","default_branch":"main"}`)
	var wfs, recent []string
	for i, file := range files {
		path := ".github/workflows/" + file
		wfs = append(wfs, fmt.Sprintf(`{"id":%d,"name":"WF %s","path":%q,"state":"active"}`, i+1, file, path))
		run := srvRun(100+i, 1, "main", "success", "2026-10-23T12:00:00Z", path)
		recent = append(recent, run)
		f.Handle(tr+"/actions/workflows/"+file, 200, fmt.Sprintf(`{"name":"WF %s","path":%q,"state":"active"}`, file, path))
		f.Handle(tr+"/actions/workflows/"+file+"/runs?branch=main&exclude_pull_requests=true&per_page=100", 200,
			`{"total_count":1,"workflow_runs":[`+run+`]}`)
	}
	f.Handle(tr+"/actions/workflows?per_page=100", 200, `{"workflows":[`+strings.Join(wfs, ",")+`]}`)
	f.Handle(tr+"/actions/runs?page=1&per_page=100", 200, `{"workflow_runs":[`+strings.Join(recent, ",")+`]}`)
	f.Handle(tr+"/actions/runs?page=2&per_page=100", 200, `{"workflow_runs":[]}`)
}

const host = "127.0.0.1:8723"

func keyed(path string) string { return path + "?key=" + testKey }

// form is url.Values for a Configure action on testRepo.
func form(kv ...string) url.Values {
	v := url.Values{"repo": {testRepo}}
	for i := 0; i+1 < len(kv); i += 2 {
		v.Add(kv[i], kv[i+1])
	}
	return v
}

func do(s *Server, method, target, host string, body url.Values) *httptest.ResponseRecorder {
	var rd io.Reader
	if body != nil {
		rd = strings.NewReader(body.Encode())
	}
	req := httptest.NewRequest(method, target, rd)
	req.Host = host
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

// writeConfig sets up testRepo with watches.
func writeConfig(t *testing.T, s *Server, watches ...Watch) {
	t.Helper()
	if err := cistatus.SaveConfig(s.dir, Config{}.SetWatches(testRepo, watches)); err != nil {
		t.Fatal(err)
	}
}

func loadConfig(t *testing.T, s *Server) Config {
	t.Helper()
	cfg, _, err := cistatus.LoadConfig(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
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
