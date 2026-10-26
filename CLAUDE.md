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
  collection, JSON report, the Slack message and post in `slack.go`, the source link in
  `source.go`). Anything an MCP server would need belongs here, not in `internal/`.
- `cmd/gh-ci-status/`: the command line (the extension's binary).
- `internal/web/`: HTTP server and `templates/*.html` (embedded). Pages show newer names than
  the code: the Workflows page is `/configure` (`configure.go`, `configure.html`) and Settings is
  `/setup` (`setup.go`, `setup.html`). `prefs.go` holds the pages' own settings (`prefs.json`).
- `internal/banner/`: the wordmark, logo and square icon.
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
  action and a `repo` hidden input when they act on a repository. Forms on Settings also carry
  `{{template "settings-from" .}}`, so its back button still returns where Settings was opened.
- A `data-confirm` question can be turned off in Settings (`data-skip-confirm` on the body), so
  the server must never rely on it: every action validates on its own.
- No built-in repositories or workflows. Examples use neutral names such as
  `octo-org/octo-repo` or `example-org/example-repo`.
- Text in pages, messages and docs: plain words, no em or en dashes. Use the literal words
  failing, passing, running (started) and waiting (queued, pending, waiting, requested), not
  red and green; the logo's tagline "from red to green" is the exception.
- The JSON report's field names are a contract: add fields, do not rename or remove them.
- The project, module, binary and settings folder are `gh-ci-status`; users run it as
  `gh ci-status`. The logo reads "gh-ci-status" with "gh-" in grey and "ci-status" coloured.

## Tests

- Tests pin the time zone to America/Los_Angeles in `TestMain`; do not change `time.Local`
  inside a test.
- Browser tests (`internal/web/browser_test.go`) need Chrome or Chromium (`$CHROME_PATH`, on
  `PATH`, or the macOS app) and skip without it. A test about motion passes
  `--force-prefers-no-reduced-motion` or `--force-prefers-reduced-motion`: CI's macOS runners
  ask for reduced motion.
- Page text includes the scripts, which contain names such as `data-skip-confirm`; check the
  tag itself (`<body data-skip-confirm>`), not the bare name.
- `TestSlackMessage` draws the whole Slack message as text (lists as `•`, sub-items as `◦`,
  links as `[text](url)`) and compares it; change the expected drawing with the format.
- To look at a page, render it to a file from a temporary `_test.go` and screenshot it with
  headless Chrome (`--screenshot`, `--virtual-time-budget`); delete the test file afterwards.
  Headless Chrome lays out at least 500 pixels wide, and a short window can come out blank.
- The daemon tests run this test binary as the background server: `TestMain` runs
  `helperMain` when `GH_CI_STATUS_HELPER=1`. They stop every server their `serve.log` names.
- `TestBannerSVG` and `TestIconSVG` fail when `assets/banner.svg` or `assets/icon.svg` no
  longer match the wordmark; run `make banner` (and `make icon` for `assets/icon.png`).
