// SPDX-License-Identifier: Apache-2.0

package cistatus

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mode(t *testing.T, path string) os.FileMode {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return st.Mode().Perm()
}

func TestConfigDirHonoursXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/x")
	if d, err := ConfigDir(); err != nil || d != "/x/ci-status" {
		t.Fatalf("got %q, %v", d, err)
	}
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", home)
	if d, err := ConfigDir(); err != nil || d != filepath.Join(home, ".config", "ci-status") {
		t.Fatalf("got %q, %v", d, err)
	}
}

func TestSaveThenLoadRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ci-status")
	cfg := Config{Repos: []RepoConfig{{Repo: "o/r", Watch: []Watch{{"a.yml", "main"}, {"a.yml", "release/x"}}}}}
	if err := SaveConfig(dir, cfg); err != nil {
		t.Fatal(err)
	}
	got, ok, err := LoadConfig(dir)
	if err != nil || !ok || len(got.Repos) != 1 || len(got.Repos[0].Watch) != 2 || got.Repos[0].Watch[1].Branch != "release/x" {
		t.Fatalf("got %+v ok=%v err=%v", got, ok, err)
	}
}

func TestSaveModes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ci-status")
	if err := SaveConfig(dir, Config{}); err != nil {
		t.Fatal(err)
	}
	if m := mode(t, dir); m != 0o700 {
		t.Errorf("dir mode %o", m)
	}
	if m := mode(t, filepath.Join(dir, "config.json")); m != 0o600 {
		t.Errorf("file mode %o", m)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("leftover files: %v", entries)
	}
	if err := SaveConfig(dir, Config{Repos: []RepoConfig{{Repo: "o/r"}}}); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("leftover files after overwrite: %v", entries)
	}
}

func TestLoadMissingIsNotError(t *testing.T) {
	cfg, ok, err := LoadConfig(t.TempDir())
	if ok || err != nil || len(cfg.Repos) != 0 {
		t.Fatalf("cfg=%+v ok=%v err=%v", cfg, ok, err)
	}
}

func TestLoadCorruptNamesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"repos": [`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := LoadConfig(dir)
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("error = %v", err)
	}
}

func TestAddAndRemoveRepo(t *testing.T) {
	cfg := Config{}.AddRepo("o/a").AddRepo("o/b").AddRepo("o/a")
	if got := strings.Join(cfg.RepoNames(), ","); got != "o/a,o/b" {
		t.Fatalf("repos = %s", got)
	}
	if !cfg.HasRepo("o/b") || cfg.HasRepo("o/c") || cfg.Watches("o/a") == nil {
		t.Fatalf("cfg = %+v", cfg)
	}
	before := cfg.AddWatches("o/b", Watch{"x.yml", "main"})
	after := before.RemoveRepo("o/b")
	if got := strings.Join(after.RepoNames(), ","); got != "o/a" || len(before.Watches("o/b")) != 1 {
		t.Fatalf("after remove %+v; input changed to %+v", after, before)
	}
}

func TestAddRemoveWatches(t *testing.T) {
	cfg := Config{Repos: []RepoConfig{{Repo: "other/repo", Watch: []Watch{{"x.yml", "main"}}}}}
	cfg = cfg.AddWatches(testRepo, Watch{"a.yml", "main"}, Watch{"b.yml", "dev"}, Watch{"a.yml", "main"})
	if len(cfg.Repos) != 2 || cfg.Repos[1].Repo != testRepo || len(cfg.Repos[1].Watch) != 2 {
		t.Fatalf("after add: %+v", cfg)
	}
	cfg = cfg.AddWatches(testRepo, Watch{"b.yml", "dev"}, Watch{"c.yml", "main"})
	if len(cfg.Repos[1].Watch) != 3 || cfg.Repos[1].Watch[2] != (Watch{"c.yml", "main"}) {
		t.Fatalf("after second add: %+v", cfg.Repos[1].Watch)
	}
	before := cfg
	cfg = cfg.RemoveWatch(testRepo, Watch{"b.yml", "dev"})
	if len(cfg.Repos[1].Watch) != 2 || cfg.Repos[1].Watch[1] != (Watch{"c.yml", "main"}) || len(cfg.Repos[0].Watch) != 1 {
		t.Fatalf("after remove: %+v", cfg)
	}
	if len(before.Repos[1].Watch) != 3 {
		t.Fatal("RemoveWatch modified its input")
	}
}

func TestRemoveWorkflow(t *testing.T) {
	cfg := Config{Repos: []RepoConfig{
		{Repo: "other/repo", Watch: []Watch{{"a.yml", "main"}}},
		{Repo: testRepo, Watch: []Watch{{"a.yml", "main"}, {"b.yml", "main"}, {"a.yml", "dev"}}},
	}}
	got := cfg.RemoveWorkflow(testRepo, "a.yml")
	if len(got.Repos[1].Watch) != 1 || got.Repos[1].Watch[0] != (Watch{"b.yml", "main"}) || len(got.Repos[0].Watch) != 1 {
		t.Fatalf("after remove: %+v", got)
	}
	if len(cfg.Repos[1].Watch) != 3 {
		t.Fatal("RemoveWorkflow modified its input")
	}
}

func TestSetWatches(t *testing.T) {
	cfg := Config{Repos: []RepoConfig{
		{Repo: "other/repo", Watch: []Watch{{"x.yml", "main"}}},
		{Repo: testRepo, Watch: []Watch{{"a.yml", "main"}, {"b.yml", "dev"}}},
	}}
	cleared := cfg.SetWatches(testRepo, nil)
	if len(cleared.Repos) != 2 || cleared.Repos[1].Repo != testRepo || cleared.Repos[1].Watch == nil ||
		len(cleared.Repos[1].Watch) != 0 || len(cleared.Repos[0].Watch) != 1 {
		t.Fatalf("cleared: %+v", cleared)
	}
	if len(cfg.Repos[1].Watch) != 2 {
		t.Fatal("SetWatches modified its input")
	}
	added := Config{}.SetWatches(testRepo, []Watch{{"c.yml", "main"}})
	if len(added.Repos) != 1 || added.Repos[0].Repo != testRepo || len(added.Repos[0].Watch) != 1 || added.Repos[0].Watch[0] != (Watch{"c.yml", "main"}) {
		t.Fatalf("added: %+v", added)
	}
}

func TestValidRepo(t *testing.T) {
	for _, r := range []string{"octo-org/octo-repo", "a/b", "A1/x.y_z-1", "o/.github"} {
		if !ValidRepo(r) {
			t.Errorf("%q should be valid", r)
		}
	}
	for _, r := range []string{"", "o", "o/", "/r", "o/r/x", "-o/r", "o/..", "o/.", "o/r?x", "o r/x", "o/r#1", strings.Repeat("o", 40) + "/r"} {
		if ValidRepo(r) {
			t.Errorf("%q should be invalid", r)
		}
	}
}

func TestValidBranch(t *testing.T) {
	for _, b := range []string{"main", "release/x", "feat-1"} {
		if !ValidBranch(b) {
			t.Errorf("%q should be valid", b)
		}
	}
	for _, b := range []string{"", "a b", "a\tb", "a\x00", strings.Repeat("x", 256)} {
		if ValidBranch(b) {
			t.Errorf("%q should be invalid", b)
		}
	}
}

func TestValidWorkflowFile(t *testing.T) {
	for _, f := range []string{"a.yml", "app-build.yml", "x_y.yaml"} {
		if !ValidWorkflowFile(f) {
			t.Errorf("%q should be valid", f)
		}
	}
	for _, f := range []string{"../x.yml", "a/b.yml", "a.txt", ".hidden.yml", ""} {
		if ValidWorkflowFile(f) {
			t.Errorf("%q should be invalid", f)
		}
	}
}

func TestParseRepo(t *testing.T) {
	for in, want := range map[string]string{
		"octo-org/octo-repo":                         "octo-org/octo-repo",
		"  octo-org/octo-repo \n":                    "octo-org/octo-repo",
		"https://github.com/octo-org/octo-repo":      "octo-org/octo-repo",
		"https://github.com/octo-org/octo-repo/":     "octo-org/octo-repo",
		"https://github.com/octo-org/octo-repo.git":  "octo-org/octo-repo",
		"github.com/octo-org/octo-repo":              "octo-org/octo-repo",
		"https://github.com/octo-org/octo-repo/pull": "octo-org/octo-repo/pull",
	} {
		if got := ParseRepo(in); got != want {
			t.Errorf("ParseRepo(%q) = %q, want %q", in, got, want)
		}
	}
}
