// SPDX-License-Identifier: Apache-2.0

package web

import (
	"encoding/json"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// Configure keeps what it read from GitHub until the user presses Refresh.

const aRuns = tr + "/actions/workflows/a.yml/runs?branch=main&exclude_pull_requests=true&per_page=100"

func rowAt(t *testing.T, s *Server, workflow string) RowStatus {
	t.Helper()
	rec := do(s, "GET", keyed("/api/row")+"&repo="+testRepo+"&workflow="+workflow+"&branch=main", host, nil)
	var st RowStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil || rec.Code != 200 {
		t.Fatalf("api/row: code %d body %q err %v", rec.Code, rec.Body.String(), err)
	}
	return st
}

// The operator's case: Remove, then come back to Configure without reading
// GitHub again; Refresh reads it again.
func TestConfigureKeepsReadUntilRefresh(t *testing.T) {
	s, f, _ := newTestServer(t)
	serveRepo(f, "a.yml", "b.yml")
	writeConfig(t, s, Watch{Workflow: "a.yml", Branch: "main"}, Watch{Workflow: "b.yml", Branch: "main"})
	if rec := do(s, "GET", keyed("/configure"), host, nil); rec.Code != 200 {
		t.Fatalf("first visit: %d", rec.Code)
	}
	first := len(f.Requests())
	if first == 0 {
		t.Fatal("first visit read nothing")
	}
	do(s, "POST", keyed("/unwatch-workflow"), host, form("workflow", "a.yml"))
	body := do(s, "GET", keyed("/configure"), host, nil).Body.String()
	if n := len(f.Requests()); n != first {
		t.Fatalf("second visit made %d GitHub requests: %v", n-first, f.Requests()[first:])
	}
	if strings.Count(body, `class="watch-item`) != 1 || !strings.Contains(body, "WF b.yml") {
		t.Fatal("second visit does not show the removal")
	}
	rec := do(s, "POST", keyed("/configure/refresh"), host, form())
	if rec.Code != 303 || rec.Header().Get("Location") != "/configure?key="+testKey+"&repo="+url.QueryEscape(testRepo) {
		t.Fatalf("refresh: code %d location %q", rec.Code, rec.Header().Get("Location"))
	}
	do(s, "GET", keyed("/configure"), host, nil)
	if n := count(f.Requests()[first:], "/actions/workflows?per_page=100"); n != 1 {
		t.Fatalf("after Refresh the workflow list was read %d times: %v", n, f.Requests()[first:])
	}
}

func TestConfigureFailedReadNotKept(t *testing.T) {
	s, f, _ := newTestServer(t)
	writeConfig(t, s)
	f.Handle(tr, 200, `{"default_branch":"main"}`)
	f.Handle(tr+"/actions/workflows?per_page=100", 500, `{"message":"boom"}`)
	if rec := do(s, "GET", keyed("/configure"), host, nil); rec.Code != 502 {
		t.Fatalf("failed read: %d", rec.Code)
	}
	serveRepo(f, "a.yml")
	if rec := do(s, "GET", keyed("/configure"), host, nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), "WF a.yml") {
		t.Fatalf("after the failure: code %d", rec.Code)
	}
}

// The first read asks GitHub for the default branch, workflows and recent
// runs at the same time.
func TestConfigureReadsInParallel(t *testing.T) {
	s, f, _ := newTestServer(t)
	writeConfig(t, s)
	serveRepo(f, "a.yml")
	f.SetDelay(30 * time.Millisecond)
	do(s, "GET", keyed("/configure"), host, nil)
	if f.MaxInFlight() < 3 {
		t.Fatalf("max in-flight requests = %d, want 3", f.MaxInFlight())
	}
}

func TestConfigureShowsReadTimeAndRefresh(t *testing.T) {
	s, f, _ := newTestServer(t)
	writeConfig(t, s)
	serveRepo(f, "a.yml")
	body := do(s, "GET", keyed("/configure"), host, nil).Body.String()
	header := strings.SplitN(body, "</header>", 2)[0]
	if !strings.Contains(header, "read 5:00 AM") {
		t.Errorf("header lacks the read time: %s", header)
	}
	if !strings.Contains(header, `action="/configure/refresh?key=`+testKey+`" data-busy=`) || !strings.Contains(header, ">Refresh</button>") {
		t.Errorf("header lacks the Refresh form: %s", header)
	}
}

