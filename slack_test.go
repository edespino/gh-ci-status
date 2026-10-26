// SPDX-License-Identifier: Apache-2.0

package cistatus

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

const testWebhook = "https://hooks.slack.com/services/T0001/B0002/abcDEF123secret"

func TestCheckSlackWebhook(t *testing.T) {
	for _, ok := range []string{testWebhook, "https://hooks.slack-gov.com/services/T1/B2/c3"} {
		if err := CheckSlackWebhook(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{
		"", "http://hooks.slack.com/services/T1/B2/c3", "https://example.com/services/T1/B2/c3",
		"https://hooks.slack.com.example.com/services/T1/B2/c3", "https://hooks.slack.com:8443/services/T1/B2/c3",
		"https://me@hooks.slack.com/services/T1/B2/c3", "https://hooks.slack.com/services/T1/B2/c3?x=1",
		"https://hooks.slack.com/services/T1/B2", "https://hooks.slack.com/workflows/T1/A2/1/c3",
		"https://hooks.slack.com/services/T1/B2/c3#x", " " + testWebhook,
	} {
		err := CheckSlackWebhook(bad)
		if err == nil {
			t.Errorf("%q accepted", bad)
			continue
		}
		if bad != "" && strings.Contains(err.Error(), bad) {
			t.Errorf("error repeats the address: %v", err)
		}
	}
}

// The webhook is kept in slack.json, readable only by the user; a missing
// file means Slack is not set up.
func TestSaveLoadRemoveSlack(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "gh-ci-status")
	if _, ok, err := LoadSlack(dir); ok || err != nil {
		t.Fatalf("nothing saved: ok %v err %v", ok, err)
	}
	if err := SaveSlack(dir, SlackSettings{Webhook: "https://example.com/x", Label: "#ci"}); err == nil {
		t.Fatal("saved a non-Slack address")
	}
	if _, err := os.Stat(filepath.Join(dir, "slack.json")); err == nil {
		t.Fatal("a refused address was written")
	}
	if err := SaveSlack(dir, SlackSettings{Webhook: testWebhook, Label: strings.Repeat("x", 81)}); err == nil {
		t.Fatal("saved an 81-character name")
	}
	if err := SaveSlack(dir, SlackSettings{Webhook: testWebhook, Label: "  #ci-status "}); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(dir, "slack.json"))
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("slack.json: %v %v", st, err)
	}
	s, ok, err := LoadSlack(dir)
	if !ok || err != nil || s.Webhook != testWebhook || s.Label != "#ci-status" {
		t.Fatalf("loaded %+v ok %v err %v", s, ok, err)
	}
	if err := SaveSlack(dir, SlackSettings{Webhook: testWebhook}); err != nil {
		t.Fatal(err)
	}
	if s, _, _ := LoadSlack(dir); s.Label != "Slack" {
		t.Errorf("label without a name = %q, want Slack", s.Label)
	}
	if err := os.Chmod(filepath.Join(dir, "slack.json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadSlack(dir); err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Errorf("readable by others: %v", err)
	}
	if err := RemoveSlack(dir); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := LoadSlack(dir); ok || err != nil {
		t.Fatalf("after remove: ok %v err %v", ok, err)
	}
	if err := RemoveSlack(dir); err != nil {
		t.Errorf("removing twice: %v", err)
	}
}

type slackMsg struct {
	Text   string `json:"text"`
	Blocks []struct {
		Type string `json:"type"`
		Text *struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"text"`
		Elements []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"elements"`
	} `json:"blocks"`
}

func decodeSlack(t *testing.T, b []byte) (slackMsg, string) {
	t.Helper()
	var m slackMsg
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	var all []string
	for _, bl := range m.Blocks {
		if bl.Text != nil {
			if bl.Type != "section" || bl.Text.Type != "mrkdwn" {
				t.Errorf("block %s with %s text", bl.Type, bl.Text.Type)
			}
			if len(bl.Text.Text) > 3000 {
				t.Errorf("section text of %d characters", len(bl.Text.Text))
			}
			all = append(all, bl.Text.Text)
		}
		for _, e := range bl.Elements {
			all = append(all, e.Text)
		}
	}
	if len(m.Blocks) > 50 {
		t.Errorf("%d blocks", len(m.Blocks))
	}
	return m, strings.Join(all, "\n")
}

var la = func() *time.Location {
	l, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		panic(err)
	}
	return l
}()

func run(id int64, number int, branch, status, conclusion, created string) *Run {
	return runPtr(mkRun(id, number, branch, status, conclusion, created, ""))
}

func hist(file, branch string) string { return HistoryURL(testRepo, file, branch) }

const runsURL = "https://github.com/" + testRepo + "/actions/runs/"

