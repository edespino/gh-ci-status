// SPDX-License-Identifier: Apache-2.0

// Package web serves gh-ci-status's local pages: the status page, Configure (what
// to watch in one repository) and Setup (which repositories, and whether the
// GitHub login works).
package web

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	cistatus "github.com/OWNER/gh-ci-status"
)

// Options configures a Server.
type Options struct {
	// Client returns a GitHub client; it is called for every page that reads
	// GitHub, so a new 'gh auth login' takes effect without a restart.
	Client func(context.Context) (*cistatus.Client, error)
	Dir    string           // config directory (config.json)
	Key    string           // the secret every URL must carry
	Port   int              // the port the server listens on, for the Host check
	Now    func() time.Time // nil: time.Now
	Logger *log.Logger      // nil: no request log
}

// Server serves the pages on 127.0.0.1 to whoever holds the key. The key
// travels in every URL (link, form action, redirect); there is no cookie,
// because browsers send cookies to every port on 127.0.0.1.
type Server struct {
	client func(context.Context) (*cistatus.Client, error)
	dir    string
	key    string
	port   int
	now    func() time.Time
	logger *log.Logger

	mu   sync.Mutex
	kept map[string]*kept // per repository: what Configure read, until Refresh
}

// New returns a Server for o.
func New(o Options) *Server {
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Server{client: o.Client, dir: o.Dir, key: o.Key, port: o.Port, now: o.Now, logger: o.Logger}
}

// Handler is the server's HTTP handler, with the key and Host checks.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.status)
	mux.HandleFunc("POST /refresh", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, s.withKey("/"), http.StatusSeeOther)
	})
	mux.HandleFunc("GET /setup", s.setupPage)
	mux.HandleFunc("POST /setup/add", s.addRepo)
	mux.HandleFunc("POST /setup/remove", s.removeRepo)
	mux.HandleFunc("GET /configure", s.configureForm)
	mux.HandleFunc("POST /configure/refresh", s.refreshConfigure)
	mux.HandleFunc("GET /api/row", s.apiRow)
	mux.HandleFunc("POST /watch", s.watch)
	mux.HandleFunc("POST /unwatch", s.unwatch)
	mux.HandleFunc("POST /watch-failing", s.watchFailing)
	mux.HandleFunc("POST /unwatch-workflow", s.unwatchWorkflow)
	mux.HandleFunc("POST /unwatch-all", s.unwatchAll)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "ok")
	})
	return s.guard(mux)
}

// guard sets security headers, logs the request path (never the query, which
// holds the key), and admits only requests for this host and port whose URL
// carries the key.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Security-Policy",
			"default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		rec := &statusRecorder{ResponseWriter: w, code: http.StatusOK}
		defer func() {
			if s.logger != nil {
				s.logger.Printf("%s %s %d", r.Method, r.URL.Path, rec.code)
			}
		}()

		if r.Host != fmt.Sprintf("127.0.0.1:%d", s.port) && r.Host != fmt.Sprintf("localhost:%d", s.port) {
			http.Error(rec, "forbidden: unexpected Host", http.StatusForbidden)
			return
		}
		if !s.validKey(r.URL.Query().Get("key")) {
			http.Error(rec, "forbidden: open the URL printed by 'gh ci-status serve'", http.StatusForbidden)
			return
		}
		next.ServeHTTP(rec, r)
	})
}

func (s *Server) withKey(path string) string { return path + "?key=" + s.key }

// configureURL is Configure for repo, with the key.
func (s *Server) configureURL(repo string) string {
	return s.withKey("/configure") + "&repo=" + url.QueryEscape(repo)
}

func (s *Server) validKey(v string) bool {
	return subtle.ConstantTimeCompare([]byte(v), []byte(s.key)) == 1
}

type statusRecorder struct {
	http.ResponseWriter
	code int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.code = code
	r.ResponseWriter.WriteHeader(code)
}

