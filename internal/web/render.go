// SPDX-License-Identifier: Apache-2.0

package web

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"strings"
	"time"

	cistatus "github.com/edespino/gh-ci-status"
	"github.com/edespino/gh-ci-status/internal/banner"
)

//go:embed templates/*.html
var templateFS embed.FS

var templates = template.Must(template.New("").Funcs(template.FuncMap{
	"localTime":  localTime,
	"historyURL": cistatus.HistoryURL,
	"plural":     plural,
	"outcome":    outcome,
	"started":    started,
	"gapAt":      gapAt,
	"stateClass": stateClass,
	"wordmark":   func() template.HTML { return wordmarkHTML },
}).ParseFS(templateFS, "templates/*.html"))

// wordmarkHTML is the logo on every page; it is our own markup.
var wordmarkHTML = template.HTML(banner.SVG(false))

// StatusPage is the status of every watched pair, reds first.
type StatusPage struct {
	Repos              []string
	Summary, Generated string
	Error              string
	Key                string // carried in every link and form action
	Rows               []cistatus.Row
}

// MultiRepo is true when rows need their repository named.
func (p StatusPage) MultiRepo() bool { return len(p.Repos) > 1 }

// RepoList names the repositories for the header.
func (p StatusPage) RepoList() string { return strings.Join(p.Repos, ", ") }

// FilterButton is one state button above the status rows.
type FilterButton struct {
	State cistatus.State
	Label string
	Count int
}

// Filters are the state buttons for the states present, in row order.
func (p StatusPage) Filters() []FilterButton {
	counts := map[cistatus.State]int{}
	for _, r := range p.Rows {
		counts[r.State]++
	}
	var out []FilterButton
	for _, s := range []struct {
		state cistatus.State
		label string
	}{
		{cistatus.Failing, "Red"}, {cistatus.Error, "Unreadable"}, {cistatus.Running, "Running"}, {cistatus.Other, "Other"},
		{cistatus.None, "No runs"}, {cistatus.Passing, "Passing"}, {cistatus.Disabled, "Disabled"},
	} {
		if counts[s.state] > 0 {
			out = append(out, FilterButton{State: s.state, Label: s.label, Count: counts[s.state]})
		}
	}
	return out
}

// ConfigurePage chooses what to watch in one repository.
type ConfigurePage struct {
	Repo          string
	Repos         []string // every set-up repository, for switching
	DefaultBranch string
	Error         string
	Key           string // carried in every form action and link
	ReadAt        string // when the kept GitHub data was read
	Watching      []WatchItem
	Items         []ConfigureItem
}

func renderStatus(w io.Writer, p StatusPage) error {
	return templates.ExecuteTemplate(w, "status.html", p)
}

func renderConfigure(w io.Writer, p ConfigurePage) error {
	return templates.ExecuteTemplate(w, "configure.html", p)
}

func renderSetup(w io.Writer, p SetupPage) error {
	return templates.ExecuteTemplate(w, "setup.html", p)
}

// localTime: "Oct 23, 5:00 AM PDT" in this machine's time zone.
func localTime(t time.Time) string {
	return t.In(time.Local).Format("Jan 2, 3:04 PM MST")
}

// readTime is when Configure read GitHub: the time of day for today, with the
// date otherwise.
func readTime(t, now time.Time) string {
	t, now = t.Local(), now.Local()
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return t.Format("3:04 PM")
	}
	return t.Format("Jan 2, 3:04 PM")
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func started(r *cistatus.Run) time.Time {
	if r.RunStartedAt.IsZero() {
		return r.CreatedAt
	}
	return r.RunStartedAt
}

// outcome names a run's result in the page's words.
func outcome(r *cistatus.Run) string {
	switch cistatus.RunState(*r) {
	case cistatus.Passing:
		return "passing"
	case cistatus.Failing:
		return "failing"
	case cistatus.Running:
		return r.Status
	}
	return r.Conclusion
}

func redish(r cistatus.Row) bool { return r.State == cistatus.Failing || r.State == cistatus.Error }

// gapAt is true where the red rows end and the rest begin.
func gapAt(rows []cistatus.Row, i int) bool {
	return i > 0 && redish(rows[i-1]) && !redish(rows[i])
}

func stateClass(s cistatus.State) string {
	switch s {
	case cistatus.Failing, cistatus.Error:
		return "bad"
	case cistatus.Running:
		return "run"
	case cistatus.Passing:
		return "ok"
	case cistatus.Disabled:
		return "off"
	}
	return "other"
}
