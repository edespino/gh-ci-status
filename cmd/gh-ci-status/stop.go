// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/edespino/gh-ci-status/internal/web"
)

// stop asks the gh-ci-status server on a port to shut down, through its
// key-protected /shutdown route, and waits until the port is free. It never
// signals processes, so it cannot stop anything else by mistake.
func stop(ctx context.Context, args []string, d deps) int {
	fs := flag.NewFlagSet("stop", flag.ContinueOnError)
	fs.SetOutput(d.stderr)
	port := fs.Int("port", defaultPort, "port on 127.0.0.1")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if _, err := os.Stat(filepath.Join(d.dir, "key")); err != nil {
		fmt.Fprintf(d.stderr, "gh-ci-status: cannot stop: no key in %s, so no server was started with these settings\n", d.dir)
		return 1
	}
	key, err := web.LoadOrCreateKey(d.dir, false)
	if err != nil {
		fmt.Fprintln(d.stderr, "gh-ci-status:", err)
		return 1
	}
	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+addr+"/shutdown?key="+key, nil)
	if err != nil {
		fmt.Fprintln(d.stderr, "gh-ci-status:", err)
		return 1
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		var opErr *net.OpError
		if errors.As(err, &opErr) && opErr.Op == "dial" {
			fmt.Fprintf(d.stdout, "No gh-ci-status server is running on port %d.\n", *port)
			return 0
		}
		fmt.Fprintf(d.stderr, "gh-ci-status: could not reach port %d (%v); not stopped\n", *port, err)
		return 1
	}
	resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusAccepted:
	case http.StatusForbidden:
		fmt.Fprintf(d.stderr, "gh-ci-status: the server on port %d did not accept the key in %s (started with other settings, or before 'serve --new-key'?); not stopped\n", *port, d.dir)
		return 1
	default:
		fmt.Fprintf(d.stderr, "gh-ci-status: port %d answered %q: another program, or a gh-ci-status older than this stop command; not stopped.\n"+
			"Stop it with Ctrl-C in its terminal, or: kill -INT $(lsof -tiTCP:%d -sTCP:LISTEN)\n", *port, resp.Status, *port)
		return 1
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err != nil {
			fmt.Fprintf(d.stdout, "Stopped the gh-ci-status server on port %d.\n", *port)
			return 0
		}
		c.Close()
	}
	fmt.Fprintf(d.stderr, "gh-ci-status: the server on port %d accepted the stop but is still running after 10 seconds\n", *port)
	return 1
}
