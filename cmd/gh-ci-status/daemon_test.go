// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/edespino/gh-ci-status/internal/fakegithub"
)

// daemonCLI is a CLI whose background server is this test binary in helper
// mode, reading the fake GitHub f. Every server its serve.log names is
// stopped when the test ends, passed or failed.
func daemonCLI(t *testing.T, f *fakegithub.Server, extraEnv ...string) *cliEnv {
	t.Helper()
	e := newCLI(t, f)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	e.d.executable = func() (string, error) { return exe, nil }
	e.d.childEnv = append([]string{"GH_CI_STATUS_HELPER=1", "GH_CI_STATUS_HELPER_BASEURL=" + f.URL(), "GH_CI_STATUS_HELPER_DIR=" + e.d.dir}, extraEnv...)
	t.Cleanup(func() {
		data, _ := os.ReadFile(filepath.Join(e.d.dir, "serve.log"))
		for _, m := range servingLine.FindAllStringSubmatch(string(data), -1) {
			d := e.d
			d.stdout, d.stderr = io.Discard, io.Discard
			run(context.Background(), []string{"stop", "--port", m[2]}, d)
		}
	})
	return e
}

// serve --daemon returns once the background server is serving, prints its
// address, logs to a private file, and gh ci-status stop ends it.
func TestServeDaemon(t *testing.T) {
	e := daemonCLI(t, fakegithub.New(t))
	if code := run(context.Background(), []string{"serve", "--daemon", "--no-open", "--port", "0"}, e.d); code != 0 {
		t.Fatalf("serve --daemon: exit %d out %q err %q", code, e.out.String(), e.errOut.String())
	}
	m := urlRe.FindStringSubmatch(e.out.String())
	if m == nil || !strings.Contains(e.out.String(), "running in the background") || !strings.Contains(e.out.String(), "gh ci-status stop --port "+m[1]) {
		t.Fatalf("output %q", e.out.String())
	}
	port := m[1]
	log := filepath.Join(e.d.dir, "serve.log")
	st, err := os.Stat(log)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("log %v %v", st, err)
	}
	if data, _ := os.ReadFile(log); !strings.Contains(string(data), "serving http://127.0.0.1:"+port) {
		t.Fatalf("log lacks the address: %s", data)
	}
	if _, err := os.Stat(filepath.Join(e.d.dir, "opened")); err == nil {
		t.Error("--no-open was not passed to the background server: it opened the browser")
	}
	e.out = &syncBuf{}
	e.d.stdout = e.out
	if code := run(context.Background(), []string{"stop", "--port", port}, e.d); code != 0 || !strings.Contains(e.out.String(), "Stopped") {
		t.Fatalf("stop: exit %d out %q err %q", code, e.out.String(), e.errOut.String())
	}
}

// Without --no-open the background server opens the browser, and --new-key
// reaches it: it replaces the saved key, and stop uses the new one.
func TestServeDaemonOpensAndNewKey(t *testing.T) {
	e := daemonCLI(t, fakegithub.New(t))
	old := strings.Repeat("ab", 32)
	if err := os.MkdirAll(e.d.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.d.dir, "key"), []byte(old+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run(context.Background(), []string{"serve", "--daemon", "--new-key", "--port", "0"}, e.d); code != 0 {
		t.Fatalf("serve --daemon: exit %d out %q err %q", code, e.out.String(), e.errOut.String())
	}
	m := urlRe.FindStringSubmatch(e.out.String())
	if m == nil {
		t.Fatalf("output %q", e.out.String())
	}
	port := m[1]
	if strings.Contains(e.out.String(), old) {
		t.Errorf("--new-key was not passed to the background server: it kept the old key: %q", e.out.String())
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if _, err := os.Stat(filepath.Join(e.d.dir, "opened")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the background server did not open the browser")
		}
	}
	if code := run(context.Background(), []string{"stop", "--port", port}, e.d); code != 0 {
		t.Fatalf("stop: exit %d err %q", code, e.errOut.String())
	}
}

// When the background server cannot start, serve --daemon says so, with the
// reason from the log, instead of returning as if it had started; an earlier
// start's address in the log does not count.
func TestServeDaemonReportsFailure(t *testing.T) {
	e := daemonCLI(t, fakegithub.New(t), "GH_CI_STATUS_HELPER_NO_LOGIN=1")
	if err := os.MkdirAll(e.d.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	earlier := "gh-ci-status: serving http://127.0.0.1:1/?key=" + strings.Repeat("cd", 32) + "\n"
	if err := os.WriteFile(filepath.Join(e.d.dir, "serve.log"), []byte(earlier), 0o600); err != nil {
		t.Fatal(err)
	}
	code := run(context.Background(), []string{"serve", "--daemon", "--no-open", "--port", "0"}, e.d)
	if code != 1 || !strings.Contains(e.errOut.String(), "did not start") || !strings.Contains(e.errOut.String(), "FAIL  GitHub login") {
		t.Fatalf("exit %d out %q err %q", code, e.out.String(), e.errOut.String())
	}
}
