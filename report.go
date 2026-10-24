// SPDX-License-Identifier: Apache-2.0

package cistatus

import "time"

// Report is the machine-readable status: what `ci-status --json` prints and
// what an MCP tool returns. Field names are stable; new fields may be added.
type Report struct {
	Generated time.Time   `json:"generated"`
	Summary   string      `json:"summary"`
	Repos     []string    `json:"repos"`
	Rows      []ReportRow `json:"rows"`
}

// ReportRow is one watched workflow + branch pair.
type ReportRow struct {
	Repo       string     `json:"repo"`
	Workflow   string     `json:"workflow"`
	Name       string     `json:"name"`
	Branch     string     `json:"branch"`
	State      State      `json:"state"`
	Conclusion string     `json:"conclusion,omitempty"`  // for "other": GitHub's word
	RedSince   *time.Time `json:"red_since,omitempty"`   // failing: first red run after the last green
	Age        string     `json:"age,omitempty"`         // failing: how long it has been red
	FailedRuns int        `json:"failed_runs,omitempty"` // failing: red runs since the last green
	NeverGreen bool       `json:"never_green,omitempty"` // failing: no green run in its whole history
	FailedStep *Link      `json:"failed_step,omitempty"` // failing: first failed step and its log
	Latest     *RunRef    `json:"latest,omitempty"`
	Previous   *RunRef    `json:"previous,omitempty"`   // running: newest completed run
	LastGreen  *RunRef    `json:"last_green,omitempty"` // failing: newest success before the red runs
	HistoryURL string     `json:"history_url"`
	Error      string     `json:"error,omitempty"`
}

// Link is a labelled URL.
type Link struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// RunRef identifies a run.
type RunRef struct {
	Number     int       `json:"number"`
	URL        string    `json:"url"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// NewReport builds the report for repos from rows (sorted as SortRows sorts).
func NewReport(repos []string, rows []Row, now time.Time) Report {
	rep := Report{Generated: now.UTC(), Summary: Summary(rows), Repos: append([]string{}, repos...), Rows: []ReportRow{}}
	for _, r := range rows {
		out := ReportRow{
			Repo: r.Repo, Workflow: r.Watch.Workflow, Name: r.Name, Branch: r.Watch.Branch, State: r.State,
			Conclusion: r.Conclusion, Age: r.Age, FailedRuns: r.FailedRuns, Error: r.Err,
			Latest: runRef(r.Latest), Previous: runRef(r.Previous), LastGreen: runRef(r.LastGreen),
			HistoryURL: HistoryURL(r.Repo, r.Watch.Workflow, r.Watch.Branch),
			NeverGreen: r.State == Failing && r.LastGreen == nil && r.AllHistory,
		}
		if !r.RedSince.IsZero() {
			t := r.RedSince.UTC()
			out.RedSince = &t
		}
		if r.FailedStep != nil {
			out.FailedStep = &Link{Label: r.FailedStep.Label, URL: r.FailedStep.URL}
		}
		rep.Rows = append(rep.Rows, out)
	}
	return rep
}

func runRef(r *Run) *RunRef {
	if r == nil {
		return nil
	}
	return &RunRef{Number: r.RunNumber, URL: r.HTMLURL, Status: r.Status, Conclusion: r.Conclusion, CreatedAt: r.CreatedAt.UTC()}
}
