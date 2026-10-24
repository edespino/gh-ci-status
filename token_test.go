// SPDX-License-Identifier: Apache-2.0

package cistatus

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeGh puts a `gh` script on PATH (and nothing else).
func fakeGh(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func TestGhTokenTrimsOutput(t *testing.T) {
	fakeGh(t, `[ "$1 $2" = "auth token" ] || exit 9; echo tkn`)
	got, err := GHToken(context.Background())
	if err != nil || got != "tkn" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestGhTokenMissingGh(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := GHToken(context.Background())
	if err == nil || !strings.Contains(err.Error(), "gh not found") {
		t.Fatalf("error = %v", err)
	}
}

func TestGhTokenNotLoggedIn(t *testing.T) {
	fakeGh(t, `echo "no oauth token found" >&2; exit 1`)
	_, err := GHToken(context.Background())
	if err == nil || !strings.Contains(err.Error(), "gh auth login") {
		t.Fatalf("error = %v", err)
	}
}

func TestGhTokenEmptyOutput(t *testing.T) {
	fakeGh(t, `exit 0`)
	_, err := GHToken(context.Background())
	if err == nil || !strings.Contains(err.Error(), "gh auth login") {
		t.Fatalf("error = %v", err)
	}
}
