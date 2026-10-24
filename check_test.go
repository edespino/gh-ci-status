// SPDX-License-Identifier: Apache-2.0

package cistatus

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/OWNER/ci-status/internal/fakegithub"
)

func TestCheckGH(t *testing.T) {
	ok := CheckGH(func(string) (string, error) { return "/usr/bin/gh", nil })
	if !ok.OK || !strings.Contains(ok.Detail, "/usr/bin/gh") || ok.Fix != "" {
		t.Fatalf("found: %+v", ok)
	}
	missing := CheckGH(func(string) (string, error) { return "", exec.ErrNotFound })
	if missing.OK || !strings.Contains(missing.Fix, "https://cli.github.com") {
		t.Fatalf("missing: %+v", missing)
	}
}

func TestCheckLogin(t *testing.T) {
	c, token := CheckLogin(context.Background(), func(context.Context) (string, error) { return "tkn", nil })
	if !c.OK || token != "tkn" || strings.Contains(c.Detail, "tkn") {
		t.Fatalf("logged in: %+v %q", c, token)
	}
	c, token = CheckLogin(context.Background(), func(context.Context) (string, error) { return "", errors.New("no token") })
	if c.OK || token != "" || !strings.Contains(c.Fix, "gh auth login") {
		t.Fatalf("logged out: %+v", c)
	}
}

func TestCheckAPI(t *testing.T) {
	f := fakegithub.New(t)
	f.Handle("/user", 200, `{"login":"octocat"}`)
	if c := CheckAPI(context.Background(), testClient(f)); !c.OK || !strings.Contains(c.Detail, "octocat") {
		t.Fatalf("ok: %+v", c)
	}
	g := fakegithub.New(t)
	g.Handle("/user", 401, `{"message":"Bad credentials"}`)
	if c := CheckAPI(context.Background(), testClient(g)); c.OK || !strings.Contains(c.Fix, "gh auth login") {
		t.Fatalf("rejected: %+v", c)
	}
}

func TestCheckRepo(t *testing.T) {
	f := fakegithub.New(t)
	f.Handle("/repos/Octo-Org/octo-repo", 200, `{"full_name":"octo-org/octo-repo","default_branch":"main"}`)
	f.Handle("/repos/octo-org/octo-repo/actions/workflows?per_page=100", 200,
		`{"workflows":[{"name":"A","path":".github/workflows/a.yml"},{"name":"B","path":".github/workflows/b.yml"},`+
			`{"name":"CodeQL","path":"dynamic/github-code-scanning/codeql"}]}`)
	f.Handle("/repos/o/empty", 200, `{"full_name":"o/empty","default_branch":"main"}`)
	// GitHub lists generated workflows too; they have no file to watch.
	f.Handle("/repos/o/empty/actions/workflows?per_page=100", 200, `{"workflows":[{"name":"CodeQL","path":"dynamic/github-code-scanning/codeql"}]}`)
	f.Handle("/repos/o/broken", 500, `{"message":"boom"}`)
	c := testClient(f)

	ok, name := CheckRepo(context.Background(), c, "Octo-Org/octo-repo")
	if !ok.OK || name != "octo-org/octo-repo" || !strings.Contains(ok.Detail, "2 workflows") || !strings.Contains(ok.Detail, "main") {
		t.Fatalf("ok: %+v %q", ok, name)
	}
	for _, tc := range []struct{ repo, detail, fix string }{
		{"not a repo", "owner/name", "owner/name"},
		{"o/missing", "not found", "can read it"},
		{"o/empty", "no GitHub Actions workflows", ""},
		{"o/broken", "500", ""},
	} {
		got, name := CheckRepo(context.Background(), c, tc.repo)
		if got.OK || name != "" || !strings.Contains(got.Detail, tc.detail) || !strings.Contains(got.Fix, tc.fix) {
			t.Errorf("%s: %+v %q", tc.repo, got, name)
		}
	}
	if n := len(f.Requests()); n != 6 {
		t.Errorf("requests = %d (%v); an invalid name must not reach GitHub", n, f.Requests())
	}
}

func TestWorkflowFile(t *testing.T) {
	for path, want := range map[string]string{
		".github/workflows/build.yml":         "build.yml",
		".github/workflows/x.yaml":            "x.yaml",
		"dynamic/github-code-scanning/codeql": "",
		".github/workflows/nested/build.yml":  "",
		".github/workflows/../../etc/x.yml":   "",
		"build.yml":                           "",
	} {
		got, ok := WorkflowFile(Workflow{Path: path})
		if got != want || ok != (want != "") {
			t.Errorf("WorkflowFile(%q) = %q, %v; want %q", path, got, ok, want)
		}
	}
}