// The message: the summary, each red row with its links, the rest in short
// lists, and when and by what it was made. Text from GitHub is escaped.
func TestSlackMessage(t *testing.T) {
	first := run(1, 1, "main", "queued", "", "2026-10-24T11:50:00Z")
	rows := []Row{
		{Repo: testRepo, Watch: Watch{"b.yml", "main"}, Name: "Build", State: Failing,
			Latest: run(120, 20, "main", "completed", "failure", "2026-10-20T12:00:00Z"), LastGreen: run(118, 18, "main", "completed", "success", "2026-10-10T12:00:00Z"),
			InProgress: run(121, 21, "main", "in_progress", "", "2026-10-24T11:00:00Z"),
			RedSince:   ts("2026-10-14T12:00:00Z"), Age: "10 days", FailedRuns: 2, FailedStep: &StepRef{URL: "https://github.com/x/job/9", Label: "Compile (build)"}},
		{Repo: testRepo, Watch: Watch{"n.yml", "main"}, Name: "Never", State: Failing, AllHistory: true,
			Latest: run(5, 5, "main", "completed", "failure", "2026-10-20T12:00:00Z"), RedSince: ts("2026-10-01T12:00:00Z"), Age: "23 days", FailedRuns: 3},
		{Repo: testRepo, Watch: Watch{"o.yml", "main"}, Name: "A <b> & c", State: Failing,
			Latest: run(7, 7, "main", "completed", "failure", "2026-10-23T12:00:00Z"), RedSince: ts("2026-10-23T12:00:00Z"), Age: "1 day", FailedRuns: 1},
		{Repo: testRepo, Watch: Watch{"gone.yml", "dev"}, Name: "gone.yml", State: Error, Err: "GET /x: 404 Not Found"},
		{Repo: testRepo, Watch: Watch{"c.yml", "main"}, Name: "Cancelled", State: Other, Conclusion: "cancelled", Latest: run(50, 5, "main", "completed", "cancelled", "2026-10-22T12:00:00Z")},
		{Repo: testRepo, Watch: Watch{"e.yml", "dev"}, Name: "Never ran", State: None},
		{Repo: testRepo, Watch: Watch{"f.yml", "main"}, Name: "First", State: Running, Latest: first, InProgress: first},
		{Repo: testRepo, Watch: Watch{"l.yml", "main"}, Name: "Lint", State: Passing,
			Latest: run(60, 6, "main", "completed", "success", "2026-10-23T12:00:00Z"), InProgress: run(61, 7, "main", "queued", "", "2026-10-24T11:30:00Z")},
		{Repo: testRepo, Watch: Watch{"d.yml", "main"}, Name: "Docs", State: Passing, Latest: run(70, 2, "main", "completed", "success", "2026-10-23T12:00:00Z")},
		{Repo: testRepo, Watch: Watch{"old.yml", "main"}, Name: "Old", State: Disabled, WorkflowState: "disabled_manually"},
	}
	rep := NewReport([]string{testRepo}, SortRows(rows), testNow)
	rep.Version = "v0.4.0"
	b, err := SlackMessage(rep, la)
	if err != nil {
		t.Fatal(err)
	}
	m, all := decodeSlack(t, b)
	if m.Text != "gh-ci-status: "+rep.Summary {
		t.Errorf("text = %q", m.Text)
	}
	for _, want := range []string{
		"*gh-ci-status* · " + testRepo + "\n" + rep.Summary,
		"*Red (3)*",
		"*<" + hist("b.yml", "main") + "|Build>* on main · red 10 days · <" + runsURL + "120|#20>\n" +
			"failed at <https://github.com/x/job/9|Compile (build)> · 2 failed runs since last green <" + runsURL + "118|#18> (Oct 10) · running <" + runsURL + "121|#21>",
		"*<" + hist("n.yml", "main") + "|Never>* on main · red 23 days · <" + runsURL + "5|#5>\nnever green on main · 3 failed runs",
		"*<" + hist("o.yml", "main") + "|A &lt;b&gt; &amp; c>* on main · red 1 day · <" + runsURL + "7|#7>\nno green in the last 1 main run",
		"*Unreadable (1)*\n<" + hist("gone.yml", "dev") + "|gone.yml> on dev: GET /x: 404 Not Found",
		"*Other (3)*\n",
		"<" + hist("c.yml", "main") + "|Cancelled> on main: cancelled <" + runsURL + "50|#5>",
		"<" + hist("e.yml", "dev") + "|Never ran> on dev: no runs yet",
		"<" + hist("f.yml", "main") + "|First> on main: waiting <" + runsURL + "1|#1>",
		"*Passing (2)*: <" + hist("l.yml", "main") + "|Lint> (waiting <" + runsURL + "61|#7>), <" + hist("d.yml", "main") + "|Docs>",
		"*Disabled (1)*: <" + hist("old.yml", "main") + "|Old>",
		"Oct 24, 5:00 AM PDT · gh-ci-status v0.4.0",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("message lacks %q", want)
		}
	}
	if strings.Contains(all, "<b>") {
		t.Error("a name was not escaped")
	}
	if last := m.Blocks[len(m.Blocks)-1]; last.Type != "context" {
		t.Errorf("last block is %s, want context", last.Type)
	}
}

