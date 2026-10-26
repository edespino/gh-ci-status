// SPDX-License-Identifier: Apache-2.0

package cistatus

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// SlackSettings is where "Send to Slack" posts: a Slack incoming webhook,
// which is a secret, and the name pages show for it (for example the
// channel, "#ci-status").
type SlackSettings struct {
	Webhook string `json:"webhook"`
	Label   string `json:"label"`
}

const slackFile = "slack.json"

var slackWebhook = regexp.MustCompile(`^https://hooks\.slack(-gov)?\.com/services/[A-Za-z0-9]+/[A-Za-z0-9]+/[A-Za-z0-9]+$`)

// CheckSlackWebhook accepts only a Slack incoming-webhook address
// (https://hooks.slack.com/services/...), so nothing else is ever posted to.
// Its error never repeats the address, which is a secret.
func CheckSlackWebhook(u string) error {
	if !slackWebhook.MatchString(u) {
		return errors.New("that is not a Slack incoming webhook address (https://hooks.slack.com/services/...)")
	}
	return nil
}

// LoadSlack reads the Slack settings saved in dir; ok is false when there
// are none. Like the page key, the file must be readable only by the user.
func LoadSlack(dir string) (s SlackSettings, ok bool, err error) {
	path := filepath.Join(dir, slackFile)
	st, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return SlackSettings{}, false, nil
	}
	if err != nil {
		return SlackSettings{}, false, err
	}
	if st.Mode().Perm()&0o077 != 0 {
		return SlackSettings{}, false, fmt.Errorf("%s can be read by other users; run: chmod 600 %s", path, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return SlackSettings{}, false, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return SlackSettings{}, false, fmt.Errorf("%s is not valid: %v", path, err)
	}
	if err := CheckSlackWebhook(s.Webhook); err != nil {
		return SlackSettings{}, false, fmt.Errorf("%s: %v", path, err)
	}
	return s, true, nil
}

