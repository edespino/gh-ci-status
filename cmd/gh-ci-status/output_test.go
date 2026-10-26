// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	cistatus "github.com/edespino/gh-ci-status"
	"github.com/edespino/gh-ci-status/internal/banner"
	"github.com/edespino/gh-ci-status/internal/fakegithub"
)

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestServeBanner(t *testing.T) {
	plain := banner.Terminal(false)
	for _, c := range []struct {
		name     string
		terminal bool
		noColor  string
		banner   bool
		color    bool
	}{
		{"terminal", true, "", true, true},
		{"terminal with NO_COLOR", true, "1", true, false},
		{"not a terminal", false, "", false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newCLI(t, fakegithub.New(t))
			e.d.isTerminal = func() bool { return c.terminal }
			e.d.getenv = func(k string) string {
				if k == "NO_COLOR" {
					return c.noColor
				}
				return ""
			}
			_, stop := startServe(t, e, "--no-open", "--port", "0")
			stop()
			out := e.out.String()
			if got := strings.Contains(ansi.ReplaceAllString(out, ""), plain); got != c.banner {
				t.Errorf("banner shown = %v, want %v:\n%s", got, c.banner, out)
			}
			if got := strings.Contains(out, "\x1b["); got != c.color {
				t.Errorf("colour = %v, want %v", got, c.color)
			}
			if c.banner && !strings.HasPrefix(ansi.ReplaceAllString(out, ""), plain) {
				t.Error("banner is not the first thing printed")
			}
		})
	}
}

// The plain status output stays clean for scripts and AI, even on a terminal.
func TestTextViewHasNoBanner(t *testing.T) {
	f := fakegithub.New(t)
	serveRepo(f, "b.yml")
	e := newCLI(t, f)
	e.d.isTerminal = func() bool { return true }
	watchIn(t, e, w("b.yml", "main"))
	run(context.Background(), nil, e.d)
	if out := e.out.String(); strings.Contains(out, banner.Tagline) || strings.Contains(out, "\x1b[") {
		t.Fatalf("text view printed the banner: %q", out)
	}
}

// run1 is a completed run of testRepo.
func run1(id int64, number int, branch, conclusion, created string) *cistatus.Run {
	r := cistatus.Run{ID: id, RunNumber: number, HeadBranch: branch, Status: "completed", Conclusion: conclusion,
		HTMLURL: "https://github.com/" + testRepo + "/actions/runs/" + strconv.FormatInt(id, 10)}
	r.CreatedAt = mustTime(created)
	r.RunStartedAt = r.CreatedAt
	return &r
}

func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestWriteText(t *testing.T) {
	red := cistatus.Row{Repo: testRepo, Watch: w("b.yml", "release/x"), Name: "Build B", State: cistatus.Failing,
		Latest: run1(120, 20, "release/x", "failure", "2026-10-20T12:00:00Z"), Age: "10 days", RedSince: mustTime("2026-10-14T12:00:00Z")}
	green := cistatus.Row{Repo: testRepo, Watch: w("g.yml", "main"), Name: "Green", State: cistatus.Passing,
		Latest: run1(50, 5, "main", "success", "2026-10-23T12:00:00Z")}
	rows := []cistatus.Row{red, green}
	var b bytes.Buffer
	writeText(&b, rows, false)
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	if len(lines) != 3 || lines[0] != cistatus.Summary(rows) {
		t.Fatalf("text = %q", b.String())
	}
	for i, want := range [][]string{
		{"failing", "Build B", "release/x", "red 10 days", "/actions/runs/120"},
		{"passing", "Green", "main", "Oct 23, 5:00 AM PDT", "/actions/runs/50"},
	} {
		for _, w := range want {
			if !strings.Contains(lines[i+1], w) {
				t.Errorf("line %d %q lacks %q", i+1, lines[i+1], w)
			}
		}
	}
	b.Reset()
	rerun := red
	rerun.InProgress = &cistatus.Run{ID: 121, RunNumber: 21, Status: "in_progress", RunStartedAt: mustTime("2026-10-24T11:00:00Z")}
	first := &cistatus.Run{ID: 9, RunNumber: 1, Status: "queued", RunStartedAt: mustTime("2026-10-24T11:20:00Z"), HTMLURL: "https://github.com/" + testRepo + "/actions/runs/9"}
	never := cistatus.Row{Repo: testRepo, Watch: w("new.yml", "main"), Name: "New", State: cistatus.Running, Latest: first, InProgress: first}
	pending := green
	pending.InProgress = &cistatus.Run{ID: 51, RunNumber: 6, Status: "pending", RunStartedAt: mustTime("2026-10-24T11:10:00Z")}
	writeText(&b, []cistatus.Row{rerun, never, pending}, false)
	if line := strings.Split(b.String(), "\n")[3]; !strings.Contains(line, "Oct 23, 5:00 AM PDT · waiting #6 since Oct 24, 4:10 AM PDT") {
		t.Errorf("passing line with a waiting run = %q", line)
	}
	if line := strings.Split(b.String(), "\n")[1]; !strings.HasPrefix(line, "failing   Build B on release/x  red 10 days · running #21 since Oct 24, 4:00 AM PDT  https://") {
		t.Errorf("red line with a run in progress = %q", line)
	}
	if line := strings.Split(b.String(), "\n")[2]; line != "running   New on main  waiting since Oct 24, 4:20 AM PDT  https://github.com/"+testRepo+"/actions/runs/9" {
		t.Errorf("running line = %q", line)
	}
	b.Reset()
	off := cistatus.Row{Repo: testRepo, Watch: w("old.yml", "main"), Name: "Old", State: cistatus.Disabled, WorkflowState: "disabled_manually",
		Latest: run1(150, 9, "main", "failure", "2026-08-01T12:00:00Z")}
	writeText(&b, []cistatus.Row{off}, false)
	if line := strings.Split(b.String(), "\n")[1]; !strings.HasPrefix(line, "disabled  Old on main  turned off on GitHub (disabled_manually)") {
		t.Errorf("disabled line = %q", line)
	}
	b.Reset()
	writeText(&b, rows, true)
	if !strings.Contains(b.String(), "Build B ("+testRepo+") on release/x") {
		t.Errorf("with several repositories, lines name theirs: %q", b.String())
	}
}
