// SPDX-License-Identifier: Apache-2.0

// Package version reports which gh-ci-status this is, from what Go stamps into
// the binary: the release tag for a build of a tagged release, a
// pseudo-version for a development build.
package version

import "runtime/debug"

// Get returns the version ("v0.1.0", a pseudo-version, or "(devel)" or
// "(unknown)" when Go recorded none) and the commit's first 12 characters, or
// "" when unknown.
func Get() (version, revision string) { return fromBuildInfo(debug.ReadBuildInfo()) }

func fromBuildInfo(info *debug.BuildInfo, ok bool) (string, string) {
	if !ok || info == nil {
		return "(unknown)", ""
	}
	v, rev := info.Main.Version, ""
	if v == "" {
		v = "(devel)"
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && len(s.Value) >= 12 {
			rev = s.Value[:12]
		}
	}
	return v, rev
}