// With several repositories each row names its repository.
func TestSlackMessageSeveralRepos(t *testing.T) {
	rows := []Row{{Repo: "o/other", Watch: Watch{"b.yml", "main"}, Name: "Build", State: Passing, Latest: run(1, 1, "main", "completed", "success", "2026-10-23T12:00:00Z")}}
	b, err := SlackMessage(NewReport([]string{testRepo, "o/other"}, rows, testNow), la)
	if err != nil {
		t.Fatal(err)
	}
	if _, all := decodeSlack(t, b); !strings.Contains(all, "|Build> (o/other)") || !strings.Contains(all, testRepo+", o/other") {
		t.Errorf("message = %s", all)
	}
}

// Slack takes at most 50 blocks and 3000 characters a section: many reds end
// in "and N more", and a long passing list is cut with a count.
func TestSlackMessageLimits(t *testing.T) {
	var rows []Row
	for i := 0; i < 60; i++ {
		rows = append(rows, Row{Repo: testRepo, Watch: Watch{Workflow: "r" + strings.Repeat("x", i%7) + ".yml", Branch: "main"}, Name: "Red " + string(rune('A'+i%26)),
			State: Failing, Latest: run(int64(i), i, "main", "completed", "failure", "2026-10-20T12:00:00Z"), Age: "4 days", FailedRuns: 1})
	}
	for i := 0; i < 400; i++ {
		rows = append(rows, Row{Repo: testRepo, Watch: Watch{Workflow: "passing-workflow-number-" + string(rune('a'+i%26)) + ".yml", Branch: "main"}, Name: "Passing workflow with a long name",
			State: Passing, Latest: run(int64(1000+i), i, "main", "completed", "success", "2026-10-23T12:00:00Z")})
	}
	b, err := SlackMessage(NewReport([]string{testRepo}, rows, testNow), la)
	if err != nil {
		t.Fatal(err)
	}
	_, all := decodeSlack(t, b)
	if !strings.Contains(all, "*Red (60)*") || !strings.Contains(all, "and 20 more red") {
		t.Errorf("reds: %.400s", all)
	}
	if !regexp.MustCompile(`\*Passing \(400\)\*: .*, and \d+ more\n`).MatchString(all) {
		t.Errorf("passing list not cut with a count")
	}
}

// toFake sends every request to the fake Slack server, whatever its host.
type toFake struct{ target *url.URL }

func (f toFake) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.URL.Scheme, r.URL.Host = f.target.Scheme, f.target.Host
	return http.DefaultTransport.RoundTrip(r)
}

func fakeSlack(t *testing.T, h http.HandlerFunc) *http.Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	return &http.Client{Timeout: 5 * time.Second, Transport: toFake{u}}
}

func TestPostSlack(t *testing.T) {
	var got struct {
		path, ctype, body string
	}
	hc := fakeSlack(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got.path, got.ctype, got.body = r.URL.Path, r.Header.Get("Content-Type"), string(b)
		_, _ = io.WriteString(w, "ok")
	})
	if err := PostSlack(context.Background(), hc, testWebhook, []byte(`{"text":"hi"}`)); err != nil {
		t.Fatal(err)
	}
	if got.path != "/services/T0001/B0002/abcDEF123secret" || got.ctype != "application/json" || got.body != `{"text":"hi"}` {
		t.Errorf("Slack got %+v", got)
	}
	if err := PostSlack(context.Background(), hc, "https://example.com/services/T1/B2/c3", nil); err == nil {
		t.Error("posted to a non-Slack address")
	}
}

// Slack's answer is reported; the address never is.
func TestPostSlackErrors(t *testing.T) {
	notFound := fakeSlack(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		_, _ = io.WriteString(w, "no_service")
	})
	err := PostSlack(context.Background(), notFound, testWebhook, []byte(`{}`))
	if err == nil || err.Error() != "Slack answered 404: no_service" {
		t.Errorf("err = %v", err)
	}
	moved := fakeSlack(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.com/elsewhere", http.StatusFound)
	})
	if err := PostSlack(context.Background(), moved, testWebhook, []byte(`{}`)); err == nil || !strings.Contains(err.Error(), "302") {
		t.Errorf("redirect: %v", err)
	}
	gone := httptest.NewServer(http.NotFoundHandler())
	u, _ := url.Parse(gone.URL)
	gone.Close()
	err = PostSlack(context.Background(), &http.Client{Timeout: 2 * time.Second, Transport: toFake{u}}, testWebhook, []byte(`{}`))
	if err == nil || strings.Contains(err.Error(), "abcDEF123secret") || strings.Contains(err.Error(), "/services/") {
		t.Errorf("unreachable: %v", err)
	}
}
