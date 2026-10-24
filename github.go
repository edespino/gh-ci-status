// SPDX-License-Identifier: Apache-2.0

package cistatus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultAPIURL is the GitHub REST API for github.com.
const DefaultAPIURL = "https://api.github.com"

// Client reads the GitHub REST API with the caller's token. It never writes.
type Client struct {
	BaseURL string // DefaultAPIURL, or a GitHub Enterprise Server API URL
	Token   string
	HTTP    *http.Client
}

// NewClient returns a Client for baseURL that sends token and gives up on a
// request after 15 seconds.
func NewClient(token, baseURL string) *Client {
	return &Client{BaseURL: baseURL, Token: token, HTTP: &http.Client{Timeout: 15 * time.Second}}
}

// Workflow is a workflow GitHub has registered for a repository.
type Workflow struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Path  string `json:"path"`
	State string `json:"state"`
}

// Run is one run of a workflow.
type Run struct {
	ID             int64     `json:"id"`
	RunNumber      int       `json:"run_number"`
	Name           string    `json:"name"`
	Path           string    `json:"path"`
	HeadBranch     string    `json:"head_branch"`
	Event          string    `json:"event"`
	Status         string    `json:"status"`
	Conclusion     string    `json:"conclusion"`
	HTMLURL        string    `json:"html_url"`
	CreatedAt      time.Time `json:"created_at"`
	RunStartedAt   time.Time `json:"run_started_at"`
	HeadRepository struct {
		FullName string `json:"full_name"`
	} `json:"head_repository"`
}

// Job is one job of a run.
type Job struct {
	Name       string `json:"name"`
	Conclusion string `json:"conclusion"`
	HTMLURL    string `json:"html_url"`
	Steps      []Step `json:"steps"`
}

// Step is one step of a job.
type Step struct {
	Name       string `json:"name"`
	Conclusion string `json:"conclusion"`
}

// Repository is what ci-status reads about a repository.
type Repository struct {
	FullName      string `json:"full_name"` // GitHub's spelling, e.g. "octo-org/octo-repo"
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
}

// Repository reads repo ("owner/name"). GitHub answers 404 both when it does
// not exist and when the token cannot read it.
func (c *Client) Repository(ctx context.Context, repo string) (Repository, error) {
	var r Repository
	_, err := c.get(ctx, "/repos/"+repo, &r)
	return r, err
}

// DefaultBranch returns repo's default branch.
func (c *Client) DefaultBranch(ctx context.Context, repo string) (string, error) {
	r, err := c.Repository(ctx, repo)
	return r.DefaultBranch, err
}

// User returns the login of the account the token belongs to.
func (c *Client) User(ctx context.Context) (string, error) {
	var u struct {
		Login string `json:"login"`
	}
	_, err := c.get(ctx, "/user", &u)
	return u.Login, err
}

// Workflow reads one workflow by its file name, e.g. "build.yml".
func (c *Client) Workflow(ctx context.Context, repo, file string) (Workflow, error) {
	var w Workflow
	_, err := c.get(ctx, "/repos/"+repo+"/actions/workflows/"+file, &w)
	return w, err
}

// Runs returns the newest runs (up to 100) of a workflow on one branch, and
// the total number of such runs GitHub reports.
func (c *Client) Runs(ctx context.Context, repo, file, branch string) ([]Run, int, error) {
	q := url.Values{"branch": {branch}, "per_page": {"100"}, "exclude_pull_requests": {"true"}}
	var r struct {
		TotalCount   int   `json:"total_count"`
		WorkflowRuns []Run `json:"workflow_runs"`
	}
	_, err := c.get(ctx, "/repos/"+repo+"/actions/workflows/"+file+"/runs?"+q.Encode(), &r)
	return r.WorkflowRuns, r.TotalCount, err
}

