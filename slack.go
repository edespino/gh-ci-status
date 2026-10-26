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

// Slack takes at most 50 blocks a message, 3000 characters a section and
// 150 a header.
const (
	slackMaxBlocks  = 50
	slackMaxSection = 3000
	slackMaxHeader  = 150
	slackMoreRoom   = 40 // kept free in a section for "and N more"
)

type slackText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type slackBlock struct {
	Type     string      `json:"type"`
	Text     *slackText  `json:"text,omitempty"`
	Elements []slackText `json:"elements,omitempty"`
}

var slackEscape = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// The marks in front of each workflow, coloured like the status page's stripes.
const (
	markFailing  = "🔴"
	markPassing  = "🟢"
	markProgress = "🟡" // running or waiting
	markOther    = "⚪"
)

// SlackMessage is the Slack message (an incoming webhook's JSON) for rep: the
// summary as a heading, the repositories, then one block per group (failing,
// unreadable, other, passing, disabled) with a line per workflow, and when it
// was made, in loc. Slack shows the app's name, so the message does not
// repeat it. Links go to GitHub only.
func SlackMessage(rep Report, loc *time.Location) ([]byte, error) {
	multi := len(rep.Repos) > 1
	link := func(url string, n int) string { return fmt.Sprintf("<%s|#%d>", url, n) }
	name := func(r ReportRow) string {
		s := "<" + r.HistoryURL + "|" + slackEscape.Replace(r.Name) + ">"
		if multi {
			s += " (" + slackEscape.Replace(r.Repo) + ")"
		}
		return s
	}
	inProgress := func(r ReportRow) string {
		if r.InProgress == nil {
			return ""
		}
		return progress(r.InProgress.Status) + " " + link(r.InProgress.URL, r.InProgress.Number)
	}
	on := func(r ReportRow) string { return " on " + slackEscape.Replace(r.Branch) }

	var failing, unreadable, other, passing, disabled []string
	for _, r := range rep.Rows {
		switch r.State {
		case Failing:
			s := markFailing + " *" + name(r) + "*" + on(r) + " · failing for " + r.Age
			if r.Latest != nil {
				s += " · " + link(r.Latest.URL, r.Latest.Number)
			}
			var more []string
			if r.FailedStep != nil {
				more = append(more, "failed at <"+r.FailedStep.URL+"|"+slackEscape.Replace(r.FailedStep.Label)+">")
			}
			switch {
			case r.LastGreen != nil:
				more = append(more, fmt.Sprintf("%s since the last passing run %s (%s)", plural(r.FailedRuns, "failed run"),
					link(r.LastGreen.URL, r.LastGreen.Number), r.LastGreen.CreatedAt.In(loc).Format("Jan 2")))
			case r.NeverGreen:
				more = append(more, "never passed"+on(r)+" · "+plural(r.FailedRuns, "failed run"))
			default:
				more = append(more, "no passing run in the last "+plural(r.FailedRuns, slackEscape.Replace(r.Branch)+" run"))
			}
			if s := inProgress(r); s != "" {
				more = append(more, s)
			}
			failing = append(failing, s+"\n"+strings.Join(more, " · "))
		case Error:
			unreadable = append(unreadable, markOther+" "+name(r)+on(r)+": "+slackEscape.Replace(cut(r.Error, 300)))
		case Running:
			other = append(other, markProgress+" "+name(r)+on(r)+": "+inProgress(r))
		case Other:
			s := markOther + " " + name(r) + on(r) + ": " + slackEscape.Replace(r.Conclusion)
			if r.Latest != nil {
				s += " " + link(r.Latest.URL, r.Latest.Number)
			}
			if p := inProgress(r); p != "" {
				s += " · " + markProgress + " " + p
			}
			other = append(other, s)
		case None:
			other = append(other, markOther+" "+name(r)+on(r)+": no runs yet")
		case Passing:
			s := markPassing + " " + name(r)
			if p := inProgress(r); p != "" {
				s += " · " + markProgress + " " + p
			}
			passing = append(passing, s)
		case Disabled:
			disabled = append(disabled, markOther+" "+name(r))
		}
	}

	blocks := []slackBlock{
		{Type: "header", Text: &slackText{Type: "plain_text", Text: cut(rep.Summary, slackMaxHeader)}},
		{Type: "context", Elements: []slackText{{Type: "mrkdwn", Text: slackEscape.Replace(strings.Join(rep.Repos, ", "))}}},
	}
	groups := []struct {
		heading string
		lines   []string
	}{{"Failing", failing}, {"Unreadable", unreadable}, {"Other", other}, {"Passing", passing}, {"Disabled", disabled}}
	room := slackMaxBlocks - len(blocks) - 1 // the last block is the footer
	for i, g := range groups {
		if len(g.lines) == 0 {
			continue
		}
		later := 0 // keep a block for each group still to come
		for _, h := range groups[i+1:] {
			if len(h.lines) > 0 {
				later++
			}
		}
		for _, s := range pack(fmt.Sprintf("*%s (%d)*", g.heading, len(g.lines)), g.lines, room-later) {
			blocks = append(blocks, slackBlock{Type: "section", Text: &slackText{Type: "mrkdwn", Text: s}})
			room--
		}
	}

	footer := rep.Generated.In(loc).Format("Jan 2, 3:04 PM MST") + " · gh-ci-status"
	if rep.Version != "" {
		footer += " " + rep.Version
	}
	blocks = append(blocks, slackBlock{Type: "context", Elements: []slackText{{Type: "mrkdwn", Text: slackEscape.Replace(footer)}}})
	return json.Marshal(struct {
		Text   string       `json:"text"`
		Blocks []slackBlock `json:"blocks"`
	}{slackEscape.Replace(rep.Summary), blocks})
}

// pack puts heading and lines, one per line, into at most max sections of
// Slack's size; when they do not all fit, the last section ends in
// "and N more".
func pack(heading string, lines []string, max int) []string {
	var secs []string
	cur := heading
	for i, l := range lines {
		l = cut(l, slackMaxSection-len(heading)-slackMoreRoom)
		if len(cur)+1+len(l) > slackMaxSection-slackMoreRoom {
			if len(secs)+1 >= max {
				return append(secs, cur+fmt.Sprintf("\nand %d more", len(lines)-i))
			}
			secs = append(secs, cur)
			cur = l
			continue
		}
		cur += "\n" + l
	}
	return append(secs, cur)
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
