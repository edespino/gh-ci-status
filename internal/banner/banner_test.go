// SPDX-License-Identifier: Apache-2.0

package banner

import (
	"image/png"
	"math"
	"os"
	"regexp"
	"strconv"
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

// icon is the square image drawn from the wordmark; icon.png is made from it
// with 'make icon'. Regenerate icon.svg with 'make banner'.
const (
	icon    = "../../assets/icon.svg"
	iconPNG = "../../assets/icon.png"
)

// The icon file matches the wordmark.
func TestIconSVG(t *testing.T) {
	want := Icon()
	if os.Getenv("UPDATE_BANNER") != "" {
		if err := os.WriteFile(icon, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(icon)
	if err != nil || string(got) != want {
		t.Fatalf("assets/icon.svg is missing or out of date (%v); regenerate with make banner", err)
	}
}

// The icon is a 512 by 512 dark square with "ci" from the wordmark in the
// red-to-green gradient, centred.
func TestIcon(t *testing.T) {
	s := Icon()
	for _, want := range []string{`width="512" height="512" viewBox="0 0 512 512"`, `<rect width="512" height="512" fill="#18181b"/>`,
		`stroke="url(#ci-status-red-to-green)"`, `aria-label="gh-ci-status"`} {
		if !strings.Contains(s, want) {
			t.Errorf("icon lacks %s", want)
		}
	}
	m := regexp.MustCompile(`translate\(([\d.]+) ([\d.]+)\) scale\(([\d.]+)\)`).FindStringSubmatch(s)
	if m == nil {
		t.Fatalf("icon has no placement: %s", s)
	}
	tx, _ := strconv.ParseFloat(m[1], 64)
	ty, _ := strconv.ParseFloat(m[2], 64)
	k, _ := strconv.ParseFloat(m[3], 64)
	// "ci" spans x 7 to 56 and y 11 to 55 in wordmark cells of 14 by 22.
	if cx, cy := tx+(7+56)/2.0*k, ty+(11+55)/2.0*k; math.Abs(cx-256) > 0.5 || math.Abs(cy-256) > 0.5 {
		t.Errorf("letters centred at %.1f, %.1f, want 256, 256", cx, cy)
	}
	if w := 49 * k; w < 280 || w > 320 {
		t.Errorf("letters %.0f pixels wide, want about 300", w)
	}
	if strings.Count(s, "M") != strings.Count(strokes(prefix, prefix+4, 0, 0, 14, 22).d, "M") {
		t.Error("the icon draws more than the letters ci")
	}
}

// icon.png is the icon at 1024 by 1024, the size Slack and others accept
// (512 to 2000 pixels).
func TestIconPNG(t *testing.T) {
	f, err := os.Open(iconPNG)
	if err != nil {
		t.Fatalf("%v; make it with make icon", err)
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	if err != nil || cfg.Width != 1024 || cfg.Height != 1024 {
		t.Fatalf("icon.png: %v %dx%d, want a 1024x1024 PNG", err, cfg.Width, cfg.Height)
	}
}