// SaveSlack checks s and saves it in dir, readable only by the user. An
// empty label becomes "Slack".
func SaveSlack(dir string, s SlackSettings) error {
	if err := CheckSlackWebhook(s.Webhook); err != nil {
		return err
	}
	s.Label = strings.TrimSpace(s.Label)
	if s.Label == "" {
		s.Label = "Slack"
	}
	if utf8.RuneCountInString(s.Label) > 80 || strings.ContainsFunc(s.Label, unicode.IsControl) {
		return errors.New("the name is at most 80 characters, on one line")
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return WritePrivate(dir, slackFile, append(data, '\n'))
}

// RemoveSlack forgets the Slack settings in dir.
func RemoveSlack(dir string) error {
	if err := os.Remove(filepath.Join(dir, slackFile)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Slack's limit for a header's text; slackMaxItems keeps each group's list
// short (the rest is counted).
const (
	slackMaxHeader = 150
	slackMaxItems  = 40
)

// slackBlock is a Block Kit block: a header with text, or a context line or
// rich text with elements.
type slackBlock struct {
	Type     string      `json:"type"`
	Text     *slackText  `json:"text,omitempty"`
	Elements []slackElem `json:"elements,omitempty"`
}

type slackText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// slackElem is an element of rich text (a section, a list, a piece of text
// or a link) or a context line's text.
type slackElem struct {
	Type     string      `json:"type"`
	Text     string      `json:"text,omitempty"`
	URL      string      `json:"url,omitempty"`
	Style    any         `json:"style,omitempty"` // a list's "bullet", or {"bold": true}
	Indent   int         `json:"indent,omitempty"`
	Elements []slackElem `json:"elements,omitempty"`
}

// slackItem is a workflow's line and, for a failing one, its details, shown
// indented under it.
type slackItem struct{ line, details []slackElem }

var slackEscape = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// The marks on group headings, coloured like the status page's stripes, and
// for a run in progress: an hourglass while it runs, a clock while it waits.
const (
	markFailing = "🔴"
	markPassing = "🟢"
	markRunning = "⏳"
	markWaiting = "🕒"
	markOther   = "⚪"
)

// SlackMessage is the Slack message (an incoming webhook's JSON) for rep: the
// summary as a heading, the repositories (and the branch, when every row is
// on the same one), then for each group (failing, unreadable, other,
// passing, disabled) one rich text block with its marked heading and a list,
// one item per workflow, a failing one's details indented under it; last,
// when it was made, in loc. Slack shows the app's name, so the message does
// not repeat it. Links go to GitHub only.
func SlackMessage(rep Report, loc *time.Location) ([]byte, error) {
	multi := len(rep.Repos) > 1
	branch := "" // the branch every row is on, when they share one
	for i, r := range rep.Rows {
		if i == 0 {
			branch = r.Branch
		} else if r.Branch != branch {
			branch = ""
			break
		}
	}
	on := func(r ReportRow) string {
		if branch != "" {
			return ""
		}
		return " on " + r.Branch
	}
	text := func(s string) slackElem { return slackElem{Type: "text", Text: s} }
	link := func(url, s string) slackElem { return slackElem{Type: "link", URL: url, Text: s} }
	run := func(r *RunRef) slackElem { return link(r.URL, fmt.Sprintf("#%d", r.Number)) }
	name := func(r ReportRow, bold bool) []slackElem {
		e := link(r.HistoryURL, r.Name)
		if bold {
			e.Style = map[string]bool{"bold": true}
		}
		if multi {
			return []slackElem{e, text(" (" + r.Repo + ")")}
		}
		return []slackElem{e}
	}
	// inProgress is the run in progress, if any: its mark (⏳ running, 🕒
	// waiting) and "running #N" or "waiting #N".
	inProgress := func(r ReportRow) (mark string, els []slackElem) {
		if r.InProgress == nil {
			return "", nil
		}
		mark = markRunning
		if waiting(r.InProgress.Status) {
			mark = markWaiting
		}
		return mark, []slackElem{text(progress(r.InProgress.Status) + " "), run(r.InProgress)}
	}
	line := func(parts ...any) []slackElem {
		var out []slackElem
		for _, p := range parts {
			switch v := p.(type) {
			case string:
				out = append(out, text(v))
			case slackElem:
				out = append(out, v)
			case []slackElem:
				out = append(out, v...)
			}
		}
		return out
	}

	var failing, unreadable, other, passing, disabled []slackItem
	for _, r := range rep.Rows {
		switch r.State {
		case Failing:
			it := slackItem{line: line(name(r, true), on(r)+" · failing for "+r.Age)}
			if r.Latest != nil {
				it.line = line(it.line, " · ", run(r.Latest))
			}
			var details [][]slackElem
			if r.FailedStep != nil {
				details = append(details, line("failed at ", link(r.FailedStep.URL, r.FailedStep.Label)))
			}
			switch {
			case r.LastGreen != nil:
				details = append(details, line(plural(r.FailedRuns, "failed run")+" since the last passing run ", run(r.LastGreen),
					" ("+r.LastGreen.CreatedAt.In(loc).Format("Jan 2")+")"))
			case r.NeverGreen:
				details = append(details, line("never passed"+on(r)+" · "+plural(r.FailedRuns, "failed run")))
			default:
				runs := r.Branch + " run"
				if branch != "" {
					runs = "run"
				}
				details = append(details, line("no passing run in the last "+plural(r.FailedRuns, runs)))
			}
			if mark, els := inProgress(r); els != nil {
				details = append(details, line(mark+" ", els))
			}
			for i, d := range details {
				if i > 0 {
					it.details = append(it.details, text(" · "))
				}
				it.details = append(it.details, d...)
			}
			failing = append(failing, it)
		case Error:
			unreadable = append(unreadable, slackItem{line: line(name(r, false), on(r)+": "+cut(r.Error, 300))})
		case Running:
			mark, els := inProgress(r)
			other = append(other, slackItem{line: line(mark+" ", name(r, false), on(r)+": ", els)})
		case Other:
			it := slackItem{line: line(markOther+" ", name(r, false), on(r)+": "+r.Conclusion)}
			if r.Latest != nil {
				it.line = line(it.line, " ", run(r.Latest))
			}
			if mark, els := inProgress(r); els != nil {
				it.line = line(it.line, " · "+mark+" ", els)
			}
			other = append(other, it)
		case None:
			other = append(other, slackItem{line: line(markOther+" ", name(r, false), on(r)+": no runs yet")})
		case Passing:
			it := slackItem{line: line(name(r, false))}
			if mark, els := inProgress(r); els != nil {
				it.line = line(it.line, " · "+mark+" ", els)
			}
			passing = append(passing, it)
		case Disabled:
			disabled = append(disabled, slackItem{line: line(name(r, false))})
		}
	}

	blocks := []slackBlock{
		{Type: "header", Text: &slackText{Type: "plain_text", Text: cut(rep.Summary, slackMaxHeader)}},
	}
	where := strings.Join(rep.Repos, ", ")
	if branch != "" {
		where += " · " + branch
	}
	blocks = append(blocks, slackBlock{Type: "context", Elements: []slackElem{{Type: "mrkdwn", Text: slackEscape.Replace(where)}}})
	// A group's mark is on its heading; Other mixes states, so its lines
	// carry their own.
	for _, g := range []struct {
		heading string
		items   []slackItem
	}{{markFailing + " Failing", failing}, {markOther + " Unreadable", unreadable}, {"Other", other}, {markPassing + " Passing", passing}, {markOther + " Disabled", disabled}} {
		if len(g.items) > 0 {
			blocks = append(blocks, slackGroup(fmt.Sprintf("%s (%d)", g.heading, len(g.items)), g.items))
		}
	}
	footer := rep.Generated.In(loc).Format("Jan 2, 3:04 PM MST") + " · gh-ci-status"
	if rep.Version != "" {
		footer += " " + rep.Version
	}
	blocks = append(blocks, slackBlock{Type: "context", Elements: []slackElem{{Type: "mrkdwn", Text: slackEscape.Replace(footer)}}})
	return json.Marshal(struct {
		Text   string       `json:"text"`
		Blocks []slackBlock `json:"blocks"`
	}{slackEscape.Replace(rep.Summary), blocks})
}

// slackGroup is one group as rich text: the heading in bold, then a bulleted
// list with an item per workflow and its details as an indented item, at
// most slackMaxItems workflows and "and N more".
func slackGroup(heading string, items []slackItem) slackBlock {
	els := []slackElem{{Type: "rich_text_section", Elements: []slackElem{{Type: "text", Text: heading, Style: map[string]bool{"bold": true}}}}}
	add := func(indent int, line []slackElem) {
		item := slackElem{Type: "rich_text_section", Elements: line}
		if last := &els[len(els)-1]; last.Type == "rich_text_list" && last.Indent == indent {
			last.Elements = append(last.Elements, item)
			return
		}
		els = append(els, slackElem{Type: "rich_text_list", Style: "bullet", Indent: indent, Elements: []slackElem{item}})
	}
	for i, it := range items {
		if i == slackMaxItems {
			add(0, []slackElem{{Type: "text", Text: fmt.Sprintf("and %d more", len(items)-i)}})
			break
		}
		add(0, it.line)
		if it.details != nil {
			add(1, it.details)
		}
	}
	return slackBlock{Type: "rich_text", Elements: els}
}

// cut shortens s to at most n bytes, on a character boundary.
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// PostSlack posts body (a message from SlackMessage) to the webhook. Slack
// answers "ok"; any other answer is returned as the error. Errors never
// contain the address, and redirects are not followed.
func PostSlack(ctx context.Context, hc *http.Client, webhook string, body []byte) error {
	if err := CheckSlackWebhook(webhook); err != nil {
		return err
	}
	c := *hc
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook, bytes.NewReader(body))
	if err != nil {
		return errors.New("cannot make the request to Slack")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err // url.Error's text includes the address
		}
		return fmt.Errorf("could not reach Slack: %v", err)
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	return fmt.Errorf("Slack answered %d: %s", resp.StatusCode, strings.TrimSpace(string(answer)))
}
