// SPDX-License-Identifier: Apache-2.0

package web

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Settings turns the questions before Remove, Remove all, Watch N shown and
// Send to Slack off and on; every page carries the choice, kept in
// prefs.json.
func TestConfirmSetting(t *testing.T) {
	s, f, _ := newTestServer(t)
	serveRepo(f, "b.yml")
	writeConfig(t, s, mainWatch)
	page := do(s, "GET", keyed("/setup"), host, nil).Body.String()
	for _, want := range []string{`<h2 id="confirm">Confirmations</h2>`, "ask before they act", `action="/settings/confirm?key=` + testKey + `"`,
		`<input type="hidden" name="ask" value="off">`, ">Don't ask</button>", "<body>"} {
		if !strings.Contains(page, want) {
			t.Errorf("Settings lacks %s", want)
		}
	}
	rec := do(s, "POST", keyed("/settings/confirm"), host, url.Values{"ask": {"off"}})
	if rec.Code != 303 || rec.Header().Get("Location") != "/setup?key="+testKey+"#confirm" {
		t.Fatalf("turn off: code %d location %q", rec.Code, rec.Header().Get("Location"))
	}
	if st, err := os.Stat(filepath.Join(s.dir, "prefs.json")); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("prefs.json: %v %v", st, err)
	}
	for _, target := range []string{"/setup", "/", "/configure"} {
		if body := do(s, "GET", keyed(target), host, nil).Body.String(); !strings.Contains(body, "<body data-skip-confirm>") {
			t.Errorf("%s does not carry the setting", target)
		}
	}
	page = do(s, "GET", keyed("/setup"), host, nil).Body.String()
	if !strings.Contains(page, "run without asking") || !strings.Contains(page, `<input type="hidden" name="ask" value="on">`) || !strings.Contains(page, ">Ask first</button>") {
		t.Errorf("Settings after turning off: %s", page)
	}
	do(s, "POST", keyed("/settings/confirm"), host, url.Values{"ask": {"on"}})
	if body := do(s, "GET", keyed("/"), host, nil).Body.String(); !strings.Contains(body, "<body>") {
		t.Error("asking is back on, but the page skips the questions")
	}
	if rec := do(s, "POST", keyed("/settings/confirm"), host, url.Values{"ask": {"maybe"}}); rec.Code != 400 {
		t.Errorf("ask=maybe: %d", rec.Code)
	}
}

// An unreadable prefs.json keeps the questions on and says why.
func TestConfirmSettingUnreadable(t *testing.T) {
	s, _, _ := newTestServer(t)
	if err := os.WriteFile(filepath.Join(s.dir, "prefs.json"), []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	page := do(s, "GET", keyed("/setup"), host, nil).Body.String()
	if !strings.Contains(page, "<body>") || !strings.Contains(page, "prefs.json") {
		t.Errorf("unreadable prefs: %s", page)
	}
}
