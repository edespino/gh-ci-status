// SPDX-License-Identifier: Apache-2.0

package web

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	cistatus "github.com/edespino/gh-ci-status"
)

var hexKey = regexp.MustCompile(`^[0-9a-f]{64}$`)

// LoadOrCreateKey returns the page key saved in dir/key, creating it on first
// use or when renew is set. A key file that other users can read is refused.
// The key keeps the bookmarked address the same across restarts.
func LoadOrCreateKey(dir string, renew bool) (string, error) {
	path := filepath.Join(dir, "key")
	if !renew {
		st, err := os.Stat(path)
		if err == nil {
			if st.Mode().Perm()&0o077 != 0 {
				return "", fmt.Errorf("%s can be read by other users; run: chmod 600 %s", path, path)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return "", err
			}
			key := strings.TrimSpace(string(data))
			if !hexKey.MatchString(key) {
				return "", fmt.Errorf("%s does not hold a valid key; start with --new-key", path)
			}
			return key, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	key := hex.EncodeToString(b)
	if err := cistatus.WritePrivate(dir, "key", []byte(key+"\n")); err != nil {
		return "", err
	}
	return key, nil
}
