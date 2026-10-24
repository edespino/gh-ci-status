// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"io"
	"time"

	cistatus "github.com/OWNER/gh-ci-status"
)

// writeText prints the summary, then one line per row; with several
// repositories each line names its repository.
func writeText(w io.Writer, rows []cistatus.Row, multiRepo bool) {
	fmt.Fprintln(w, cistatus.Summary(rows))
	for _, r := range rows {
		state, detail, link := string(r.State), "", cistatus.HistoryURL(r.Repo, r.Watch.Workflow, r.Watch.Branch)
		if r.Latest != nil {
			link = r.Latest.HTMLURL
			detail = localTime(started(r.Latest))
		}
		switch r.State {
		case cistatus.Failing:
			detail = "red " + r.Age
		case cistatus.Running:
			detail = "running since " + detail
		case cistatus.Other:
			state = r.Conclusion
		case cistatus.None:
			detail = "no run on " + r.Watch.Branch + " yet"
		case cistatus.Error:
			detail = r.Err
		}
		name := r.Name
		if multiRepo {
			name += " (" + r.Repo + ")"
		}
		fmt.Fprintf(w, "%-9s %s on %s  %s  %s\n", state, name, r.Watch.Branch, detail, link)
	}
}

// localTime: "Oct 23, 5:00 AM PDT" in this machine's time zone.
func localTime(t time.Time) string {
	return t.In(time.Local).Format("Jan 2, 3:04 PM MST")
}

func started(r *cistatus.Run) time.Time {
	if r.RunStartedAt.IsZero() {
		return r.CreatedAt
	}
	return r.RunStartedAt
}
