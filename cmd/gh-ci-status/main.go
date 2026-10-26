// SPDX-License-Identifier: Apache-2.0

// Command gh-ci-status shows the current state of chosen GitHub Actions
// workflows on chosen branches: in the terminal, as JSON, or as a local web
// page. See README.md.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	cistatus "github.com/edespino/gh-ci-status"
	"github.com/edespino/gh-ci-status/internal/banner"
	"github.com/edespino/gh-ci-status/internal/web"
)

const usage = `gh-ci-status: the current state of chosen GitHub Actions workflows, per branch.

It is a GitHub CLI extension; install it with 'gh extension install edespino/gh-ci-status'
and run it as 'gh ci-status'. The binary also runs on its own as 'gh-ci-status'.

Usage:
  gh ci-status                  print the status of everything you watch
  gh ci-status --json           the same as JSON, for scripts and tools
  gh ci-status serve [--port N] [--no-open] [--new-key]
                                serve the web page on http://127.0.0.1:N/ (default 8723)
                                and open it; --no-open does not open the browser;
                                --new-key replaces the saved key (old bookmarks stop working)
  gh ci-status add OWNER/REPO   check a repository with GitHub and add it
  gh ci-status check            check the GitHub CLI, your login and each repository
  gh ci-status --version
  gh ci-status --help

First use: run 'gh ci-status serve' and follow the Setup page, or run
'gh ci-status add OWNER/REPO' and then choose workflows in the web page.

gh-ci-status reads GitHub with your GitHub CLI login and keeps its settings in
$XDG_CONFIG_HOME/gh-ci-status or ~/.config/gh-ci-status.

Exit codes: 0 everything was read, 1 something could not be read or a check
failed, 2 a usage error or nothing set up yet.
`

const defaultPort = 8723

const notSetUp = "gh-ci-status: no repository set up yet. Run 'gh ci-status serve' and use the Setup page,\n" +
	"or run 'gh ci-status add OWNER/REPO'."

// deps holds everything run touches outside the process, so tests can
// replace it.
type deps struct {
	stdout, stderr io.Writer
	token          func(context.Context) (string, error)
	lookPath       func(string) (string, error)
	baseURL, dir   string
	now            func() time.Time
	open           func(string) error
	listen         func(addr string) (net.Listener, error)
	isTerminal     func() bool         // nil: not a terminal
	getenv         func(string) string // nil: empty environment
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	dir, err := cistatus.ConfigDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "gh-ci-status:", err)
		os.Exit(1)
	}
	code := run(ctx, os.Args[1:], deps{
		stdout: os.Stdout, stderr: os.Stderr,
		token: cistatus.GHToken, lookPath: exec.LookPath,
		baseURL: cistatus.DefaultAPIURL, dir: dir,
		now: time.Now, open: openBrowser,
		listen:     func(addr string) (net.Listener, error) { return net.Listen("tcp", addr) },
		isTerminal: stdoutIsTerminal, getenv: os.Getenv,
	})
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, d deps) int {
	if len(args) == 0 {
		return status(ctx, d, false)
	}
	switch args[0] {
	case "--json":
		return status(ctx, d, true)
	case "-h", "--help", "help":
		fmt.Fprint(d.stdout, usage)
		return 0
	case "--version", "version":
		fmt.Fprintln(d.stdout, "gh-ci-status", version())
		return 0
	case "serve":
		return serve(ctx, args[1:], d)
	case "check":
		return check(ctx, d)
	case "add":
		return add(ctx, args[1:], d)
	}
	fmt.Fprintf(d.stderr, "gh-ci-status: unknown command %q\n\n%s", args[0], usage)
	return 2
}

// status prints what is watched, as text or as the JSON report.
func status(ctx context.Context, d deps, asJSON bool) int {
	cfg, _, err := cistatus.LoadConfig(d.dir)
	if err != nil {
		fmt.Fprintln(d.stderr, "gh-ci-status:", err)
		return 1
	}
	if len(cfg.Repos) == 0 {
		if asJSON {
			writeJSON(d.stdout, cistatus.NewReport(nil, nil, d.now()))
		}
		fmt.Fprintln(d.stderr, notSetUp)
		return 2
	}
	token, err := d.token(ctx)
	if err != nil {
		fmt.Fprintln(d.stderr, "gh-ci-status:", err)
		return 1
	}
	client := cistatus.NewClient(token, d.baseURL)
	var rows []cistatus.Row
	for _, rc := range cfg.Repos {
		rows = append(rows, cistatus.Collect(ctx, client, rc.Repo, rc.Watch, d.now())...)
	}
	rows = cistatus.SortRows(rows)
	if asJSON {
		writeJSON(d.stdout, cistatus.NewReport(cfg.RepoNames(), rows, d.now()))
	} else {
		writeText(d.stdout, rows, len(cfg.Repos) > 1)
	}
	if len(rows) == 0 {
		fmt.Fprintln(d.stderr, "gh-ci-status: no workflows watched yet. Run 'gh ci-status serve' and choose them on the Configure page.")
	}
	for _, r := range rows {
		if r.State == cistatus.Error {
			return 1
		}
	}
	return 0
}

func writeJSON(w io.Writer, v any) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// prerequisites runs the GitHub CLI, login and API checks in order, printing
// each, and returns a client when they all pass.
func prerequisites(ctx context.Context, d deps) (*cistatus.Client, bool) {
	fmt.Fprintln(d.stdout, cistatus.CheckGH(d.lookPath))
	login, token := cistatus.CheckLogin(ctx, d.token)
	fmt.Fprintln(d.stdout, login)
	if !login.OK {
		return nil, false
	}
	client := cistatus.NewClient(token, d.baseURL)
	api := cistatus.CheckAPI(ctx, client)
	fmt.Fprintln(d.stdout, api)
	return client, api.OK
}

