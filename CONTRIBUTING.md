# Contributing to ci-status

Thank you for helping. Issues and pull requests are welcome.

## Before you start

For anything larger than a small fix, open an issue first to agree on the change. ci-status is
deliberately small: on-demand reads, no background polling, no hosted service, standard library
only.

## Making a change

1. Build and test: `make check` runs gofmt, `go vet` and the tests with the race detector, the
   same as CI. Go 1.23 or newer is needed.
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

## License

ci-status is licensed under the [Apache License 2.0](LICENSE). Unless you state otherwise,
any contribution you submit is under the same license, as section 5 of the license describes.
