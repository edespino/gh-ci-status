// SPDX-License-Identifier: Apache-2.0

package web

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cistatus "github.com/edespino/gh-ci-status"
)

func TestSetupShowsLogin(t *testing.T) {
	s, _, _ := newTestServer(t)
	rec := do(s, "GET", keyed("/setup"), host, nil)
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, "Working:</b> signed in as octocat") {
		t.Fatalf("code %d body %s", rec.Code, body)
	}
	if !strings.Contains(body, "<h1>Settings</h1>") || !strings.Contains(body, `<h2 id="repositories">Repositories</h2>`) {
		t.Error("the page is Settings, with an anchor on Repositories")
	}
	if !strings.Contains(body, "No repository yet") || strings.Contains(body, `class="done"`) {
		t.Error("with nothing set up, Setup asks for a repository and offers no Done")
	}
	if !strings.Contains(body, `action="/setup/add?key=`+testKey+`"`) || !strings.Contains(body, `name="repo"`) {
		t.Error("no add form")
	}
}

func TestSetupShowsLoginProblem(t *testing.T) {
	s, _, _ := newTestServer(t)
	s.client = func(context.Context) (*Client, error) {
		return nil, errors.New("gh not found on PATH; install the GitHub CLI")
	}
	body := do(s, "GET", keyed("/setup"), host, nil).Body.String()
	if !strings.Contains(body, "Not working:</b> gh not found on PATH") || !strings.Contains(body, "Run &#39;gh auth login&#39;") {
		t.Fatalf("body %s", body)
	}
}

// Adding saves GitHub's spelling of the name, whatever form was typed, and
// opens Configure for it.
func TestSetupAddRepo(t *testing.T) {
	s, f, _ := newTestServer(t)
	f.Handle("/repos/Example-Org/example-repo", 200, `{"full_name":"`+testRepo+`","default_branch":"main"}`)
	serveRepo(f, "b.yml")
	rec := do(s, "POST", keyed("/setup/add"), host, url.Values{"repo": {" https://github.com/Example-Org/example-repo.git "}})
	if rec.Code != 303 || rec.Header().Get("Location") != "/configure?key="+testKey+"&repo="+url.QueryEscape(testRepo) {
		t.Fatalf("code %d location %q body %s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	if got := loadConfig(t, s).RepoNames(); len(got) != 1 || got[0] != testRepo {
		t.Fatalf("repos %v", got)
	}
	// Adding it again changes nothing.
	do(s, "POST", keyed("/setup/add"), host, url.Values{"repo": {testRepo}})
	if got := loadConfig(t, s).RepoNames(); len(got) != 1 {
		t.Fatalf("repos after second add %v", got)
	}
}

func TestSetupAddRepoProblems(t *testing.T) {
	s, f, _ := newTestServer(t)
	f.Handle("/repos/o/empty", 200, `{"full_name":"o/empty","default_branch":"main"}`)
	f.Handle("/repos/o/empty/actions/workflows?per_page=100", 200, `{"workflows":[]}`)
	for typed, want := range map[string]string{
		"not a repo": "owner/name",
		"o/missing":  "not found, or your GitHub login cannot read it",
		"o/empty":    "has no GitHub Actions workflows",
	} {
		rec := do(s, "POST", keyed("/setup/add"), host, url.Values{"repo": {typed}})
		body := rec.Body.String()
		if rec.Code != 400 || !strings.Contains(body, want) || !strings.Contains(body, `value="`+typed+`"`) {
			t.Errorf("%q: code %d body %s", typed, rec.Code, body)
		}
	}
	if _, err := os.Stat(filepath.Join(s.dir, "config.json")); !errors.Is(err, os.ErrNotExist) {
		t.Error("a failed add must not save anything")
	}
}

func TestSetupListsAndRemovesRepos(t *testing.T) {
	s, _, _ := newTestServer(t)
	cfg := Config{}.AddWatches(testRepo, Watch{Workflow: "b.yml", Branch: "main"}).AddWatches("o/other", Watch{Workflow: "c.yml", Branch: "dev"})
	if err := cistatus.SaveConfig(s.dir, cfg); err != nil {
		t.Fatal(err)
	}
	s.keptFor("o/other").setRowStatus(Watch{Workflow: "c.yml", Branch: "dev"}, RowStatus{State: Passing})
	body := do(s, "GET", keyed("/setup"), host, nil).Body.String()
	for _, want := range []string{testRepo, "o/other", `href="/configure?key=` + testKey + `&amp;repo=o%2fother"`, `class="done"`, `action="/setup/remove?key=` + testKey + `"`, "data-confirm="} {
		if !strings.Contains(body, want) {
			t.Errorf("setup lacks %s", want)
		}
	}
	rec := do(s, "POST", keyed("/setup/remove"), host, url.Values{"repo": {"o/other"}})
	if rec.Code != 303 || rec.Header().Get("Location") != "/setup?key="+testKey {
		t.Fatalf("remove: code %d location %q", rec.Code, rec.Header().Get("Location"))
	}
	if got := loadConfig(t, s).RepoNames(); len(got) != 1 || got[0] != testRepo {
		t.Fatalf("repos %v", got)
	}
	if _, ok := s.keptFor("o/other").rowStatus(Watch{Workflow: "c.yml", Branch: "dev"}); ok {
		t.Error("removing a repository should forget what was read for it")
	}
	before, _ := os.ReadFile(filepath.Join(s.dir, "config.json"))
	if rec := do(s, "POST", keyed("/setup/remove"), host, url.Values{"repo": {"o/never"}}); rec.Code != 400 {
		t.Errorf("remove unknown: %d", rec.Code)
	}
	if after, _ := os.ReadFile(filepath.Join(s.dir, "config.json")); !bytes.Equal(before, after) {
		t.Error("removing an unknown repository changed the config")
	}
}
