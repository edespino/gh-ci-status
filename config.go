// SPDX-License-Identifier: Apache-2.0

package cistatus

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// Watch is one thing the user follows: a workflow file on a branch.
type Watch struct {
	Workflow string `json:"workflow"` // file name under .github/workflows, e.g. "build.yml"
	Branch   string `json:"branch"`
}

// Config is the user's saved selection, one entry per repository. There are
// no built-in defaults: a new user starts with no repositories.
//
// The methods that change a Config return a new one and leave the receiver
// as it was.
type Config struct {
	Repos []RepoConfig `json:"repos"`
}

// RepoConfig is one repository and the watches in it.
type RepoConfig struct {
	Repo  string  `json:"repo"` // "owner/name"
	Watch []Watch `json:"watch"`
}

// ConfigDir is $XDG_CONFIG_HOME/gh-ci-status, else ~/.config/gh-ci-status.
func ConfigDir() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "gh-ci-status"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "gh-ci-status"), nil
}

// LoadConfig reads dir/config.json; ok is false when the file does not exist.
func LoadConfig(dir string) (cfg Config, ok bool, err error) {
	path := filepath.Join(dir, "config.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, false, nil
	}
	if err != nil {
		return Config{}, false, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, false, fmt.Errorf("%s: %v", path, err)
	}
	return cfg, true, nil
}

// SaveConfig writes dir/config.json, readable only by the user.
func SaveConfig(dir string, cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return WritePrivate(dir, "config.json", append(data, '\n'))
}

// WritePrivate writes dir/name atomically with mode 0600, creating dir with
// mode 0700.
func WritePrivate(dir, name string, data []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+name+"-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, name))
}

// RepoNames lists the configured repositories in order.
func (c Config) RepoNames() []string {
	var out []string
	for _, rc := range c.Repos {
		out = append(out, rc.Repo)
	}
	return out
}

// HasRepo reports whether repo is configured.
func (c Config) HasRepo(repo string) bool {
	return slices.Contains(c.RepoNames(), repo)
}

// Watches returns repo's watches.
func (c Config) Watches(repo string) []Watch {
	for _, rc := range c.Repos {
		if rc.Repo == repo {
			return rc.Watch
		}
	}
	return nil
}

// AddRepo adds repo with no watches, unless it is already there.
func (c Config) AddRepo(repo string) Config {
	out := c.clone()
	if !out.HasRepo(repo) {
		out.Repos = append(out.Repos, RepoConfig{Repo: repo, Watch: []Watch{}})
	}
	return out
}

// RemoveRepo drops repo and its watches.
func (c Config) RemoveRepo(repo string) Config {
	out := Config{Repos: []RepoConfig{}}
	for _, rc := range c.clone().Repos {
		if rc.Repo != repo {
			out.Repos = append(out.Repos, rc)
		}
	}
	return out
}

// AddWatches adds ws to repo's list (adding repo if needed), skipping pairs
// already there.
func (c Config) AddWatches(repo string, ws ...Watch) Config {
	out := c.AddRepo(repo)
	i := slices.Index(out.RepoNames(), repo)
	for _, w := range ws {
		if !slices.Contains(out.Repos[i].Watch, w) {
			out.Repos[i].Watch = append(out.Repos[i].Watch, w)
		}
	}
	return out
}

// RemoveWatch drops one workflow + branch pair from repo's list.
func (c Config) RemoveWatch(repo string, w Watch) Config {
	return c.filter(repo, func(x Watch) bool { return x != w })
}

// RemoveWorkflow drops every branch of one workflow file from repo's list.
func (c Config) RemoveWorkflow(repo, file string) Config {
	return c.filter(repo, func(x Watch) bool { return x.Workflow != file })
}

// SetWatches replaces repo's list with ws (adding repo if needed).
func (c Config) SetWatches(repo string, ws []Watch) Config {
	out := c.AddRepo(repo)
	out.Repos[slices.Index(out.RepoNames(), repo)].Watch = append([]Watch{}, ws...)
	return out
}

func (c Config) filter(repo string, keep func(Watch) bool) Config {
	out := c.clone()
	for i := range out.Repos {
		if out.Repos[i].Repo != repo {
			continue
		}
		kept := []Watch{}
		for _, x := range out.Repos[i].Watch {
			if keep(x) {
				kept = append(kept, x)
			}
		}
		out.Repos[i].Watch = kept
	}
	return out
}

func (c Config) clone() Config {
	out := Config{Repos: make([]RepoConfig, len(c.Repos))}
	for i, rc := range c.Repos {
		out.Repos[i] = RepoConfig{Repo: rc.Repo, Watch: append([]Watch{}, rc.Watch...)}
	}
	return out
}

var (
	repoName     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9._-]{1,100}$`)
	dotName      = regexp.MustCompile(`/\.{1,2}$`)
	workflowFile = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*\.ya?ml$`)
)

// ValidRepo accepts "owner/name" in the characters GitHub allows. It ends up
// in API paths, so nothing else is let through ("." and ".." included).
func ValidRepo(s string) bool { return repoName.MatchString(s) && !dotName.MatchString(s) }

// ParseRepo turns what a person typed into "owner/name". It accepts
// "owner/name" and "https://github.com/owner/name" (with or without ".git" or
// a trailing slash) and drops surrounding spaces. The result still needs
// ValidRepo.
func ParseRepo(s string) string {
	s = strings.TrimSpace(s)
	for _, prefix := range []string{"https://github.com/", "http://github.com/", "github.com/"} {
		s = strings.TrimPrefix(s, prefix)
	}
	return strings.TrimSuffix(strings.TrimSuffix(s, "/"), ".git")
}

// ValidWorkflowFile accepts a plain workflow file name such as "build.yml";
// it ends up in API paths.
func ValidWorkflowFile(s string) bool { return workflowFile.MatchString(s) }

// ValidBranch rejects empty, overlong, whitespace or control-character names.
func ValidBranch(b string) bool {
	if b == "" || len(b) > 255 {
		return false
	}
	for _, r := range b {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}
