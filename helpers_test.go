// SPDX-License-Identifier: Apache-2.0

package cistatus

import (
	"net/http"
	"time"

	"github.com/edespino/gh-ci-status/internal/fakegithub"
)

// testClient reads the fake API with the token "tkn".
func testClient(f *fakegithub.Server) *Client {
	return &Client{BaseURL: f.URL(), Token: "tkn", HTTP: &http.Client{Timeout: 5 * time.Second}}
}
