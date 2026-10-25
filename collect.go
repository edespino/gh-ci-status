// SPDX-License-Identifier: Apache-2.0

package cistatus

import (
	"context"
	"sync"
	"time"
)

// maxParallel bounds concurrent GitHub requests.
const maxParallel = 8

// Collect reads GitHub for every watch in repo and returns sorted rows. Each
// workflow file's name is looked up once; jobs are read only for red rows. A
// failed read turns that watch into an Error row and leaves the others alone.
// A workflow turned off on GitHub gives a Disabled row.
func Collect(ctx context.Context, c *Client, repo string, watches []Watch, now time.Time) []Row {
	var files []string
	seen := map[string]bool{}
	for _, w := range watches {
		if !seen[w.Workflow] {
			seen[w.Workflow] = true
			files = append(files, w.Workflow)
		}
	}

	var mu sync.Mutex
	workflows := map[string]Workflow{}
	lookupErr := map[string]error{}
	parallel(len(files), func(i int) {
		wf, err := c.Workflow(ctx, repo, files[i])
		mu.Lock()
		defer mu.Unlock()
		workflows[files[i]], lookupErr[files[i]] = wf, err
	})

	rows := make([]Row, len(watches))
	parallel(len(watches), func(i int) {
		w := watches[i]
		wf := workflows[w.Workflow]
		name := wf.Name
		if name == "" {
			name = w.Workflow
		}
		if err := lookupErr[w.Workflow]; err != nil {
			rows[i] = Row{Repo: repo, Watch: w, Name: name, State: Error, Err: err.Error()}
			return
		}
		if wf.Disabled() {
			rows[i] = disabledRow(ReadRow(ctx, c, repo, w, name, now, false), wf.State)
			return
		}
		rows[i] = ReadRow(ctx, c, repo, w, name, now, true)
	})
	return SortRows(rows)
}

// ReadRow reads one watch's runs and evaluates them, using name as the row's
// workflow name; withJobs also looks up the failed step of a red row (one more
// request).
func ReadRow(ctx context.Context, c *Client, repo string, w Watch, name string, now time.Time, withJobs bool) Row {
	runs, total, err := c.Runs(ctx, repo, w.Workflow, w.Branch)
	if err != nil {
		return Row{Repo: repo, Watch: w, Name: name, State: Error, Err: err.Error()}
	}
	row := Evaluate(repo, w, name, runs, total, now)
	if withJobs && row.State == Failing {
		row.FailedStep = failedStep(ctx, c, repo, row.Latest.ID)
	}
	return row
}

// disabledRow keeps only what a turned-off workflow's row shows: its name and
// its last run.
func disabledRow(r Row, workflowState string) Row {
	return Row{Repo: r.Repo, Watch: r.Watch, Name: r.Name, State: Disabled, WorkflowState: workflowState, Latest: r.Latest}
}

// failedStep returns the first red job of a run and its first red step, or
// nil when the jobs cannot be read or none is red.
func failedStep(ctx context.Context, c *Client, repo string, runID int64) *StepRef {
	jobs, err := c.Jobs(ctx, repo, runID)
	if err != nil {
		return nil
	}
	for _, j := range jobs {
		if !red[j.Conclusion] {
			continue
		}
		for _, s := range j.Steps {
			if red[s.Conclusion] {
				return &StepRef{URL: j.HTMLURL, Label: s.Name + " (" + j.Name + ")"}
			}
		}
		return &StepRef{URL: j.HTMLURL, Label: j.Name}
	}
	return nil
}

// parallel runs fn(0..n-1) with at most maxParallel running at once.
func parallel(n int, fn func(i int)) {
	sem := make(chan struct{}, maxParallel)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			fn(i)
		}(i)
	}
	wg.Wait()
}
