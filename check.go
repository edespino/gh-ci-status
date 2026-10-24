// SPDX-License-Identifier: Apache-2.0

package cistatus

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Check is the result of one setup check, in words a person can act on: what
// was checked, what was found, and, when it failed, what to do.
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"`
}

// CheckGH looks for the GitHub CLI with lookPath (exec.LookPath outside
// tests). ci-status reads GitHub with the CLI's login.
func CheckGH(lookPath func(string) (string, error)) Check {
	path, err := lookPath("gh")
	if err != nil {
		return Check{Name: "GitHub CLI", Detail: "gh is not on PATH",
			Fix: "Install the GitHub CLI from https://cli.github.com, then run 'gh auth login'"}
	}
	return Check{Name: "GitHub CLI", OK: true, Detail: "found at " + path}
}

// CheckLogin asks token (GHToken outside tests) for the login's token and
// returns it with the result. The token never appears in the result.
func CheckLogin(ctx context.Context, token func(context.Context) (string, error)) (Check, string) {
	t, err := token(ctx)
	if err != nil {
		return Check{Name: "GitHub login", Detail: err.Error(), Fix: "Run 'gh auth login'"}, ""
	}
	return Check{Name: "GitHub login", OK: true, Detail: "gh has a token"}, t
}

// CheckAPI reads the signed-in account with c, which proves the network path
// and the token.
func CheckAPI(ctx context.Context, c *Client) Check {
	login, err := c.User(ctx)
	if err != nil {
		return Check{Name: "GitHub API", Detail: err.Error(),
			Fix: "Check your network connection; if GitHub rejected the token, run 'gh auth login'"}
	}
	return Check{Name: "GitHub API", OK: true, Detail: "signed in as " + login}
}

// CheckRepo reads repo and its workflows with c. On success it returns
// GitHub's spelling of the name ("Octo-Org/Repo" becomes "octo-org/repo" if
// that is how GitHub has it), which is what should be saved.
func CheckRepo(ctx context.Context, c *Client, repo string) (Check, string) {
	name := "Repository " + repo
	if !ValidRepo(repo) {
		return Check{Name: name, Detail: fmt.Sprintf("%q is not in owner/name form", repo),
			Fix: "Use the form owner/name, as in github.com/owner/name"}, ""
	}
	r, err := c.Repository(ctx, repo)
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
			return Check{Name: name, Detail: "not found, or your GitHub login cannot read it",
				Fix: "Check the spelling; for a private repository, sign in to gh with an account that can read it"}, ""
		}
		return Check{Name: name, Detail: err.Error()}, ""
	}
	workflows, err := c.Workflows(ctx, r.FullName)
	if err != nil {
		return Check{Name: name, Detail: err.Error()}, ""
	}
	files := 0
	for _, w := range workflows {
		if _, ok := WorkflowFile(w); ok {
			files++
		}
	}
	if files == 0 {
		return Check{Name: name, Detail: "has no GitHub Actions workflows in .github/workflows"}, ""
	}
	return Check{Name: "Repository " + r.FullName, OK: true,
		Detail: fmt.Sprintf("%s, default branch %s", plural(files, "workflow"), r.DefaultBranch)}, r.FullName
}

// String is the check as one or two lines for a terminal.
func (c Check) String() string {
	var b strings.Builder
	if c.OK {
		fmt.Fprintf(&b, "ok    %s: %s", c.Name, c.Detail)
	} else {
		fmt.Fprintf(&b, "FAIL  %s: %s", c.Name, c.Detail)
		if c.Fix != "" {
			fmt.Fprintf(&b, "\n      %s", c.Fix)
		}
	}
	return b.String()
}

// WorkflowFile is the file name of a workflow defined in .github/workflows,
// the only kind that can be watched. GitHub also lists generated workflows
// (such as CodeQL default setup) that have no such file; ok is false for them.
func WorkflowFile(w Workflow) (file string, ok bool) {
	const dir = ".github/workflows/"
	file = strings.TrimPrefix(w.Path, dir)
	if !strings.HasPrefix(w.Path, dir) || !ValidWorkflowFile(file) {
		return "", false
	}
	return file, true
}
