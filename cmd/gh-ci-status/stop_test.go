// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/edespino/gh-ci-status/internal/fakegithub"
)

// freePort returns a port nothing listens on.
func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	ln.Close()
	return port
}

func TestStopStopsServer(t *testing.T) {
	e := newCLI(t, fakegithub.New(t))
	u, stopServe := startServe(t, e, "--no-open", "--port", "0")
	port := urlRe.FindStringSubmatch(u)[1]
	e.out = &syncBuf{}
	e.d.stdout = e.out
	if code := run(context.Background(), []string{"stop", "--port", port}, e.d); code != 0 {
		t.Fatalf("stop: exit %d out %q err %q", code, e.out.String(), e.errOut.String())
	}
	if !strings.Contains(e.out.String(), "Stopped the gh-ci-status server on port "+port) {
		t.Errorf("stop said %q", e.out.String())
	}
	if code := stopServe(); code != 0 {
		t.Errorf("serve exit %d", code)
	}
	if c, err := net.Dial("tcp", "127.0.0.1:"+port); err == nil {
		c.Close()
		t.Fatal("the port is still open after stop")
	}
}

// Nothing to stop is not an error, so scripts can call stop safely.
func TestStopNothingRunning(t *testing.T) {
	e := newCLI(t, fakegithub.New(t))
	if _, err := os.Stat(e.d.dir); err != nil {
		_ = os.MkdirAll(e.d.dir, 0o700)
	}
	if err := os.WriteFile(filepath.Join(e.d.dir, "key"), []byte(strings.Repeat("ab", 32)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	if code := run(context.Background(), []string{"stop", "--port", port}, e.d); code != 0 || !strings.Contains(e.out.String(), "No gh-ci-status server is running on port "+port) {
		t.Fatalf("exit %d out %q err %q", code, e.out.String(), e.errOut.String())
	}
}

func TestStopWrongKey(t *testing.T) {
	e := newCLI(t, fakegithub.New(t))
	u, stopServe := startServe(t, e, "--no-open", "--port", "0")
	defer stopServe()
	port := urlRe.FindStringSubmatch(u)[1]
	if err := os.WriteFile(filepath.Join(e.d.dir, "key"), []byte(strings.Repeat("cd", 32)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run(context.Background(), []string{"stop", "--port", port}, e.d); code != 1 || !strings.Contains(e.errOut.String(), "did not accept the key") {
		t.Fatalf("exit %d err %q", code, e.errOut.String())
	}
	if c, err := net.Dial("tcp", "127.0.0.1:"+port); err != nil {
		t.Fatal("the server stopped without the right key")
	} else {
		c.Close()
	}
}

// Something that is not a gh-ci-status with stop (another program, or an
// older gh-ci-status) is left alone and the message says so.
func TestStopOtherProgram(t *testing.T) {
	e := newCLI(t, fakegithub.New(t))
	if err := os.WriteFile(filepath.Join(e.d.dir, "key"), []byte(strings.Repeat("ab", 32)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	other := httptest.NewServer(http.NotFoundHandler())
	defer other.Close()
	port := other.Listener.Addr().(*net.TCPAddr).Port
	if code := run(context.Background(), []string{"stop", "--port", strconv.Itoa(port)}, e.d); code != 1 || !strings.Contains(e.errOut.String(), "not stopped") {
		t.Fatalf("exit %d err %q", code, e.errOut.String())
	}
}

func TestStopWithoutKey(t *testing.T) {
	e := newCLI(t, fakegithub.New(t))
	if code := run(context.Background(), []string{"stop", "--port", freePort(t)}, e.d); code != 1 || !strings.Contains(e.errOut.String(), "no key") {
		t.Fatalf("exit %d err %q", code, e.errOut.String())
	}
}