// check reports whether ci-status can work: the GitHub CLI, the login, the
// API, and each set-up repository.
func check(ctx context.Context, d deps) int {
	client, ok := prerequisites(ctx, d)
	if !ok {
		return 1
	}
	cfg, _, err := cistatus.LoadConfig(d.dir)
	if err != nil {
		fmt.Fprintln(d.stdout, cistatus.Check{Name: "Configuration", Detail: err.Error(),
			Fix: "Fix or delete the file, then run 'gh ci-status add OWNER/REPO'"})
		return 1
	}
	if len(cfg.Repos) == 0 {
		fmt.Fprintln(d.stdout, cistatus.Check{Name: "Repositories", Detail: "none set up yet",
			Fix: "Run 'gh ci-status serve' and use the Setup page, or run 'gh ci-status add OWNER/REPO'"})
		return 1
	}
	code := 0
	for _, repo := range cfg.RepoNames() {
		c, _ := cistatus.CheckRepo(ctx, client, repo)
		fmt.Fprintln(d.stdout, c)
		if !c.OK {
			code = 1
		}
	}
	return code
}

// add checks a repository with GitHub and saves it under GitHub's spelling.
func add(ctx context.Context, args []string, d deps) int {
	if len(args) != 1 {
		fmt.Fprintf(d.stderr, "gh-ci-status: add takes one repository, as OWNER/REPO\n\n%s", usage)
		return 2
	}
	cfg, _, err := cistatus.LoadConfig(d.dir)
	if err != nil {
		fmt.Fprintln(d.stderr, "gh-ci-status:", err)
		fmt.Fprintln(d.stderr, "gh-ci-status: fix or delete that file first; nothing was changed")
		return 1
	}
	token, err := d.token(ctx)
	if err != nil {
		fmt.Fprintln(d.stderr, "gh-ci-status:", err)
		return 1
	}
	c, name := cistatus.CheckRepo(ctx, cistatus.NewClient(token, d.baseURL), cistatus.ParseRepo(args[0]))
	fmt.Fprintln(d.stdout, c)
	if !c.OK {
		return 1
	}
	if cfg.HasRepo(name) {
		fmt.Fprintf(d.stdout, "%s is already set up.\n", name)
		return 0
	}
	if err := cistatus.SaveConfig(d.dir, cfg.AddRepo(name)); err != nil {
		fmt.Fprintln(d.stderr, "gh-ci-status:", err)
		return 1
	}
	fmt.Fprintf(d.stdout, "Added %s. Next: run 'gh ci-status serve' and choose its workflows on the Configure page.\n", name)
	return 0
}

func serve(ctx context.Context, args []string, d deps) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(d.stderr)
	port := fs.Int("port", defaultPort, "port on 127.0.0.1")
	noOpen := fs.Bool("no-open", false, "do not open the browser")
	newKey := fs.Bool("new-key", false, "replace the saved key")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if d.isTerminal != nil && d.isTerminal() {
		getenv := d.getenv
		if getenv == nil {
			getenv = func(string) string { return "" }
		}
		fmt.Fprint(d.stdout, banner.Terminal(banner.UseColor(getenv)), "\n")
	}
	if _, ok := prerequisites(ctx, d); !ok {
		fmt.Fprintln(d.stderr, "gh-ci-status: fix the check above, then start again")
		return 1
	}
	key, err := web.LoadOrCreateKey(d.dir, *newKey)
	if err != nil {
		fmt.Fprintln(d.stderr, "gh-ci-status:", err)
		return 1
	}
	ln, err := d.listen(fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		if strings.Contains(err.Error(), "address already in use") {
			fmt.Fprintf(d.stderr, "gh-ci-status: port %d is in use; start with --port N to use another\n", *port)
		} else {
			fmt.Fprintln(d.stderr, "gh-ci-status:", err)
		}
		return 1
	}
	actual := ln.Addr().(*net.TCPAddr).Port
	url := fmt.Sprintf("http://127.0.0.1:%d/?key=%s", actual, key)

	s := web.New(web.Options{
		Client: func(ctx context.Context) (*cistatus.Client, error) {
			t, err := d.token(ctx) // read per request, so a new 'gh auth login' needs no restart
			if err != nil {
				return nil, err
			}
			return cistatus.NewClient(t, d.baseURL), nil
		},
		Dir: d.dir, Key: key, Port: actual, Now: d.now,
		Logger: log.New(d.stderr, "gh-ci-status: ", log.LstdFlags),
	})
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()

	if cfg, _, err := cistatus.LoadConfig(d.dir); err == nil && len(cfg.Repos) == 0 {
		fmt.Fprintln(d.stdout, "No repository set up yet: the page opens on Setup.")
	}
	fmt.Fprintf(d.stdout, "gh-ci-status: serving %s\nBookmark that address; press Ctrl-C to stop.\n", url)
	if !*noOpen {
		if err := d.open(url); err != nil {
			fmt.Fprintln(d.stderr, "gh-ci-status: could not open the browser:", err)
		}
	}

	select {
	case <-ctx.Done():
	case err := <-served:
		if !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(d.stderr, "gh-ci-status:", err)
			return 1
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	return 0
}

// version is the module version (set by 'go install module@version') and the
// commit it was built from, when known.
func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "(unknown)"
	}
	v := info.Main.Version
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && len(s.Value) >= 12 {
			v += " " + s.Value[:12]
		}
	}
	return v
}

func openBrowser(u string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	return exec.Command(name, u).Start()
}

// stdoutIsTerminal reports whether standard output is a terminal rather than
// a pipe or file.
func stdoutIsTerminal() bool {
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