func TestAPIRowKeptUntilRefresh(t *testing.T) {
	s, f, _ := newTestServer(t)
	writeConfig(t, s)
	serveRepo(f, "a.yml")
	rowAt(t, s, "a.yml")
	rowAt(t, s, "a.yml")
	if n := count(f.Requests(), aRuns); n != 1 {
		t.Fatalf("runs read %d times, want 1", n)
	}
	do(s, "POST", keyed("/configure/refresh"), host, form())
	rowAt(t, s, "a.yml")
	if n := count(f.Requests(), aRuns); n != 2 {
		t.Fatalf("after Refresh runs read %d times, want 2", n)
	}
}

func TestAPIRowErrorNotKept(t *testing.T) {
	s, f, _ := newTestServer(t)
	writeConfig(t, s)
	f.Handle(aRuns, 500, `{"message":"boom"}`)
	if st := rowAt(t, s, "a.yml"); st.State != Error {
		t.Fatalf("first read: %+v", st)
	}
	serveRepo(f, "a.yml")
	if st := rowAt(t, s, "a.yml"); st.State != Passing {
		t.Fatalf("second read: %+v", st)
	}
}

// The status page always reads fresh; what it reads replaces the status
// Configure keeps, so the two pages agree.
func TestStatusPageUpdatesKeptStatus(t *testing.T) {
	s, f, _ := newTestServer(t)
	serveRepo(f, "a.yml")
	writeConfig(t, s, Watch{Workflow: "a.yml", Branch: "main"})
	if st := rowAt(t, s, "a.yml"); st.State != Passing {
		t.Fatalf("kept: %+v", st)
	}
	f.Handle(aRuns, 200, `{"total_count":1,"workflow_runs":[`+srvRun(300, 7, "main", "failure", "2026-10-20T12:00:00Z", ".github/workflows/a.yml")+`]}`)
	if body := do(s, "GET", keyed("/"), host, nil).Body.String(); !strings.Contains(body, "failing #7") {
		t.Fatal("status page did not read fresh")
	}
	before := len(f.Requests())
	if st := rowAt(t, s, "a.yml"); st.State != Failing {
		t.Fatalf("Configure still shows %s after the status page read failing", st.State)
	}
	if len(f.Requests()) != before {
		t.Fatal("api/row read GitHub instead of using the status page's read")
	}
}

func TestReadTime(t *testing.T) {
	now := ts("2026-10-24T18:20:00Z")
	if got := readTime(now.Add(-time.Hour), now); got != "10:20 AM" {
		t.Errorf("same day: %q", got)
	}
	if got := readTime(ts("2026-10-23T18:20:00Z"), now); got != "Oct 23, 11:20 AM" {
		t.Errorf("earlier day: %q", got)
	}
}

// Pages, dots and Refresh at the same time share the kept data; run with -race.
func TestKeptConcurrentUse(t *testing.T) {
	s, f, _ := newTestServer(t)
	serveRepo(f, "a.yml", "b.yml")
	writeConfig(t, s, Watch{Workflow: "a.yml", Branch: "main"})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(4)
		go func() { defer wg.Done(); do(s, "GET", keyed("/configure"), host, nil) }()
		go func() { defer wg.Done(); do(s, "GET", keyed("/api/row")+"&workflow=b.yml&branch=main", host, nil) }()
		go func() { defer wg.Done(); do(s, "GET", keyed("/"), host, nil) }()
		go func() { defer wg.Done(); do(s, "POST", keyed("/configure/refresh"), host, form()) }()
	}
	wg.Wait()
	if rec := do(s, "GET", keyed("/configure"), host, nil); rec.Code != 200 {
		t.Fatalf("configure after concurrent use: %d", rec.Code)
	}
}
