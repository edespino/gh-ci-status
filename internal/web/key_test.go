// SPDX-License-Identifier: Apache-2.0

package web

import (
	"os"
	"path/filepath"
	"regexp"
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

func TestKeyCreatedOnceAndReused(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ci-status")
	k1, err := LoadOrCreateKey(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(k1) {
		t.Fatalf("key %q", k1)
	}
	if m := mode(t, filepath.Join(dir, "key")); m != 0o600 {
		t.Errorf("key mode %o", m)
	}
	if m := mode(t, dir); m != 0o700 {
		t.Errorf("dir mode %o", m)
	}
	k2, err := LoadOrCreateKey(dir, false)
	if err != nil || k2 != k1 {
		t.Fatalf("second key %q, %v", k2, err)
	}
	k3, err := LoadOrCreateKey(dir, true)
	if err != nil || k3 == k1 || len(k3) != 64 {
		t.Fatalf("renewed key %q, %v", k3, err)
	}
	if k4, _ := LoadOrCreateKey(dir, false); k4 != k3 {
		t.Fatalf("renewed key not kept: %q", k4)
	}
}

func TestKeyWithGroupBitsRefused(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ci-status")
	if _, err := LoadOrCreateKey(dir, false); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "key")
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	_, err := LoadOrCreateKey(dir, false)
	if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("error = %v", err)
	}
}

func TestKeyMalformedRefused(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ci-status")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "key"), []byte("short\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadOrCreateKey(dir, false)
	if err == nil || !strings.Contains(err.Error(), "--new-key") {
		t.Fatalf("error = %v", err)
	}
}
