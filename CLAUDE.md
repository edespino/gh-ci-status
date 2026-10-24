# ci-status

Shows the current state of chosen GitHub Actions workflow and branch pairs: a local web page,
terminal text, or JSON. Go, standard library only. Read README.md for what it does and
docs/architecture.md for how.

## Commands

```shell
make check                       # gofmt check, go vet, go test -race (what CI runs)
go test -count=1 ./...           # tests
go test -run TestName ./internal/web
make build && bin/ci-status serve --port 8724 --no-open
make banner                      # regenerate assets/banner.svg after changing the wordmark
```

Try changes against a scratch configuration so a real one is not touched:
`XDG_CONFIG_HOME=$(mktemp -d) bin/ci-status serve --port 8724`.

## Layout

- `*.go` at the root: package `cistatus`, the library (GitHub client, checks, config, rules,
  collection, JSON report). Anything an MCP server would need belongs here, not in `internal/`.
- `cmd/ci-status/`: the command line.
- `internal/web/`: HTTP server and `templates/*.html` (embedded).
- `internal/banner/`: the wordmark and logo.
- `internal/fakegithub/`: fake GitHub API used by tests.

## Rules

- Standard library only. Do not add a dependency without the maintainer's agreement.
- Every Go file starts with `// SPDX-License-Identifier: Apache-2.0`.
- Write the failing test first, watch it fail, then make it pass. Run `make check` before
  committing.
- Read-only toward GitHub. Never print or log the token or the page key; tests check this.
- Keep the web server's guarantees: `127.0.0.1` only, Host check, key in every URL, no cookie,
  CSP without remote sources. New routes go behind `guard`; new forms carry the key in their
  action and a `repo` hidden input when they act on a repository.
- No built-in repositories or workflows. Examples use neutral names such as
  `octo-org/octo-repo` or `example-org/example-repo`.
- Text in pages, messages and docs: plain words, no em or en dashes.
- The JSON report's field names are a contract: add fields, do not rename or remove them.
- `OWNER` in `go.mod`, import paths and README badges is a placeholder for the GitHub owner,
  replaced when the repository is published.

## Tests

- Tests pin the time zone to America/Los_Angeles in `TestMain`; do not change `time.Local`
  inside a test.
- Browser tests (`internal/web/browser_test.go`) need Chrome or Chromium (`$CHROME_PATH`, on
  `PATH`, or the macOS app) and skip without it.
- `TestBannerSVG` fails when `assets/banner.svg` no longer matches the wordmark; run
  `make banner`.
