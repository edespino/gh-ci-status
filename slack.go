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

// Slack takes at most 50 blocks a message and 3000 characters a section.
const (
	slackMaxReds    = 40 // red rows with a block each; the rest share one
	slackMaxSection = 3000
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

// SlackMessage is the Slack message (an incoming webhook's JSON) for rep:
// the summary, each red row with its links, the other rows in short lists,
// and when it was made, in loc. Links go to GitHub only.
func SlackMessage(rep Report, loc *time.Location) ([]byte, error) {
	multi := len(rep.Repos) > 1
	name := func(r ReportRow) string {
		s := "<" + r.HistoryURL + "|" + slackEscape.Replace(r.Name) + ">"
		if multi {
			s += " (" + slackEscape.Replace(r.Repo) + ")"
		}
		return s
	}
	running := func(r ReportRow) string {
		if r.InProgress == nil {
			return ""
		}
		return fmt.Sprintf("%s <%s|#%d>", progress(r.InProgress.Status), r.InProgress.URL, r.InProgress.Number)
	}
	section := func(s string) slackBlock {
		return slackBlock{Type: "section", Text: &slackText{Type: "mrkdwn", Text: s}}
	}
	groups := map[State][]ReportRow{}
	for _, r := range rep.Rows {
		groups[r.State] = append(groups[r.State], r)
	}

	blocks := []slackBlock{section("*gh-ci-status* · " + slackEscape.Replace(strings.Join(rep.Repos, ", ")) + "\n" + slackEscape.Replace(rep.Summary))}
	if reds := groups[Failing]; len(reds) > 0 {
		blocks = append(blocks, section(fmt.Sprintf("*Failing (%d)*", len(reds))))
		for i, r := range reds {
			if i == slackMaxReds {
				var names []string
				for _, r := range reds[i:] {
					names = append(names, name(r))
				}
				blocks = append(blocks, section(joinFit(fmt.Sprintf("and %d more failing: ", len(reds)-i), names, ", ")))
				break
			}
			line := "*" + name(r) + "* on " + slackEscape.Replace(r.Branch) + " · failing for " + r.Age
			if r.Latest != nil {
				line += fmt.Sprintf(" · <%s|#%d>", r.Latest.URL, r.Latest.Number)
			}
			var more []string
			if r.FailedStep != nil {
				more = append(more, "failed at <"+r.FailedStep.URL+"|"+slackEscape.Replace(r.FailedStep.Label)+">")
			}
			switch {
			case r.LastGreen != nil:
				more = append(more, fmt.Sprintf("%s since the last passing run <%s|#%d> (%s)", plural(r.FailedRuns, "failed run"),
					r.LastGreen.URL, r.LastGreen.Number, r.LastGreen.CreatedAt.In(loc).Format("Jan 2")))
			case r.NeverGreen:
				more = append(more, "never passed on "+slackEscape.Replace(r.Branch)+" · "+plural(r.FailedRuns, "failed run"))
			default:
				more = append(more, "no passing run in the last "+plural(r.FailedRuns, slackEscape.Replace(r.Branch)+" run"))
			}
			if s := running(r); s != "" {
				more = append(more, s)
			}
			blocks = append(blocks, section(cut(line+"\n"+strings.Join(more, " · "), slackMaxSection)))
		}
	}

	var errs, others, passing, disabled []string
	for _, r := range groups[Error] {
		errs = append(errs, name(r)+" on "+slackEscape.Replace(r.Branch)+": "+slackEscape.Replace(cut(r.Error, 300)))
	}
	for _, r := range rep.Rows {
		var s string
		switch r.State {
		case Running:
			s = name(r) + " on " + slackEscape.Replace(r.Branch) + ": " + running(r)
		case Other:
			s = name(r) + " on " + slackEscape.Replace(r.Branch) + ": " + slackEscape.Replace(r.Conclusion)
			if r.Latest != nil {
				s += fmt.Sprintf(" <%s|#%d>", r.Latest.URL, r.Latest.Number)
			}
			if rs := running(r); rs != "" {
				s += " · " + rs
			}
		case None:
			s = name(r) + " on " + slackEscape.Replace(r.Branch) + ": no runs yet"
		default:
			continue
		}
		others = append(others, s)
	}
	for _, r := range groups[Passing] {
		s := name(r)
		if rs := running(r); rs != "" {
			s += " (" + rs + ")"
		}
		passing = append(passing, s)
	}
	for _, r := range groups[Disabled] {
		disabled = append(disabled, name(r))
	}
	for _, g := range []struct {
		heading   string
		items     []string
		sep, join string
	}{
		{"Unreadable", errs, "\n", "\n"},
		{"Other", others, "\n", "\n"},
		{"Passing", passing, ": ", ", "},
		{"Disabled", disabled, ": ", ", "},
	} {
		if len(g.items) > 0 {
			blocks = append(blocks, section(joinFit(fmt.Sprintf("*%s (%d)*%s", g.heading, len(g.items), g.sep), g.items, g.join)))
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
	}{"gh-ci-status: " + slackEscape.Replace(rep.Summary), blocks})
}

// joinFit is head and the items joined by sep, within Slack's section limit;
// the items that do not fit are counted: "and 12 more".
func joinFit(head string, items []string, sep string) string {
	s := head
	for i, it := range items {
		next := it
		if i > 0 {
			next = sep + it
		}
		reserve := 0
		if rest := len(items) - i - 1; rest > 0 {
			reserve = len(fmt.Sprintf("%sand %d more", sep, rest))
		}
		if len(s)+len(next)+reserve > slackMaxSection {
			if i > 0 {
				s += sep
			}
			return s + fmt.Sprintf("and %d more", len(items)-i)
		}
		s += next
	}
	return s
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
