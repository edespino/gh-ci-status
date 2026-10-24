// SPDX-License-Identifier: Apache-2.0

package cistatus

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// GHToken asks the GitHub CLI (gh) for the logged-in user's token. The token
// is returned to the caller and never written anywhere.
func GHToken(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "gh", "auth", "token").Output()
	if errors.Is(err, exec.ErrNotFound) {
		return "", fmt.Errorf("gh not found on PATH; install the GitHub CLI")
	}
	if err != nil {
		return "", fmt.Errorf("gh auth token failed (%v); run 'gh auth login'", err)
	}
	token := strings.TrimSpace(string(out))
	if token == "" {
		return "", fmt.Errorf("gh returned no token; run 'gh auth login'")
	}
	return token, nil
}
