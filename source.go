// SPDX-License-Identifier: Apache-2.0

package cistatus

import "regexp"

// SourceURL is where gh-ci-status's source code and issues live.
const SourceURL = "https://github.com/edespino/gh-ci-status"

var (
	pseudoVersion  = regexp.MustCompile(`\d{14}-([0-9a-f]{12})$`) // v0.4.1-0.20261026182707-7d2679d4ffa4
	releaseVersion = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z.]+)?$`)
)

// SourceLink is the source of the given gh-ci-status version: a release's
// tag, a development build's commit, else (a build with local changes, or
// no version) the repository.
func SourceLink(version string) string {
	if m := pseudoVersion.FindStringSubmatch(version); m != nil {
		return SourceURL + "/tree/" + m[1]
	}
	if releaseVersion.MatchString(version) {
		return SourceURL + "/tree/" + version
	}
	return SourceURL
}
