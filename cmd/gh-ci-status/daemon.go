// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var servingLine = regexp.MustCompile(`serving (http://127\.0\.0\.1:(\d+)/\?key=[0-9a-f]+)`)

// daemon starts "gh-ci-status serve" again as a background process, detached
// from the terminal, with its output appended to serve.log in the settings
// folder (readable only by the user: it holds the address). It returns once
// the server is serving, or reports why it did not start.
func daemon(serveArgs []string, d deps) int {
	exe, err := d.executable()
	if err != nil {
		fmt.Fprintln(d.stderr, "gh-ci-status: cannot find its own program to start in the background:", err)
		return 1
	}
	if err := os.MkdirAll(d.dir, 0o700); err != nil {
		fmt.Fprintln(d.stderr, "gh-ci-status:", err)
		return 1
	}
	logPath := filepath.Join(d.dir, "serve.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		fmt.Fprintln(d.stderr, "gh-ci-status:", err)
		return 1
	}
	_ = logFile.Chmod(0o600)
	st, err := logFile.Stat()
	if err != nil {
		logFile.Close()
		fmt.Fprintln(d.stderr, "gh-ci-status:", err)
		return 1
	}
	start := st.Size() // read only what this start writes

	cmd := exec.Command(exe, append([]string{"serve"}, serveArgs...)...)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.Env = append(os.Environ(), d.childEnv...)
	cmd.SysProcAttr = detached()
	err = cmd.Start()
	logFile.Close()
	if err != nil {
		fmt.Fprintln(d.stderr, "gh-ci-status: cannot start in the background:", err)
		return 1
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()

	output := func() string {
		data, _ := os.ReadFile(logPath)
		if int64(len(data)) < start {
			return ""
		}
		return string(data[start:])
	}
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if m := servingLine.FindStringSubmatch(output()); m != nil {
			fmt.Fprintf(d.stdout, "gh-ci-status is running in the background:\n  %s\nLog: %s\nStop it with: gh ci-status stop --port %s\n", m[1], logPath, m[2])
			return 0
		}
		select {
		case <-exited:
			fmt.Fprintf(d.stderr, "gh-ci-status: the background server did not start:\n%s", indent(output()))
			return 1
		default:
		}
	}
	fmt.Fprintf(d.stderr, "gh-ci-status: the background server has not started after 30 seconds; see %s\n", logPath)
	return 1
}

func indent(s string) string {
	var b strings.Builder
	for _, line := range strings.SplitAfter(s, "\n") {
		if line != "" {
			b.WriteString("  " + line)
		}
	}
	return b.String()
}
