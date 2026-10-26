# Contributing to gh-ci-status

Thank you for helping. Issues and pull requests are welcome.

## Before you start

For anything larger than a small fix, open an issue first to agree on the change. gh-ci-status is
deliberately small: on-demand reads, no background polling, no hosted service, standard library
only.

## Making a change

1. Build and test: `make check` runs gofmt, `go vet` and the tests with the race detector, the
   same as CI. Go 1.27 or newer is needed.
2. Write a test that fails without your change, then make it pass. Tests use the fake GitHub API
   in `internal/fakegithub`; they never call the real GitHub.
3. The browser tests need Chrome or Chromium. They run when one is found on `PATH`, at
   `$CHROME_PATH`, or as the macOS application, and are skipped otherwise.
4. Keep every Go file's first line: `// SPDX-License-Identifier: Apache-2.0`.
5. Keep the security guarantees listed in [docs/architecture.md](docs/architecture.md).
6. Update README.md or docs/architecture.md when behaviour changes.

## Pull requests

- One topic per pull request, with a description of what changed and why.
- CI must pass.

## Versions and releases

gh-ci-status follows [Semantic Versioning](https://semver.org) with a `v` prefix:
`vMAJOR.MINOR.PATCH`.

- Until `v1.0.0`, a minor release (`v0.2.0`) may change behaviour; a patch release (`v0.1.1`)
  only fixes.
- From `v1.0.0`, the commands, their exit codes and the JSON report's field names are stable:
  changing or removing them needs a new major version; adding is a minor release.
- A tag with a `-` (`v0.2.0-rc.1`) makes a prerelease.

To release, tag a commit on `main` and push the tag: `git tag v0.1.0 && git push origin v0.1.0`.
The release workflow builds the binaries with the Go version in `go.mod`, which stamps the tag
into them, and attaches them to a GitHub release with build attestations.
`gh ci-status --version`, the footer of every page and the JSON report's `version` then show it.

## License

gh-ci-status is licensed under the [Apache License 2.0](LICENSE). Unless you state otherwise,
any contribution you submit is under the same license, as section 5 of the license describes.