// Jobs returns the jobs of a run, with their steps.
func (c *Client) Jobs(ctx context.Context, repo string, runID int64) ([]Job, error) {
	var r struct {
		Jobs []Job `json:"jobs"`
	}
	_, err := c.get(ctx, "/repos/"+repo+"/actions/runs/"+strconv.FormatInt(runID, 10)+"/jobs?per_page=100", &r)
	return r.Jobs, err
}

// Workflows lists every workflow GitHub has registered for the repo,
// following pagination.
func (c *Client) Workflows(ctx context.Context, repo string) ([]Workflow, error) {
	var all []Workflow
	next := "/repos/" + repo + "/actions/workflows?per_page=100"
	for next != "" {
		var page struct {
			Workflows []Workflow `json:"workflows"`
		}
		h, err := c.get(ctx, next, &page)
		if err != nil {
			return nil, err
		}
		all = append(all, page.Workflows...)
		next, err = c.nextPath(h.Get("Link"))
		if err != nil {
			return nil, err
		}
	}
	return all, nil
}

// RecentRuns returns the repo's newest runs across all workflows and
// branches, `pages` pages of 100. The pages are requested at the same time
// (each takes seconds on GitHub); a short page ends the list, and later
// pages' runs or errors are ignored.
func (c *Client) RecentRuns(ctx context.Context, repo string, pages int) ([]Run, error) {
	runs := make([][]Run, pages)
	errs := make([]error, pages)
	var wg sync.WaitGroup
	for p := 0; p < pages; p++ {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			q := url.Values{"per_page": {"100"}, "page": {strconv.Itoa(p + 1)}}
			var r struct {
				WorkflowRuns []Run `json:"workflow_runs"`
			}
			_, errs[p] = c.get(ctx, "/repos/"+repo+"/actions/runs?"+q.Encode(), &r)
			runs[p] = r.WorkflowRuns
		}(p)
	}
	wg.Wait()
	var all []Run
	for p := range runs {
		if errs[p] != nil {
			return nil, errs[p]
		}
		all = append(all, runs[p]...)
		if len(runs[p]) < 100 {
			break
		}
	}
	return all, nil
}

var nextLink = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

// nextPath turns a Link header's rel="next" URL into a path on BaseURL. A
// next URL on any other host is refused so the token never leaves BaseURL.
func (c *Client) nextPath(link string) (string, error) {
	m := nextLink.FindStringSubmatch(link)
	if m == nil {
		return "", nil
	}
	if !strings.HasPrefix(m[1], c.BaseURL+"/") {
		return "", fmt.Errorf("GitHub returned a next page outside %s; not following it", c.BaseURL)
	}
	return strings.TrimPrefix(m[1], c.BaseURL), nil
}

// get fetches BaseURL+path into v. Errors name the path and status but
// never the token.
func (c *Client) get(ctx context.Context, path string, v any) (http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %v", path, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		var ne net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
			return nil, fmt.Errorf("GET %s: timed out", path)
		}
		return nil, fmt.Errorf("GET %s: %v", path, redact(err, c.Token))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("GET %s: reading response: %v", path, err)
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(body, &e)
		return nil, &APIError{Path: path, Status: resp.StatusCode, Message: e.Message}
	}
	if err := json.Unmarshal(body, v); err != nil {
		return nil, fmt.Errorf("GET %s: bad JSON: %v", path, err)
	}
	return resp.Header, nil
}

func redact(err error, token string) string {
	if token == "" {
		return err.Error()
	}
	return strings.ReplaceAll(err.Error(), token, "[token]")
}

// APIError is a GitHub answer other than success: the request path, the HTTP
// status and GitHub's message. It never holds the token.
type APIError struct {
	Path    string
	Status  int
	Message string
}

func (e *APIError) Error() string {
	if e.Status == http.StatusUnauthorized {
		return fmt.Sprintf("GET %s: 401: GitHub rejected the token; run 'gh auth login'", e.Path)
	}
	return fmt.Sprintf("GET %s: %d %s", e.Path, e.Status, e.Message)
}
