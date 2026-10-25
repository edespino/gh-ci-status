// SPDX-License-Identifier: Apache-2.0

package banner

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

const plainBanner = ` ┌─┐┬ ┬   ┌─┐┬   ┌─┐┌┬┐┌─┐┌┬┐┬ ┬┌─┐
 │ ┬├─┤ ─ │  │ ─ └─┐ │ ├─┤ │ │ │└─┐
 └─┘┴ ┴   └─┘┴   └─┘ ┴ ┴ ┴ ┴ └─┘└─┘
 from red to green
`

func TestBannerPlain(t *testing.T) {
	if got := Terminal(false); got != plainBanner {
		t.Fatalf("Terminal(false) =\n%s\nwant\n%s", got, plainBanner)
	}
}

// "gh-" is grey (xterm 244); "ci-status" runs from red (160) to green (28), deep
// enough to read on light and dark terminals. Only escape codes are added.
func TestBannerColor(t *testing.T) {
	got := Terminal(true)
	if ansi.ReplaceAllString(got, "") != plainBanner {
		t.Fatalf("colour banner without escapes differs:\n%s", ansi.ReplaceAllString(got, ""))
	}
	first := strings.SplitN(got, "\n", 2)[0]
	codes := regexp.MustCompile(`\x1b\[38;5;(\d+)m`).FindAllStringSubmatch(first, -1)
	if len(codes) < 4 || codes[0][1] != "244" || codes[1][1] != "160" || codes[len(codes)-1][1] != "28" {
		t.Fatalf("first line colours %v, want 244, then 160 ... 28", codes)
	}
	grey := strings.SplitN(strings.SplitN(first, "\x1b[38;5;160m", 2)[0], "\x1b[38;5;244m", 2)[1]
	if ansi.ReplaceAllString(grey, "") != "┌─┐┬ ┬   " {
		t.Errorf("grey part = %q, want the \"gh-\" columns only", ansi.ReplaceAllString(grey, ""))
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

// In the images, "gh-" is one grey stroke (fixed in the README image, the
// page's text colour on the pages) and the gradient spans "ci-status" only.
func TestSVGGreyPrefix(t *testing.T) {
	for _, c := range []struct {
		tagged   bool
		grey, x1 string
		label    string
	}{
		{true, "#888888", `x1="138"`, `aria-label="gh-ci-status: from red to green"`},
		{false, "currentColor", `x1="129"`, `aria-label="gh-ci-status"`},
	} {
		s := SVG(c.tagged)
		strokes := regexp.MustCompile(`<path d="[^"]+" fill="none" stroke="([^"]+)"`).FindAllStringSubmatch(s, -1)
		if len(strokes) != 2 || strokes[0][1] != c.grey || strokes[1][1] != "url(#ci-status-red-to-green)" {
			t.Errorf("tagged=%v strokes %v, want %s then the gradient", c.tagged, strokes, c.grey)
		}
		if !strings.Contains(s, c.x1) || !strings.Contains(s, c.label) {
			t.Errorf("tagged=%v: gradient start or label wrong:\n%.300s", c.tagged, s)
		}
	}
}
