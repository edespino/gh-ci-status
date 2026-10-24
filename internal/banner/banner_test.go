// SPDX-License-Identifier: Apache-2.0

package banner

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

const plainBanner = ` ┌─┐┬   ┌─┐┌┬┐┌─┐┌┬┐┬ ┬┌─┐
 │  │ ─ └─┐ │ ├─┤ │ │ │└─┐
 └─┘┴   └─┘ ┴ ┴ ┴ ┴ └─┘└─┘
 from red to green
`

func TestBannerPlain(t *testing.T) {
	if got := Terminal(false); got != plainBanner {
		t.Fatalf("Terminal(false) =\n%s\nwant\n%s", got, plainBanner)
	}
}

// Colour runs from red (xterm 160) on the left to green (28) on the right, deep
// enough to read on light and dark terminals, and adds nothing but escape codes.
func TestBannerColor(t *testing.T) {
	got := Terminal(true)
	if ansi.ReplaceAllString(got, "") != plainBanner {
		t.Fatalf("colour banner without escapes differs:\n%s", ansi.ReplaceAllString(got, ""))
	}
	first := strings.SplitN(got, "\n", 2)[0]
	codes := regexp.MustCompile(`\x1b\[38;5;(\d+)m`).FindAllStringSubmatch(first, -1)
	if len(codes) < 3 || codes[0][1] != "160" || codes[len(codes)-1][1] != "28" {
		t.Fatalf("first line colours %v, want 160 ... 28", codes)
	}
	if !strings.Contains(first, "\x1b[0m") {
		t.Error("first line does not reset the colour")
	}
}

// readme is the README's image, drawn from the same wordmark. Regenerate it
// with: UPDATE_BANNER=1 go test ./internal/banner -run TestBannerSVG
const readme = "../../assets/banner.svg"

// The README image matches the wordmark.
func TestBannerSVG(t *testing.T) {
	want := SVG(true)
	if os.Getenv("UPDATE_BANNER") != "" {
		if err := os.WriteFile(readme, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(readme)
	if err != nil || string(got) != want {
		t.Fatalf("assets/banner.svg is missing or out of date (%v); regenerate with UPDATE_BANNER=1", err)
	}
}
