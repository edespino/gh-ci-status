// SPDX-License-Identifier: Apache-2.0

package web

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	cistatus "github.com/edespino/gh-ci-status"
)

// WatchItem is one watched workflow in the Configure page's Watching list.
type WatchItem struct {
	File, Name string
	Missing    bool // watched, but GitHub no longer lists the file
	Branches   []string
}

// ConfigureItem is one workflow in the Add list. Branches holds the other
// branches (not the default) that recently ran it, each with its latest result.
type ConfigureItem struct {
	File, Name, State string
	WatchedOnDefault  bool // watched on the default branch: no + main, and the "Watch" buttons skip it
	Branches          []BranchStatus
}

// BranchStatus is one branch's newest run of a workflow.
type BranchStatus struct {
	Name       string
	State      cistatus.State
	Conclusion string // for Other
	RunURL     string
	RunNumber  int
	Watched    bool
}

// configureModel builds the Watching list (config order, grouped per file)
// and the Add list (every workflow file, sorted by name). Branch results come
// from the repo's recent runs, newest first, from this repo only and not from
// pull_request runs, the same rule the status page uses.
func configureModel(workflows []cistatus.Workflow, recent []cistatus.Run, repo string, watched []cistatus.Watch, defaultBranch string) ([]WatchItem, []ConfigureItem) {
	names := map[string]string{}
	var items []ConfigureItem
	for _, wf := range workflows {
		file, ok := cistatus.WorkflowFile(wf)
		if !ok {
			continue
		}
		name := wf.Name
		if name == "" {
			name = file
		}
		names[file] = name
		items = append(items, ConfigureItem{File: file, Name: name, State: wf.State})
	}

	var watching []WatchItem
	index := map[string]int{}
	isWatched := map[cistatus.Watch]bool{}
	for _, w := range watched {
		isWatched[w] = true
		i, ok := index[w.Workflow]
		if !ok {
			name, present := names[w.Workflow]
			if !present {
				name = w.Workflow
			}
			watching = append(watching, WatchItem{File: w.Workflow, Name: name, Missing: !present})
			i = len(watching) - 1
			index[w.Workflow] = i
		}
		if !slices.Contains(watching[i].Branches, w.Branch) {
			watching[i].Branches = append(watching[i].Branches, w.Branch)
		}
	}

	runs := append([]cistatus.Run(nil), recent...)
	sort.SliceStable(runs, func(i, j int) bool {
		if !runs[i].CreatedAt.Equal(runs[j].CreatedAt) {
			return runs[i].CreatedAt.After(runs[j].CreatedAt)
		}
		return runs[i].ID > runs[j].ID
	})
	seen := map[cistatus.Watch]bool{}
	branches := map[string][]BranchStatus{}
	for _, r := range runs {
		file, isFile := cistatus.WorkflowFile(cistatus.Workflow{Path: r.Path})
		key := cistatus.Watch{Workflow: file, Branch: r.HeadBranch}
		if !cistatus.OwnRun(r, repo) || !isFile ||
			r.HeadBranch == "" || r.HeadBranch == defaultBranch || seen[key] {
			continue
		}
		seen[key] = true
		branches[file] = append(branches[file], branchStatus(r, isWatched[key]))
	}

	for i := range items {
		items[i].WatchedOnDefault = isWatched[cistatus.Watch{Workflow: items[i].File, Branch: defaultBranch}]
		items[i].Branches = branches[items[i].File]
	}
	sort.SliceStable(items, func(i, j int) bool { return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name) })
	return watching, items
}

func branchStatus(r cistatus.Run, watched bool) BranchStatus {
	b := BranchStatus{Name: r.HeadBranch, RunURL: r.HTMLURL, RunNumber: r.RunNumber, Watched: watched, State: cistatus.RunState(r)}
	if b.State == cistatus.Other {
		b.Conclusion = r.Conclusion
	}
	return b
}

// RowStatus is what the Configure page's status dots receive from /api/row.
type RowStatus struct {
	State cistatus.State `json:"state"`
	Text  string         `json:"text"`
	URL   string         `json:"url"`
}

func rowStatus(r cistatus.Row) RowStatus {
	s := RowStatus{State: r.State, URL: cistatus.HistoryURL(r.Repo, r.Watch.Workflow, r.Watch.Branch)}
	if r.Latest != nil {
		s.URL = r.Latest.HTMLURL
	}
	switch r.State {
	case cistatus.Failing:
		s.Text = fmt.Sprintf("failing since %s (#%d)", r.RedSince.In(time.Local).Format("Jan 2"), r.Latest.RunNumber)
	case cistatus.Passing:
		s.Text = "passing · " + localTime(started(r.Latest))
	case cistatus.Running:
		s.Text = fmt.Sprintf("running #%d", r.Latest.RunNumber)
	case cistatus.Other:
		s.Text = fmt.Sprintf("%s #%d", r.Conclusion, r.Latest.RunNumber)
	case cistatus.None:
		s.Text = "no run on " + r.Watch.Branch
	case cistatus.Error:
		s.Text = r.Err
	case cistatus.Disabled:
		s.Text = "turned off on GitHub (" + r.WorkflowState + ")"
	}
	if r.InProgress != nil && r.State != cistatus.Running {
		s.Text += fmt.Sprintf(" · running #%d", r.InProgress.RunNumber)
	}
	return s
}
