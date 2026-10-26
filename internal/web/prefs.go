// SPDX-License-Identifier: Apache-2.0

package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	cistatus "github.com/edespino/gh-ci-status"
)

const prefsFile = "prefs.json"

// Prefs are the pages' own settings, kept in prefs.json in the settings
// folder. The zero value is the default.
type Prefs struct {
	// SkipConfirm runs Remove, Remove all, Watch N shown and Send to Slack
	// without asking first.
	SkipConfirm bool `json:"skip_confirm"`
}

// loadPrefs reads prefs.json in dir; without one, the defaults apply. When
// it cannot be read, the defaults apply and the error says why.
func loadPrefs(dir string) (Prefs, error) {
	var p Prefs
	data, err := os.ReadFile(filepath.Join(dir, prefsFile))
	if errors.Is(err, os.ErrNotExist) {
		return Prefs{}, nil
	}
	if err == nil {
		err = json.Unmarshal(data, &p)
	}
	if err != nil {
		return Prefs{}, fmt.Errorf("%s is not valid (%v); the defaults apply until it is saved again", filepath.Join(dir, prefsFile), err)
	}
	return p, nil
}

func savePrefs(dir string, p Prefs) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return cistatus.WritePrivate(dir, prefsFile, append(data, '\n'))
}

// skipConfirm is whether pages run their confirmed actions without asking.
func (s *Server) skipConfirm() bool {
	p, _ := loadPrefs(s.dir)
	return p.SkipConfirm
}

// setConfirm turns the questions before confirmed actions on (ask=on) or off
// (ask=off), and returns to that part of Settings.
func (s *Server) setConfirm(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form: "+err.Error(), http.StatusBadRequest)
		return
	}
	var skip bool
	switch r.PostForm.Get("ask") {
	case "on":
	case "off":
		skip = true
	default:
		http.Error(w, "ask must be on or off.\n\nNothing was saved.", http.StatusBadRequest)
		return
	}
	p, _ := loadPrefs(s.dir) // an unreadable file is replaced
	p.SkipConfirm = skip
	if err := savePrefs(s.dir, p); err != nil {
		http.Error(w, "saving failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, s.withKey("/setup")+"#confirm", http.StatusSeeOther)
}
