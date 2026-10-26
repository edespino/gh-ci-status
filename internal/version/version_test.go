// SPDX-License-Identifier: Apache-2.0

package version

import (
	"runtime/debug"
	"testing"
)

func info(v, rev string) *debug.BuildInfo {
	i := &debug.BuildInfo{Main: debug.Module{Path: "github.com/edespino/gh-ci-status", Version: v}}
	if rev != "" {
		i.Settings = []debug.BuildSetting{{Key: "vcs.revision", Value: rev}}
	}
	return i
}

func TestFromBuildInfo(t *testing.T) {
	for _, c := range []struct {
		name         string
		info         *debug.BuildInfo
		ok           bool
		version, rev string
	}{
		{"release", info("v0.1.0", "dbcd96230358aaaabbbbccccddddeeeeffff0000"), true, "v0.1.0", "dbcd96230358"},
		{"development build", info("v0.0.0-20261026070725-dbcd96230358+dirty", "dbcd96230358aaaa"), true, "v0.0.0-20261026070725-dbcd96230358+dirty", "dbcd96230358"},
		{"no version or commit", info("(devel)", ""), true, "(devel)", ""},
		{"short commit", info("v0.1.0", "abc"), true, "v0.1.0", ""},
		{"no build information", nil, false, "(unknown)", ""},
	} {
		if v, rev := fromBuildInfo(c.info, c.ok); v != c.version || rev != c.rev {
			t.Errorf("%s: got %q %q, want %q %q", c.name, v, rev, c.version, c.rev)
		}
	}
}
