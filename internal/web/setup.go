// SPDX-License-Identifier: Apache-2.0

package web

import (
	"io"
	"net/http"

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
}

func (s *Server) setupPage(w http.ResponseWriter, r *http.Request) {
	s.renderSetup(w, r, http.StatusOK, "", nil)
}

func (s *Server) renderSetup(w http.ResponseWriter, r *http.Request, code int, typed string, problem *cistatus.Check) {
	p := SetupPage{Key: s.key, Repo: typed, Problem: problem}
	cfg, _, err := cistatus.LoadConfig(s.dir)
	if err != nil {
		p.Error = err.Error() + ". Adding a repository here replaces the file."
	}
	p.Repos = cfg.RepoNames()
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
		s.renderSetup(w, r, http.StatusBadGateway, typed,
			&cistatus.Check{Name: "GitHub login", Detail: err.Error(), Fix: "Run 'gh auth login', then try again"})
		return
	}
	check, name := cistatus.CheckRepo(r.Context(), client, cistatus.ParseRepo(typed))
	if !check.OK {
		s.renderSetup(w, r, http.StatusBadRequest, typed, &check)
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
	http.Redirect(w, r, s.withKey("/setup"), http.StatusSeeOther)
}
