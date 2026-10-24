// SPDX-License-Identifier: Apache-2.0

package cistatus

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// State is a watched pair's current condition.
type State string

// The states, in the order SortRows puts them.
const (
	Failing State = "failing"
	Error   State = "error"
	Running State = "running"
	Other   State = "other"
	None    State = "none"
	Passing State = "passing"
)

// StepRef points at the failing job's log; Label is "step (job)", or the
// job name when no single step failed.
type StepRef struct {
	URL, Label string
}

// Row is the current state of one watched workflow + branch pair.
type Row struct {
	Repo       string
	Watch      Watch
	Name       string
	State      State
	Conclusion string // for Other: GitHub's conclusion word
	Latest     *Run
	Previous   *Run // for Running: newest completed run
	LastGreen  *Run // for Failing: newest success before the red streak
	RedSince   time.Time
	Age        string // "71 days"
	FailedRuns int
	AllHistory bool // for Failing with no green: the fetch covered every run
	FailedStep *StepRef
	Err        string
}

var (
	prEvent = map[string]bool{"pull_request": true, "pull_request_target": true}
	green   = map[string]bool{"success": true}
	red     = map[string]bool{"failure": true, "timed_out": true, "startup_failure": true}
)

// Evaluate decides a row from the runs GitHub returned for the watch's
// branch; total is the number of such runs GitHub reported. Only runs whose
// head branch is the watch's branch and whose head repository is repo (in any
// letter case, as GitHub compares names) count:
// runs from forks never decide a row, and neither do pull_request runs (they
// test a merge into another branch).
func Evaluate(repo string, w Watch, name string, runs []Run, total int, now time.Time) Row {
	row := Row{Repo: repo, Watch: w, Name: name}
	var mine []Run
	for _, r := range runs {
		if r.HeadBranch == w.Branch && OwnRun(r, repo) {
			mine = append(mine, r)
		}
	}
	sort.SliceStable(mine, func(i, j int) bool { return newer(mine[i], mine[j]) })
	if len(mine) == 0 {
		row.State = None
		return row
	}
	var done []Run
	for _, r := range mine {
		if r.Status == "completed" {
			done = append(done, r)
		}
	}
	latest := mine[0]
	row.Latest = &latest
	switch {
	case latest.Status != "completed":
		row.State = Running
		if len(done) > 0 {
			row.Previous = &done[0]
		}
	case green[latest.Conclusion]:
		row.State = Passing
	case red[latest.Conclusion]:
		row.State = Failing
		for i := range done {
			if green[done[i].Conclusion] {
				row.LastGreen = &done[i]
				break
			}
		}
		for _, r := range done {
			if !red[r.Conclusion] || (row.LastGreen != nil && !newer(r, *row.LastGreen)) {
				continue
			}
			row.FailedRuns++
			if row.RedSince.IsZero() || r.CreatedAt.Before(row.RedSince) {
				row.RedSince = r.CreatedAt
			}
		}
		row.Age = FormatAge(now.Sub(row.RedSince))
		row.AllHistory = len(runs) >= total
	default:
		row.State = Other
		row.Conclusion = latest.Conclusion
	}
	return row
}

func newer(a, b Run) bool {
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.After(b.CreatedAt)
	}
	return a.ID > b.ID
}

// OwnRun reports whether r is repo's own run: started in repo itself (not a
// fork; names compare in any letter case, as on GitHub) and not by a
// pull_request or pull_request_target event, which tests a merge into another
// branch rather than the branch itself.
func OwnRun(r Run, repo string) bool {
	return strings.EqualFold(r.HeadRepository.FullName, repo) && !prEvent[r.Event]
}

// RunState classifies one run on its own: Running until it completes, then
// Passing, Failing or Other by its conclusion.
func RunState(r Run) State {
	switch {
	case r.Status != "completed":
		return Running
	case green[r.Conclusion]:
		return Passing
	case red[r.Conclusion]:
		return Failing
	}
	return Other
}

// HistoryURL is the workflow's run list on GitHub, filtered to one branch.
func HistoryURL(repo, file, branch string) string {
	return "https://github.com/" + repo + "/actions/workflows/" + file + "?query=" + url.QueryEscape("branch:"+branch)
}

// FormatAge says how long d is in its largest whole unit: "71 days",
// "5 hours", "1 minute", "under a minute".
func FormatAge(d time.Duration) string {
	for _, u := range []struct {
		name string
		size time.Duration
	}{{"day", 24 * time.Hour}, {"hour", time.Hour}, {"minute", time.Minute}} {
		if n := int(d / u.size); n >= 1 {
			return plural(n, u.name)
		}
	}
	return "under a minute"
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

var order = map[State]int{Failing: 0, Error: 1, Running: 2, Other: 3, None: 4, Passing: 5}

// SortRows puts reds first (oldest red at the top), then errors, running,
// other conclusions, no runs, passing; ties keep their order. rows is not
// modified.
func SortRows(rows []Row) []Row {
	out := append([]Row(nil), rows...)
	sort.SliceStable(out, func(i, j int) bool {
		if order[out[i].State] != order[out[j].State] {
			return order[out[i].State] < order[out[j].State]
		}
		return out[i].State == Failing && out[i].RedSince.Before(out[j].RedSince)
	})
	return out
}

// Summary is one short line: how many are red (and the oldest red's age),
// then the other states present, passing last.
func Summary(rows []Row) string {
	if len(rows) == 0 {
		return "nothing watched"
	}
	counts := map[State]int{}
	var oldest *Row
	for i, r := range rows {
		counts[r.State]++
		if r.State == Failing && (oldest == nil || r.RedSince.Before(oldest.RedSince)) {
			oldest = &rows[i]
		}
	}
	parts := []string{"nothing red"}
	if oldest != nil {
		parts[0] = fmt.Sprintf("%d red, oldest %s", counts[Failing], oldest.Age)
	}
	for _, s := range []struct {
		state State
		text  func(n int) string
	}{
		{Running, func(n int) string { return fmt.Sprintf("%d running", n) }},
		{Error, func(n int) string { return fmt.Sprintf("%d unreadable", n) }},
		{Other, func(n int) string { return plural(n, "other result") }},
		{None, func(n int) string { return fmt.Sprintf("%d with no runs", n) }},
		{Passing, func(n int) string { return fmt.Sprintf("%d passing", n) }},
	} {
		if n := counts[s.state]; n > 0 {
			parts = append(parts, s.text(n))
		}
	}
	return strings.Join(parts, " · ")
}
