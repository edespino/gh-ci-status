// SPDX-License-Identifier: Apache-2.0

package cistatus

import "regexp"

// SourceURL is where gh-ci-status's source code and issues live.
const SourceURL = "https://github.com/edespino/gh-ci-status"

// releaseVersion is a release's tag, such as v0.4.0 or v0.5.0-rc.1. A
// development build's version (v0.4.1-0.20261026182707-7d2679d4ffa4) does
// not match: its suffix holds a second hyphen.
var releaseVersion = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z.]+)?$`)

// SourceLink is the source of the given gh-ci-status version: a release's
// tag; anything else (a development build, whose commit may exist only on
// the machine that built it, or no version) the repository.
func SourceLink(version string) string {
	if releaseVersion.MatchString(version) {
		return SourceURL + "/tree/" + version
	}
	return SourceURL
}
