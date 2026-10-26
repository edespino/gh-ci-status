# gh-ci-status

A GitHub CLI extension (`gh ci-status`) that shows the current state of chosen GitHub Actions workflow and branch pairs: a local web page,
terminal text, or JSON. Go, standard library only. Read README.md for what it does and
docs/architecture.md for how.

## Commands

```shell
make check                       # gofmt check, go vet, go test -race (what CI runs)
go test -count=1 ./...           # tests
go test -run TestName ./internal/web
make build && ./gh-ci-status serve --port 8724 --no-open   # or: gh extension install . && gh ci-status ...
make banner                      # regenerate assets/banner.svg and assets/icon.svg after changing the wordmark
make icon                        # also assets/icon.png (1024x1024), drawn by Chrome from icon.svg
```

Try changes against a scratch configuration so a real one is not touched:
`XDG_CONFIG_HOME=$(mktemp -d) ./gh-ci-status serve --port 8724`.

## Layout

- `*.go` at the root: package `cistatus`, the library (GitHub client, checks, config, rules,
  collection, JSON report). Anything an MCP server would need belongs here, not in `internal/`.
- `cmd/gh-ci-status/`: the command line (the extension's binary).
- `internal/web/`: HTTP server and `templates/*.html` (embedded).
- `internal/banner/`: the wordmark and logo.
- `internal/fakegithub/`: fake GitHub API used by tests.

## Rules

- Standard library only. Do not add a dependency without the maintainer's agreement.
- Every Go file starts with `// SPDX-License-Identifier: Apache-2.0`.
- Write the failing test first, watch it fail, then make it pass. Run `make check` before
  committing.
- Read-only toward GitHub. Never print or log the token, the page key or the Slack webhook;
  tests check this. The only write anywhere is the Slack post when the user presses Send to
  Slack, and only to a `hooks.slack.com/services/` address.
- Keep the web server's guarantees: `127.0.0.1` only, Host check, key in every URL, no cookie,
  CSP without remote sources. New routes go behind `guard`; new forms carry the key in their
  action and a `repo` hidden input when they act on a repository.
- No built-in repositories or workflows. Examples use neutral names such as
  `octo-org/octo-repo` or `example-org/example-repo`.
- Text in pages, messages and docs: plain words, no em or en dashes.
- The JSON report's field names are a contract: add fields, do not rename or remove them.
- The project, module, binary and settings folder are `gh-ci-status`; users run it as
  `gh ci-status`. The logo reads "gh-ci-status" with "gh-" in grey and "ci-status" coloured.

## Tests

- Tests pin the time zone to America/Los_Angeles in `TestMain`; do not change `time.Local`
  inside a test.
- Browser tests (`internal/web/browser_test.go`) need Chrome or Chromium (`$CHROME_PATH`, on
  `PATH`, or the macOS app) and skip without it.
- The daemon tests run this test binary as the background server: `TestMain` runs
  `helperMain` when `GH_CI_STATUS_HELPER=1`. They stop every server their `serve.log` names.
- `TestBannerSVG` and `TestIconSVG` fail when `assets/banner.svg` or `assets/icon.svg` no
  longer match the wordmark; run `make banner` (and `make icon` for `assets/icon.png`).
