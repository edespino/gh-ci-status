// SPDX-License-Identifier: Apache-2.0

package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	cistatus "github.com/edespino/gh-ci-status"
)

const (
	testWebhook = "https://hooks.slack.com/services/T0001/B0002/abcDEF123secret"
	webhookPart = "abcDEF123secret"
)

// toFake sends every request to the fake Slack server, whatever its host.
type toFake struct{ target *url.URL }

func (f toFake) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.URL.Scheme, r.URL.Host = f.target.Scheme, f.target.Host
	return http.DefaultTransport.RoundTrip(r)
}

// slackInbox is what the fake Slack received.
type slackInbox struct {
	mu     sync.Mutex
	bodies []string
}

func (in *slackInbox) got() []string {
	in.mu.Lock()
	defer in.mu.Unlock()
	return append([]string(nil), in.bodies...)
}

// withSlack points s's Slack posts at a fake Slack that answers code and
// answer.
func withSlack(t *testing.T, s *Server, code int, answer string) *slackInbox {
	t.Helper()
	in := &slackInbox{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		in.mu.Lock()
		in.bodies = append(in.bodies, string(b))
		in.mu.Unlock()
		w.WriteHeader(code)
		_, _ = io.WriteString(w, answer)
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	s.slackHTTP = &http.Client{Timeout: 5 * time.Second, Transport: toFake{u}}
	return in
}

func saveSlack(t *testing.T, s *Server) {
	t.Helper()
	if err := cistatus.SaveSlack(s.dir, cistatus.SlackSettings{Webhook: testWebhook, Label: "#ci-status"}); err != nil {
		t.Fatal(err)
	}
}

// Setup saves a webhook, never shows it again, and forgets it on Remove; a
// non-Slack address is refused without being repeated.
func TestSlackSetupSaveAndRemove(t *testing.T) {
	s, _, logs := newTestServer(t)
	page := do(s, "GET", keyed("/setup"), host, nil).Body.String()
	for _, want := range []string{`action="/slack/save?key=` + testKey + `"`, `name="webhook" type="password"`, `name="label"`, "https://api.slack.com/apps"} {
		if !strings.Contains(page, want) {
			t.Errorf("Setup lacks %s", want)
		}
	}
	rec := do(s, "POST", keyed("/slack/save"), host, url.Values{"webhook": {"https://example.com/services/T1/B2/" + webhookPart}, "label": {"#ci-status"}})
	if body := rec.Body.String(); rec.Code != 400 || !strings.Contains(body, "not a Slack incoming webhook") ||
		strings.Contains(body, webhookPart) || !strings.Contains(body, `value="#ci-status"`) {
		t.Fatalf("bad address: code %d body %s", rec.Code, body)
	}
	if _, ok, _ := cistatus.LoadSlack(s.dir); ok {
		t.Fatal("a refused address was saved")
	}
	rec = do(s, "POST", keyed("/slack/save"), host, url.Values{"webhook": {" " + testWebhook + "\n"}, "label": {"#ci-status"}})
	if rec.Code != 303 || rec.Header().Get("Location") != "/setup?key="+testKey {
		t.Fatalf("save: code %d location %q", rec.Code, rec.Header().Get("Location"))
	}
	page = do(s, "GET", keyed("/setup"), host, nil).Body.String()
	if !strings.Contains(page, "Send to Slack posts to <b>#ci-status</b>") || !strings.Contains(page, `action="/slack/remove?key=`+testKey+`"`) ||
		strings.Contains(page, `name="webhook"`) || strings.Contains(page, webhookPart) {
		t.Fatalf("after save: %s", page)
	}
	if rec := do(s, "POST", keyed("/slack/remove"), host, url.Values{}); rec.Code != 303 {
		t.Fatalf("remove: %d", rec.Code)
	}
	if _, ok, _ := cistatus.LoadSlack(s.dir); ok {
		t.Fatal("still set after Remove")
	}
	if strings.Contains(logs.String(), webhookPart) {
		t.Errorf("log holds the webhook: %s", logs.String())
	}
}

// The status page always shows Send to Slack: without a webhook it opens
// Setup's Slack section; with one it posts, after a confirmation.
func TestStatusSendButton(t *testing.T) {
	s, f, _ := newTestServer(t)
	serveRepo(f, "b.yml")
	writeConfig(t, s, mainWatch)
	page := do(s, "GET", keyed("/"), host, nil).Body.String()
	if strings.Contains(page, "/slack/send") || !strings.Contains(page, `href="/setup?key=`+testKey+`#slack"`) {
		t.Fatalf("without a webhook Send to Slack should open Setup's Slack section: %s", page)
	}
	if setup := do(s, "GET", keyed("/setup"), host, nil).Body.String(); !strings.Contains(setup, `<h2 id="slack">Slack</h2>`) {
		t.Error("Setup's Slack section has no anchor")
	}
	saveSlack(t, s)
	page = do(s, "GET", keyed("/"), host, nil).Body.String()
	if strings.Contains(page, "#slack") {
		t.Error("with a webhook Send to Slack should post, not open Setup")
	}
	for _, want := range []string{`action="/slack/send?key=` + testKey + `"`, `data-confirm="Send the status to #ci-status?"`, "Send to Slack</button>"} {
		if !strings.Contains(page, want) {
			t.Errorf("status page lacks %s", want)
		}
	}
	if strings.Contains(page, webhookPart) {
		t.Error("status page holds the webhook")
	}
}

// Send to Slack reads GitHub, posts the message, and shows that same status
// once, with a note, without reading GitHub again.
func TestSendToSlack(t *testing.T) {
	s, f, logs := newTestServer(t)
	serveRepo(f, "b.yml")
	writeConfig(t, s, mainWatch)
	saveSlack(t, s)
	in := withSlack(t, s, 200, "ok")
	rec := do(s, "POST", keyed("/slack/send"), host, url.Values{})
	if rec.Code != 303 || rec.Header().Get("Location") != "/?key="+testKey {
		t.Fatalf("send: code %d location %q body %s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	got := in.got()
	if len(got) != 1 {
		t.Fatalf("Slack got %d posts", len(got))
	}
	var msg struct{ Text string }
	if err := json.Unmarshal([]byte(got[0]), &msg); err != nil || msg.Text != "gh-ci-status: nothing failing · 1 passing" || !strings.Contains(got[0], "WF b.yml") {
		t.Fatalf("message %v: %s", err, got[0])
	}
	reads := len(f.Requests())
	page := do(s, "GET", keyed("/"), host, nil).Body.String()
	if !strings.Contains(page, `<p class="notice" id="notice">Sent to #ci-status.</p>`) || !strings.Contains(page, "passing #1") {
		t.Fatalf("page after send: %s", page)
	}
	if n := len(f.Requests()); n != reads {
		t.Errorf("the page after Send read GitHub again: %d more requests", n-reads)
	}
	page = do(s, "GET", keyed("/"), host, nil).Body.String()
	if strings.Contains(page, `id="notice"`) || len(f.Requests()) == reads {
		t.Error("the sent page should be shown once; the next one reads GitHub")
	}
	if strings.Contains(logs.String(), webhookPart) {
		t.Errorf("log holds the webhook: %s", logs.String())
	}
}

// A page kept from Send to Slack is not shown after a minute.
func TestSentPageExpires(t *testing.T) {
	s, f, _ := newTestServer(t)
	serveRepo(f, "b.yml")
	writeConfig(t, s, mainWatch)
	saveSlack(t, s)
	withSlack(t, s, 200, "ok")
	do(s, "POST", keyed("/slack/send"), host, url.Values{})
	s.now = func() time.Time { return testNow.Add(61 * time.Second) }
	reads := len(f.Requests())
	if page := do(s, "GET", keyed("/"), host, nil).Body.String(); strings.Contains(page, `id="notice"`) || len(f.Requests()) == reads {
		t.Error("a sent page older than a minute was shown")
	}
}

// When Slack refuses, the page says so with Slack's answer.
func TestSendToSlackRefused(t *testing.T) {
	s, f, _ := newTestServer(t)
	serveRepo(f, "b.yml")
	writeConfig(t, s, mainWatch)
	saveSlack(t, s)
	withSlack(t, s, 404, "no_service")
	do(s, "POST", keyed("/slack/send"), host, url.Values{})
	page := do(s, "GET", keyed("/"), host, nil).Body.String()
	if !strings.Contains(page, `<p class="notice bad" id="notice">Not sent to #ci-status: Slack answered 404: no_service</p>`) {
		t.Fatalf("page: %s", page)
	}
}

func TestSendToSlackNotSetUp(t *testing.T) {
	s, f, _ := newTestServer(t)
	serveRepo(f, "b.yml")
	writeConfig(t, s, mainWatch)
	if rec := do(s, "POST", keyed("/slack/send"), host, url.Values{}); rec.Code != 400 || !strings.Contains(rec.Body.String(), "Settings") {
		t.Fatalf("code %d body %s", rec.Code, rec.Body.String())
	}
	if rec := do(s, "GET", keyed("/slack/send"), host, nil); rec.Code != 405 {
		t.Errorf("GET: %d", rec.Code)
	}
	if n := len(f.Requests()); n != 0 {
		t.Errorf("read GitHub without a webhook: %d requests", n)
	}
}