// page renders into a buffer first so a template error becomes a clean 500.
func (s *Server) page(w http.ResponseWriter, code int, render func(io.Writer) error) {
	var b bytes.Buffer
	if err := render(&b); err != nil {
		http.Error(w, "rendering failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write(b.Bytes())
}

// keptFor is what Configure read for repo.
func (s *Server) keptFor(repo string) *kept {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.kept == nil {
		s.kept = map[string]*kept{}
	}
	if s.kept[repo] == nil {
		s.kept[repo] = &kept{}
	}
	return s.kept[repo]
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	generated := localTime(s.now())
	fail := func(code int, summary, msg string) {
		s.page(w, code, func(out io.Writer) error {
			return renderStatus(out, StatusPage{Summary: summary, Generated: generated, Key: s.key, Error: msg})
		})
	}
	cfg, _, err := cistatus.LoadConfig(s.dir)
	if err != nil {
		fail(http.StatusInternalServerError, "configuration unreadable", err.Error()+". Open Setup and add a repository to replace it.")
		return
	}
	if len(cfg.Repos) == 0 {
		http.Redirect(w, r, s.withKey("/setup"), http.StatusSeeOther)
		return
	}
	client, err := s.client(ctx)
	if err != nil {
		fail(http.StatusBadGateway, "cannot read GitHub", err.Error())
		return
	}
	var rows []cistatus.Row
	for _, rc := range cfg.Repos {
		rows = append(rows, cistatus.Collect(ctx, client, rc.Repo, rc.Watch, s.now())...)
	}
	rows = cistatus.SortRows(rows)
	for _, row := range rows {
		s.keptFor(row.Repo).setRowStatus(row.Watch, rowStatus(row))
	}
	s.page(w, http.StatusOK, func(out io.Writer) error {
		return renderStatus(out, StatusPage{Repos: cfg.RepoNames(), Summary: cistatus.Summary(rows), Generated: generated, Key: s.key, Rows: rows})
	})
}

func (s *Server) configureForm(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg, _, err := cistatus.LoadConfig(s.dir)
	if err != nil || len(cfg.Repos) == 0 {
		http.Redirect(w, r, s.withKey("/setup"), http.StatusSeeOther)
		return
	}
	repo := r.URL.Query().Get("repo")
	if repo == "" {
		repo = cfg.Repos[0].Repo
	}
	if !cfg.HasRepo(repo) {
		http.Error(w, fmt.Sprintf("%q is not set up; add it on the Setup page", repo), http.StatusNotFound)
		return
	}
	p := ConfigurePage{Repo: repo, Repos: cfg.RepoNames(), Key: s.key}
	k := s.keptFor(repo)
	l := k.listing()
	if l == nil {
		client, err := s.client(ctx)
		if err == nil {
			l, err = readListing(ctx, client, repo, s.now())
		}
		if err != nil {
			p.Error = err.Error()
			s.page(w, http.StatusBadGateway, func(out io.Writer) error { return renderConfigure(out, p) })
			return
		}
		k.setListing(l)
	}
	p.DefaultBranch, p.ReadAt, p.Error = l.def, readTime(l.readAt, s.now()), l.note
	p.Watching, p.Items = configureModel(l.workflows, l.recent, repo, cfg.Watches(repo), l.def)
	s.page(w, http.StatusOK, func(out io.Writer) error { return renderConfigure(out, p) })
}

func (s *Server) refreshConfigure(w http.ResponseWriter, r *http.Request) {
	repo := r.FormValue("repo")
	if !cistatus.ValidRepo(repo) {
		http.Error(w, "repository is missing or not valid", http.StatusBadRequest)
		return
	}
	s.keptFor(repo).clear()
	http.Redirect(w, r, s.configureURL(repo), http.StatusSeeOther)
}

// apiRow answers the Configure page's status dots: one workflow on one
// branch of a set-up repository, without the jobs lookup, from what is kept
// when it can.
func (s *Server) apiRow(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	repo, wf, branch := q.Get("repo"), q.Get("workflow"), q.Get("branch")
	if !cistatus.ValidWorkflowFile(wf) || !cistatus.ValidBranch(branch) {
		http.Error(w, "workflow and branch are required and must be valid", http.StatusBadRequest)
		return
	}
	if cfg, _, err := cistatus.LoadConfig(s.dir); err != nil || !cfg.HasRepo(repo) {
		http.Error(w, fmt.Sprintf("%q is not set up", repo), http.StatusBadRequest)
		return
	}
	pair := cistatus.Watch{Workflow: wf, Branch: branch}
	k := s.keptFor(repo)
	st, ok := k.rowStatus(pair)
	if state, off := k.disabled(wf); !ok && off {
		// Turned off on GitHub, as the listing already read says: no request.
		st = rowStatus(cistatus.Row{Repo: repo, Watch: pair, State: cistatus.Disabled, WorkflowState: state})
		ok = true
	}
	if !ok {
		if client, err := s.client(r.Context()); err != nil {
			st = RowStatus{State: cistatus.Error, Text: err.Error(), URL: cistatus.HistoryURL(repo, wf, branch)}
		} else {
			st = rowStatus(cistatus.ReadRow(r.Context(), client, repo, pair, wf, s.now(), false))
		}
		k.setRowStatus(pair, st)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(st)
}

func (s *Server) watch(w http.ResponseWriter, r *http.Request) {
	s.change(w, r, func(form url.Values, repo string, _ func() (string, error)) (func(cistatus.Config) cistatus.Config, error) {
		pair, err := formWatch(form)
		if err != nil {
			return nil, err
		}
		return func(c cistatus.Config) cistatus.Config { return c.AddWatches(repo, pair) }, nil
	})
}

func (s *Server) unwatch(w http.ResponseWriter, r *http.Request) {
	s.change(w, r, func(form url.Values, repo string, _ func() (string, error)) (func(cistatus.Config) cistatus.Config, error) {
		pair, err := formWatch(form)
		if err != nil {
			return nil, err
		}
		return func(c cistatus.Config) cistatus.Config { return c.RemoveWatch(repo, pair) }, nil
	})
}

// watchFailing adds the posted workflows (the ones the page saw failing) on
// the default branch.
func (s *Server) watchFailing(w http.ResponseWriter, r *http.Request) {
	s.change(w, r, func(form url.Values, repo string, defaultBranch func() (string, error)) (func(cistatus.Config) cistatus.Config, error) {
		for _, f := range form["workflow"] {
			if !cistatus.ValidWorkflowFile(f) {
				return nil, fmt.Errorf("workflow file %q is not valid", f)
			}
		}
		def, err := defaultBranch()
		if err != nil {
			return nil, err
		}
		var pairs []cistatus.Watch
		for _, f := range form["workflow"] {
			pairs = append(pairs, cistatus.Watch{Workflow: f, Branch: def})
		}
		return func(c cistatus.Config) cistatus.Config { return c.AddWatches(repo, pairs...) }, nil
	})
}

// unwatchWorkflow stops watching one workflow on every branch.
func (s *Server) unwatchWorkflow(w http.ResponseWriter, r *http.Request) {
	s.change(w, r, func(form url.Values, repo string, _ func() (string, error)) (func(cistatus.Config) cistatus.Config, error) {
		wf := form.Get("workflow")
		if !cistatus.ValidWorkflowFile(wf) {
			return nil, fmt.Errorf("workflow file %q is not valid", wf)
		}
		return func(c cistatus.Config) cistatus.Config { return c.RemoveWorkflow(repo, wf) }, nil
	})
}

// unwatchAll empties the repository's list; the repository stays set up.
func (s *Server) unwatchAll(w http.ResponseWriter, r *http.Request) {
	s.change(w, r, func(_ url.Values, repo string, _ func() (string, error)) (func(cistatus.Config) cistatus.Config, error) {
		return func(c cistatus.Config) cistatus.Config { return c.SetWatches(repo, nil) }, nil
	})
}

func formWatch(form url.Values) (cistatus.Watch, error) {
	wf, branch := form.Get("workflow"), strings.TrimSpace(form.Get("branch"))
	if !cistatus.ValidWorkflowFile(wf) {
		return cistatus.Watch{}, fmt.Errorf("workflow file %q is not valid", wf)
	}
	if !cistatus.ValidBranch(branch) {
		return cistatus.Watch{}, fmt.Errorf("branch %q is not valid", branch)
	}
	return cistatus.Watch{Workflow: wf, Branch: branch}, nil
}

// change validates a Configure action for the posted repository, applies it
// to the saved selection, saves, and returns to Configure for that repository.
func (s *Server) change(w http.ResponseWriter, r *http.Request,
	prepare func(form url.Values, repo string, defaultBranch func() (string, error)) (func(cistatus.Config) cistatus.Config, error)) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form: "+err.Error(), http.StatusBadRequest)
		return
	}
	repo := r.PostForm.Get("repo")
	cfg, _, err := cistatus.LoadConfig(s.dir)
	if err != nil {
		http.Error(w, err.Error()+"\n\nNothing was saved. Open Setup and add the repository again.", http.StatusConflict)
		return
	}
	if !cfg.HasRepo(repo) {
		http.Error(w, fmt.Sprintf("%q is not set up; add it on the Setup page.\n\nNothing was saved.", repo), http.StatusBadRequest)
		return
	}
	defaultBranch := func() (string, error) {
		if l := s.keptFor(repo).listing(); l != nil {
			return l.def, nil
		}
		client, err := s.client(r.Context())
		if err != nil {
			return "", err
		}
		return client.DefaultBranch(r.Context(), repo)
	}
	apply, err := prepare(r.PostForm, repo, defaultBranch)
	if err != nil {
		http.Error(w, err.Error()+"\n\nNothing was saved. Go back and try again.", http.StatusBadRequest)
		return
	}
	if err := cistatus.SaveConfig(s.dir, apply(cfg)); err != nil {
		http.Error(w, "saving failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, s.configureURL(repo), http.StatusSeeOther)
}
