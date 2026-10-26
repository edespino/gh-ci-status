// SPDX-License-Identifier: Apache-2.0

package web

import (
	"io"
	"net/http"
	"net/url"
	"strings"

	cistatus "github.com/edespino/gh-ci-status"
)

// SetupPage is the first page a new user sees: whether the GitHub login
// works, which repositories are set up, and a form to add one.
type SetupPage struct {
	Key     string
	Login   cistatus.Check  // the GitHub API check
	Repos   []string        // set up, in order
	Repo    string          // what was typed, shown again after a failed add
	Problem *cistatus.Check // why the add failed
	Error   string          // for example an unreadable config file
	Version string          // shown in the footer
	// Slack is where Send to Slack posts, "" when no webhook is set; the
	// webhook itself is never shown.
	Slack        string
	SlackLabel   string // what was typed, shown again after a failed save
	SlackProblem string
	SkipConfirm  bool   // confirmed actions run without asking
	PrefsProblem string // prefs.json could not be read
	// From and FromRepo are the page Settings was opened from ("status", or
	// "workflows" and its repository); its forms carry them, so the back
	// button keeps working after them.
	From, FromRepo string
	// Back is the header button to that page: Status, or Workflows for
	// FromRepo; none while the status page would be empty.
	BackLabel, BackURL, BackBusy string
}

// settingsFrom is the page Settings was opened from, as the request says
// (query or form); anything else is ignored.
func settingsFrom(r *http.Request) (from, fromRepo string) {
	switch r.FormValue("from") {
	case "status":
		return "status", ""
	case "workflows":
		if repo := r.FormValue("from_repo"); cistatus.ValidRepo(repo) {
			return "workflows", repo
		}
	}
	return "", ""
}

// settingsURL is Settings, remembering the page it was opened from, at
// anchor (for example "#slack").
func (s *Server) settingsURL(r *http.Request, anchor string) string {
	v := url.Values{}
	if from, repo := settingsFrom(r); from != "" {
		v.Set("from", from)
		if repo != "" {
			v.Set("from_repo", repo)
		}
	}
	u := s.withKey("/setup")
	if len(v) > 0 {
		u += "&" + v.Encode()
	}
	return u + anchor
}

func (s *Server) setupPage(w http.ResponseWriter, r *http.Request) {
	s.renderSetup(w, r, http.StatusOK, SetupPage{})
}

// renderSetup fills p with the login check, the repositories and the Slack
// setting, and renders it.
func (s *Server) renderSetup(w http.ResponseWriter, r *http.Request, code int, p SetupPage) {
	p.Key, p.Version = s.key, s.version
	if prefs, err := loadPrefs(s.dir); err != nil {
		p.PrefsProblem = err.Error()
	} else {
		p.SkipConfirm = prefs.SkipConfirm
	}
	if st, set, err := cistatus.LoadSlack(s.dir); err != nil {
		p.SlackProblem = err.Error()
	} else if set {
		p.Slack = st.Label
	}
	cfg, _, err := cistatus.LoadConfig(s.dir)
	if err != nil {
		p.Error = err.Error() + ". Adding a repository here replaces the file."
	}
	p.Repos = cfg.RepoNames()
	p.From, p.FromRepo = settingsFrom(r)
	switch {
	case p.From == "workflows" && cfg.HasRepo(p.FromRepo):
		p.BackLabel, p.BackURL, p.BackBusy = "Workflows", s.configureURL(p.FromRepo), "Loading Workflows…"
	case anyWatched(cfg):
		p.BackLabel, p.BackURL, p.BackBusy = "Status", s.withKey("/"), "Reading GitHub…"
	}
	if client, err := s.client(r.Context()); err != nil {
		p.Login = cistatus.Check{Name: "GitHub login", Detail: err.Error(), Fix: "Run 'gh auth login', then reload this page"}
	} else {
		p.Login = cistatus.CheckAPI(r.Context(), client)
	}
	s.page(w, code, func(out io.Writer) error { return renderSetup(out, p) })
}

// addRepo checks the typed repository with GitHub and, when it can be read
// and has workflows, saves it under GitHub's spelling and opens Configure.
func (s *Server) addRepo(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form: "+err.Error(), http.StatusBadRequest)
		return
	}
	typed := r.PostForm.Get("repo")
	client, err := s.client(r.Context())
	if err != nil {
		s.renderSetup(w, r, http.StatusBadGateway, SetupPage{Repo: typed,
			Problem: &cistatus.Check{Name: "GitHub login", Detail: err.Error(), Fix: "Run 'gh auth login', then try again"}})
		return
	}
	check, name := cistatus.CheckRepo(r.Context(), client, cistatus.ParseRepo(typed))
	if !check.OK {
		s.renderSetup(w, r, http.StatusBadRequest, SetupPage{Repo: typed, Problem: &check})
		return
	}
	cfg, _, err := cistatus.LoadConfig(s.dir)
	if err != nil {
		cfg = cistatus.Config{} // unreadable: the page said adding replaces it
	}
	if err := cistatus.SaveConfig(s.dir, cfg.AddRepo(name)); err != nil {
		http.Error(w, "saving failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, s.configureURL(name), http.StatusSeeOther)
}

// removeRepo forgets a repository and everything watched in it.
func (s *Server) removeRepo(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form: "+err.Error(), http.StatusBadRequest)
		return
	}
	repo := r.PostForm.Get("repo")
	cfg, _, err := cistatus.LoadConfig(s.dir)
	if err != nil {
		http.Error(w, err.Error()+"\n\nNothing was saved.", http.StatusConflict)
		return
	}
	if !cfg.HasRepo(repo) {
		http.Error(w, "that repository is not set up.\n\nNothing was saved.", http.StatusBadRequest)
		return
	}
	if err := cistatus.SaveConfig(s.dir, cfg.RemoveRepo(repo)); err != nil {
		http.Error(w, "saving failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.mu.Lock()
	delete(s.kept, repo)
	s.mu.Unlock()
	http.Redirect(w, r, s.settingsURL(r, "#repositories"), http.StatusSeeOther)
}

// saveSlack checks and saves the Slack webhook. A refused address is not
// shown again: the page keeps only the typed name.
func (s *Server) saveSlack(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form: "+err.Error(), http.StatusBadRequest)
		return
	}
	label := r.PostForm.Get("label")
	settings := cistatus.SlackSettings{Webhook: strings.TrimSpace(r.PostForm.Get("webhook")), Label: label}
	if err := cistatus.SaveSlack(s.dir, settings); err != nil {
		s.renderSetup(w, r, http.StatusBadRequest, SetupPage{SlackLabel: label, SlackProblem: err.Error()})
		return
	}
	http.Redirect(w, r, s.settingsURL(r, "#slack"), http.StatusSeeOther)
}

// removeSlack forgets the Slack webhook.
func (s *Server) removeSlack(w http.ResponseWriter, r *http.Request) {
	if err := cistatus.RemoveSlack(s.dir); err != nil {
		http.Error(w, "removing failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, s.settingsURL(r, "#slack"), http.StatusSeeOther)
}
