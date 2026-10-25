// SPDX-License-Identifier: Apache-2.0

package web

import (
	"context"
	"sync"
	"time"

	cistatus "github.com/OWNER/gh-ci-status"
)

// kept is what Configure read from GitHub for one repository: the listing
// (default branch, workflows, recent runs) and each status it showed. It is
// reused until the user presses Refresh, so a change on Configure comes back
// without reading GitHub again. Failed reads are not kept. The status page
// always reads fresh and replaces the statuses kept here, so both pages agree.
type kept struct {
	mu     sync.Mutex
	list   *listing
	status map[cistatus.Watch]RowStatus
}

// listing is what the Configure page lists.
type listing struct {
	readAt    time.Time
	def       string
	workflows []cistatus.Workflow
	recent    []cistatus.Run
	note      string // why other branches are missing, if they are
}

func (k *kept) listing() *listing {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.list
}

func (k *kept) setListing(l *listing) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.list = l
}

// disabled reports whether the kept listing has file turned off on GitHub,
// with GitHub's word for it.
func (k *kept) disabled(file string) (string, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.list == nil {
		return "", false
	}
	for _, w := range k.list.workflows {
		if f, ok := cistatus.WorkflowFile(w); ok && f == file && w.Disabled() {
			return w.State, true
		}
	}
	return "", false
}

func (k *kept) rowStatus(w cistatus.Watch) (RowStatus, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	st, ok := k.status[w]
	return st, ok
}

func (k *kept) setRowStatus(w cistatus.Watch, st RowStatus) {
	if st.State == cistatus.Error {
		return
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.status == nil {
		k.status = map[cistatus.Watch]RowStatus{}
	}
	k.status[w] = st
}

// clear forgets everything; the next Configure visit reads GitHub again.
func (k *kept) clear() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.list, k.status = nil, nil
}

// readListing reads what Configure lists, its requests at the same time. Only
// the recent runs may fail; the page then lists no other branches.
func readListing(ctx context.Context, c *cistatus.Client, repo string, now time.Time) (*listing, error) {
	l := &listing{readAt: now}
	var defErr, wfErr, recentErr error
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); l.def, defErr = c.DefaultBranch(ctx, repo) }()
	go func() { defer wg.Done(); l.workflows, wfErr = c.Workflows(ctx, repo) }()
	go func() { defer wg.Done(); l.recent, recentErr = c.RecentRuns(ctx, repo, 2) }()
	wg.Wait()
	if defErr != nil {
		return nil, defErr
	}
	if wfErr != nil {
		return nil, wfErr
	}
	if recentErr != nil {
		l.note = "Other branches unavailable: " + recentErr.Error()
	}
	return l, nil
}
